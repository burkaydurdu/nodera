package invitation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/authz"
)

const pendingUniqueConstraint = "invitations_pending_unique"

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const selectColumns = `i.id, i.team_id, i.email, i.role, i.invited_by, i.token_hash, i.expires_at, i.status,
       i.accepted_by, i.responded_at, i.last_sent_at, i.created_at, t.name, u.full_name`

const fromJoined = `
		FROM invitations i
		JOIN teams t ON t.id = i.team_id
		JOIN users u ON u.id = i.invited_by`

func (r *Repository) Create(ctx context.Context, inv *Invitation) error {
	if inv.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return ErrorCannotCreate.Copy().SetError(err)
		}
		inv.ID = id
	}
	inv.Status = StatusPending

	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO invitations (id, team_id, email, role, invited_by, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING last_sent_at, created_at`,
		inv.ID, inv.TeamID, inv.Email, string(inv.Role), inv.InvitedBy, inv.TokenHash, inv.ExpiresAt,
	).Scan(&inv.LastSentAt, &inv.CreatedAt)
	if err != nil {
		if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == pendingUniqueConstraint {
			return ErrorAlreadyPending
		}

		return ErrorCannotCreate.Copy().SetError(err)
	}

	return nil
}

// ExpireStale writes the lazily computed expiry down for one address in one
// Team, so the one-pending-per-address index does not hold a dead offer
// against a new one.
func (r *Repository) ExpireStale(ctx context.Context, teamID uuid.UUID, email string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE invitations SET status = $3
		WHERE team_id = $1 AND email = $2 AND status = $4 AND expires_at <= now()`,
		teamID, email, string(StatusExpired), string(StatusPending))
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*Invitation, error) {
	return scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+selectColumns+fromJoined+` WHERE i.id = $1`, id))
}

func (r *Repository) FindByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error) {
	return scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+selectColumns+fromJoined+` WHERE i.token_hash = $1`, tokenHash))
}

func (r *Repository) FindByTeamID(ctx context.Context, teamID uuid.UUID) (Invitations, error) {
	return r.list(ctx, `SELECT `+selectColumns+fromJoined+` WHERE i.team_id = $1 ORDER BY i.created_at DESC`, teamID)
}

// FindPendingByEmail answers the offers a User can still act on.
func (r *Repository) FindPendingByEmail(ctx context.Context, email string) (Invitations, error) {
	return r.list(ctx, `
		SELECT `+selectColumns+fromJoined+`
		WHERE i.email = $1 AND i.status = $2 AND i.expires_at > now() AND t.deleted_at IS NULL
		ORDER BY i.created_at DESC`, email, string(StatusPending))
}

// Reissue gives a pending offer a new token and a new life; the old link dies.
func (r *Repository) Reissue(ctx context.Context, id uuid.UUID, tokenHash string, expiresAt time.Time) error {
	return r.updatePending(ctx, `
		UPDATE invitations SET token_hash = $2, expires_at = $3, last_sent_at = now()
		WHERE id = $1 AND status = $4`, id, tokenHash, expiresAt, string(StatusPending))
}

// Respond closes a pending offer with its answer. Only a pending row moves,
// so two answers racing cannot both win.
func (r *Repository) Respond(ctx context.Context, id uuid.UUID, status Status, acceptedBy *uuid.UUID) error {
	return r.updatePending(ctx, `
		UPDATE invitations SET status = $2, accepted_by = $3, responded_at = now()
		WHERE id = $1 AND status = $4`, id, string(status), acceptedBy, string(StatusPending))
}

func (r *Repository) updatePending(ctx context.Context, query string, args ...any) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, query, args...)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNoLongerValid
	}

	return nil
}

func (r *Repository) list(ctx context.Context, query string, args ...any) (Invitations, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invitations := make(Invitations, 0)
	for rows.Next() {
		inv, err := scan(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, inv)
	}

	return invitations, rows.Err()
}

func scan(row pgx.Row) (*Invitation, error) {
	var (
		inv          Invitation
		role, status string
	)
	err := row.Scan(
		&inv.ID, &inv.TeamID, &inv.Email, &role, &inv.InvitedBy, &inv.TokenHash, &inv.ExpiresAt, &status,
		&inv.AcceptedBy, &inv.RespondedAt, &inv.LastSentAt, &inv.CreatedAt, &inv.TeamName, &inv.InviterName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, err
	}
	inv.Role = authz.Role(role)
	inv.Status = Status(status)

	return &inv, nil
}
