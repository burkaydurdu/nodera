package notification

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const columns = `id, source_key, type, team_id, user_id, email, full_name, locale, params, in_inbox, created_at, read_at`

// visible is what an Inbox shows: the User's own In-app Notifications, and of
// those about a Team only while they are still in it ($2, their Teams).
const visible = `user_id = $1 AND in_inbox AND (team_id IS NULL OR team_id = ANY($2::uuid[]))`

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// Record writes the Notification and one pending delivery per Channel, in the
// caller's transaction. A Notification already recorded for the same event and
// Recipient is left as it is and Record answers false.
func (r *Repository) Record(ctx context.Context, n *Notification, channels []Channel) (bool, error) {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO notifications (source_key, recipient, type, team_id, user_id, email, full_name, locale, params, in_inbox)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (source_key, recipient) DO NOTHING
		RETURNING id, created_at`,
		n.SourceKey, n.Recipient.Key(), n.Type, n.TeamID, n.Recipient.UserID, n.Recipient.Email, n.Recipient.FullName,
		n.Recipient.Locale, n.Params, n.InInbox,
	).Scan(&n.ID, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	for _, channel := range channels {
		if _, err := r.queries.Executor(ctx).Exec(ctx, `
			INSERT INTO notification_deliveries (notification_id, channel) VALUES ($1, $2)
			ON CONFLICT (notification_id, channel) DO NOTHING`, n.ID, channel); err != nil {
			return false, ErrorCannotStore.Copy().SetError(err)
		}
	}

	return true, nil
}

// FindPending answers the Notifications of one event that still have a
// Channel to go out on, oldest first.
func (r *Repository) FindPending(ctx context.Context, sourceKey string) ([]Pending, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT n.id, n.source_key, n.type, n.team_id, n.user_id, n.email, n.full_name, n.locale, n.params, n.in_inbox,
		       n.created_at, n.read_at, d.channel
		FROM notifications n
		JOIN notification_deliveries d ON d.notification_id = n.id
		WHERE n.source_key = $1 AND d.delivered_at IS NULL
		ORDER BY n.id, d.channel`, sourceKey)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	pending := make([]Pending, 0)
	for rows.Next() {
		var n Notification
		var channel Channel
		if err := rows.Scan(&n.ID, &n.SourceKey, &n.Type, &n.TeamID, &n.Recipient.UserID, &n.Recipient.Email, &n.Recipient.FullName,
			&n.Recipient.Locale, &n.Params, &n.InInbox, &n.CreatedAt, &n.ReadAt, &channel); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		if last := len(pending) - 1; last >= 0 && pending[last].Notification.ID == n.ID {
			pending[last].Channels = append(pending[last].Channels, channel)

			continue
		}
		pending = append(pending, Pending{Notification: &n, Channels: []Channel{channel}})
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return pending, nil
}

func (r *Repository) MarkDelivered(ctx context.Context, id uuid.UUID, channel Channel, at time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE notification_deliveries SET delivered_at = $3, attempts = attempts + 1, last_error = ''
		WHERE notification_id = $1 AND channel = $2 AND delivered_at IS NULL`, id, channel, at)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, channel Channel, cause string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE notification_deliveries SET attempts = attempts + 1, last_error = $3
		WHERE notification_id = $1 AND channel = $2 AND delivered_at IS NULL`, id, channel, cause)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// ListInbox answers one page of the Inbox, newest first; before is the id
// the previous page ended on, nil for the first page.
func (r *Repository) ListInbox(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, before *uuid.UUID, limit int) ([]*Notification, error) {
	var cursor *string
	if before != nil {
		id := before.String()
		cursor = &id
	}

	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+columns+` FROM notifications
		WHERE `+visible+` AND ($3::uuid IS NULL OR id < $3::uuid)
		ORDER BY id DESC
		LIMIT $4`, userID, uuidStrings(teams), cursor, limit)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*Notification, 0, limit)
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

func (r *Repository) CountUnread(ctx context.Context, userID uuid.UUID, teams []uuid.UUID) (int, error) {
	var count int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT count(*) FROM notifications WHERE `+visible+` AND read_at IS NULL`, userID, uuidStrings(teams)).Scan(&count)
	if err != nil {
		return 0, ErrorCannotRead.Copy().SetError(err)
	}

	return count, nil
}

// MarkRead keeps the first time a Notification was read; reading it again
// changes nothing.
func (r *Repository) MarkRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, id uuid.UUID, at time.Time) (*Notification, error) {
	return scan(r.queries.Executor(ctx).QueryRow(ctx, `
		UPDATE notifications SET read_at = COALESCE(read_at, $4)
		WHERE `+visible+` AND id = $3
		RETURNING `+columns, userID, uuidStrings(teams), id, at))
}

// MarkAllRead answers the ids it marked, so the User's other tabs can follow.
func (r *Repository) MarkAllRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		UPDATE notifications SET read_at = $3
		WHERE `+visible+` AND read_at IS NULL
		RETURNING id`, userID, uuidStrings(teams), at)
	if err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, ErrorCannotStore.Copy().SetError(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}

	return ids, nil
}

func scan(row pgx.Row) (*Notification, error) {
	var n Notification
	err := row.Scan(&n.ID, &n.SourceKey, &n.Type, &n.TeamID, &n.Recipient.UserID, &n.Recipient.Email, &n.Recipient.FullName,
		&n.Recipient.Locale, &n.Params, &n.InInbox, &n.CreatedAt, &n.ReadAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &n, nil
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}

	return out
}
