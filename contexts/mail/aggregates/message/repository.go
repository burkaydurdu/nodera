package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/contexts/mail/paging"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/utils/str"
)

const messageColumns = `m.id, m.project_id, m.team_id, m.sending_domain_id, m.api_key_id, m.source, m.from_address,
	m.from_name, m.subject, m.message_id_header, m.tags, m.size_bytes, m.idempotency_key, m.created_at, m.handed_over_at`

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// Create stores the Message, its recipients and the events they start with.
func (r *Repository) Create(ctx context.Context, m *Message) error {
	exec := r.queries.Executor(ctx)
	_, err := exec.Exec(ctx, `
		INSERT INTO messages (id, project_id, team_id, sending_domain_id, api_key_id, source, from_address, from_name,
			subject, message_id_header, tags, size_bytes, idempotency_key, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		m.ID, m.ProjectID, m.TeamID, m.SendingDomainID, m.APIKeyID, m.Source, m.From.Email, m.From.Name, m.Subject,
		m.MessageIDHeader, m.Tags, m.SizeBytes, m.IdempotencyKey, m.CreatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	for _, rcpt := range m.Recipients {
		_, err := exec.Exec(ctx, `
			INSERT INTO message_recipients (message_id, position, address, name, kind, status, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, m.ID, rcpt.Position, rcpt.Address, rcpt.Name, rcpt.Kind, rcpt.Status, rcpt.UpdatedAt)
		if err != nil {
			return ErrorCannotStore.Copy().SetError(err)
		}
		for _, e := range rcpt.Events {
			if _, err := r.RecordEvent(ctx, e); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Repository) Spool(ctx context.Context, entry *SpoolEntry) error {
	envelope, err := json.Marshal(entry.Envelope)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	_, err = r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO message_spool (message_id, content, envelope, next_attempt_at) VALUES ($1, $2, $3, $4)`,
		entry.MessageID, entry.Content, envelope, entry.NextAttemptAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// LockIdempotencyKey serializes two requests carrying the same key until the
// first one's transaction ends, so the second finds the first's Message
// instead of sending again.
func (r *Repository) LockIdempotencyKey(ctx context.Context, projectID uuid.UUID, key string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, projectID.String()+":"+key)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindByIdempotencyKey answers nil, without an error, when no Message carries
// the key since the given time.
func (r *Repository) FindByIdempotencyKey(ctx context.Context, projectID uuid.UUID, key string, since time.Time) (*Message, error) {
	m, err := r.oneMessage(ctx, `SELECT `+messageColumns+` FROM messages m
		WHERE m.project_id = $1 AND m.idempotency_key = $2 AND m.created_at >= $3
		ORDER BY m.created_at DESC LIMIT 1`, projectID, key, since)
	if errors.Is(err, ErrorNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return m, r.attachRecipients(ctx, []*Message{m}, false)
}

func (r *Repository) Find(ctx context.Context, projectID, id uuid.UUID) (*Message, error) {
	m, err := r.oneMessage(ctx, `SELECT `+messageColumns+` FROM messages m WHERE m.id = $1 AND m.project_id = $2`, id, projectID)
	if err != nil {
		return nil, err
	}

	return m, r.attachRecipients(ctx, []*Message{m}, true)
}

func (r *Repository) FindForEvent(ctx context.Context, id uuid.UUID) (*Message, error) {
	m, err := r.oneMessage(ctx, `SELECT `+messageColumns+` FROM messages m WHERE m.id = $1`, id)
	if err != nil {
		return nil, err
	}

	return m, r.attachRecipients(ctx, []*Message{m}, false)
}

// List pages a Project's Messages newest first. A status or a recipient
// filter matches a Message when one of its recipients does.
func (r *Repository) List(ctx context.Context, f Filter) ([]*Message, *paging.Cursor, error) {
	where := []string{"m.project_id = $1", "m.created_at >= $2"}
	args := []any{f.ProjectID, f.Since}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	if f.Until != nil {
		add("m.created_at <= $%d", *f.Until)
	}
	if str.IsNotEmpty(f.From) {
		add("m.from_address = $%d", f.From)
	}
	if str.IsNotEmpty(f.Tag) {
		add("$%d = ANY(m.tags)", f.Tag)
	}
	switch {
	case str.IsNotEmpty(f.Recipient) && f.Status != nil:
		args = append(args, f.Recipient, string(*f.Status))
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM message_recipients r WHERE r.message_id = m.id AND r.address = $%d AND r.status = $%d)", len(args)-1, len(args)))
	case str.IsNotEmpty(f.Recipient):
		add("EXISTS (SELECT 1 FROM message_recipients r WHERE r.message_id = m.id AND r.address = $%d)", f.Recipient)
	case f.Status != nil:
		add("EXISTS (SELECT 1 FROM message_recipients r WHERE r.message_id = m.id AND r.status = $%d)", string(*f.Status))
	}
	if f.Cursor != nil {
		args = append(args, f.Cursor.At, f.Cursor.ID)
		where = append(where, fmt.Sprintf("(m.created_at, m.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit+1)

	query := `SELECT ` + messageColumns + ` FROM messages m WHERE ` + strings.Join(where, " AND ") +
		fmt.Sprintf(` ORDER BY m.created_at DESC, m.id DESC LIMIT $%d`, len(args))
	messages, err := r.manyMessages(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}

	var next *paging.Cursor
	if len(messages) > f.Limit {
		messages = messages[:f.Limit]
		last := messages[len(messages)-1]
		next = &paging.Cursor{At: last.CreatedAt, ID: last.ID}
	}

	return messages, next, r.attachRecipients(ctx, messages, false)
}

// RecordEvent answers whether the event is new: KumoMTA may post a record
// twice, and the second must change nothing.
func (r *Repository) RecordEvent(ctx context.Context, e *Event) (bool, error) {
	var code *int
	var enhanced, text *string
	if e.Response != nil {
		code, enhanced, text = &e.Response.Code, &e.Response.EnhancedCode, &e.Response.Text
	}

	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO message_events (id, message_id, position, type, occurred_at, response_code, enhanced_code, response_text, bounce_class)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (message_id, position, type, occurred_at) DO NOTHING`,
		e.ID, e.MessageID, e.Position, e.Type, e.OccurredAt, code, enhanced, text, e.BounceClass)
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return tag.RowsAffected() > 0, nil
}

func (r *Repository) UpdateRecipientStatus(ctx context.Context, messageID uuid.UUID, position int, status EventType, at time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE message_recipients SET status = $3, updated_at = $4 WHERE message_id = $1 AND position = $2`,
		messageID, position, status, at)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

const spoolColumns = `s.message_id, s.content, s.envelope, s.attempts, s.next_attempt_at, m.created_at`

// DueSpool claims the Messages due for hand-over for the caller's
// transaction; a second dispatcher skips the rows the first holds, so a
// Message is never handed over twice.
// DueSpool answers the Messages whose next hand-over is due; each is claimed
// on its own afterwards.
func (r *Repository) DueSpool(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT message_id FROM message_spool WHERE next_attempt_at <= $1 ORDER BY next_attempt_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}

	return out, rows.Err()
}

func (r *Repository) ClaimSpool(ctx context.Context, messageID uuid.UUID) (*SpoolEntry, error) {
	e, err := scanSpool(r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT `+spoolColumns+`
		FROM message_spool s JOIN messages m ON m.id = s.message_id
		WHERE s.message_id = $1
		FOR UPDATE OF s SKIP LOCKED`, messageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	return e, err
}

func (r *Repository) DropSpool(ctx context.Context, messageID uuid.UUID) error {
	if _, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM message_spool WHERE message_id = $1`, messageID); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func scanSpool(row pgx.Row) (*SpoolEntry, error) {
	var e SpoolEntry
	var envelope []byte
	if err := row.Scan(&e.MessageID, &e.Content, &envelope, &e.Attempts, &e.NextAttemptAt, &e.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(envelope, &e.Envelope); err != nil {
		return nil, err
	}

	return &e, nil
}

// HandOver deletes the content the moment the MTA holds it: Nodera keeps no
// Message body.
func (r *Repository) HandOver(ctx context.Context, messageID uuid.UUID, at time.Time) error {
	exec := r.queries.Executor(ctx)
	if _, err := exec.Exec(ctx, `DELETE FROM message_spool WHERE message_id = $1`, messageID); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if _, err := exec.Exec(ctx, `UPDATE messages SET handed_over_at = $2 WHERE id = $1`, messageID, at); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// RetrySpool keeps the recipients still to hand over for another attempt.
func (r *Repository) RetrySpool(ctx context.Context, messageID uuid.UUID, pending Envelope, attempts int, next time.Time, lastError string) error {
	envelope, err := json.Marshal(pending)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	_, err = r.queries.Executor(ctx).Exec(ctx, `
		UPDATE message_spool SET envelope = $2, attempts = $3, next_attempt_at = $4, last_error = $5 WHERE message_id = $1`,
		messageID, envelope, attempts, next, lastError)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) ExpireCreatedBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM messages WHERE created_at < $1`, before)
	if err != nil {
		return 0, ErrorCannotStore.Copy().SetError(err)
	}

	return tag.RowsAffected(), nil
}

func (r *Repository) oneMessage(ctx context.Context, query string, args ...any) (*Message, error) {
	m, err := scanMessage(r.queries.Executor(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return m, err
}

func (r *Repository) manyMessages(ctx context.Context, query string, args ...any) ([]*Message, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}

	return out, rows.Err()
}

// attachRecipients loads the recipients of a page in one query, and their
// events in a second when the detail is asked for.
func (r *Repository) attachRecipients(ctx context.Context, messages []*Message, withEvents bool) error {
	if len(messages) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, len(messages))
	byID := make(map[uuid.UUID]*Message, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
		byID[m.ID] = m
		m.Recipients = []*Recipient{}
	}

	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT message_id, position, address, name, kind, status, updated_at FROM message_recipients
		WHERE message_id = ANY($1) ORDER BY message_id, position`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var rcpt Recipient
		if err := rows.Scan(&id, &rcpt.Position, &rcpt.Address, &rcpt.Name, &rcpt.Kind, &rcpt.Status, &rcpt.UpdatedAt); err != nil {
			rows.Close()

			return err
		}
		rcpt.Events = []*Event{}
		byID[id].Recipients = append(byID[id].Recipients, &rcpt)
	}
	rows.Close()
	if err := rows.Err(); err != nil || !withEvents {
		return err
	}

	return r.attachEvents(ctx, ids, byID)
}

// attachEvents orders events of the same instant the way a recipient lives
// through them, so a history never shows a bounce before its acceptance.
func (r *Repository) attachEvents(ctx context.Context, ids []uuid.UUID, byID map[uuid.UUID]*Message) error {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT id, message_id, position, type, occurred_at, response_code, enhanced_code, response_text, bounce_class
		FROM message_events WHERE message_id = ANY($1)
		ORDER BY occurred_at,
			CASE type WHEN 'accepted' THEN 0 WHEN 'suppressed' THEN 0 WHEN 'deferred' THEN 1 WHEN 'complained' THEN 3 ELSE 2 END, id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var e Event
		var code *int
		var enhanced, text *string
		if err := rows.Scan(&e.ID, &e.MessageID, &e.Position, &e.Type, &e.OccurredAt, &code, &enhanced, &text, &e.BounceClass); err != nil {
			return err
		}
		if code != nil {
			e.Response = &Response{Code: *code, EnhancedCode: deref(enhanced), Text: deref(text)}
		}
		for _, rcpt := range byID[e.MessageID].Recipients {
			if rcpt.Position == e.Position {
				rcpt.Events = append(rcpt.Events, &e)
			}
		}
	}

	return rows.Err()
}

func scanMessage(row pgx.Row) (*Message, error) {
	var m Message
	err := row.Scan(&m.ID, &m.ProjectID, &m.TeamID, &m.SendingDomainID, &m.APIKeyID, &m.Source, &m.From.Email, &m.From.Name,
		&m.Subject, &m.MessageIDHeader, &m.Tags, &m.SizeBytes, &m.IdempotencyKey, &m.CreatedAt, &m.HandedOverAt)
	if err != nil {
		return nil, err
	}

	return &m, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
