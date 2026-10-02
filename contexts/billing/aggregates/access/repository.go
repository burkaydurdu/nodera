package access

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/authz"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// The writes are last-writer-wins on the event's own time: the events travel
// on separate queues, so a row only moves forward and an older event,
// redelivered or overtaken, changes nothing.

// UpsertTeam records a Team's name as of createdAt. The first insert keeps
// createdAt as the Team's creation time, which its Threshold is aged from.
func (r *Repository) UpsertTeam(ctx context.Context, teamID uuid.UUID, name string, createdAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO teams (team_id, name, created_at, updated_at)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (team_id) DO UPDATE
		SET name = COALESCE(NULLIF(EXCLUDED.name, ''), teams.name), updated_at = EXCLUDED.updated_at
		WHERE teams.updated_at <= EXCLUDED.updated_at`, teamID, name, createdAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// UpsertTeamMember records a membership as of its UpdatedAt; being added
// again after a removal lifts the tombstone. The comparison admits an equal
// time: TeamCreated and the owner's TeamMemberAdded carry the same instant.
// An empty contact field never blanks a known one, since an older producer
// sends none.
func (r *Repository) UpsertTeamMember(ctx context.Context, m *Member) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_members (team_id, user_id, role, email, full_name, locale, updated_at, removed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL)
		ON CONFLICT (team_id, user_id) DO UPDATE
		SET role = EXCLUDED.role,
		    email = COALESCE(NULLIF(EXCLUDED.email, ''), team_members.email),
		    full_name = COALESCE(NULLIF(EXCLUDED.full_name, ''), team_members.full_name),
		    locale = COALESCE(NULLIF(EXCLUDED.locale, ''), team_members.locale),
		    updated_at = EXCLUDED.updated_at, removed_at = NULL
		WHERE team_members.updated_at <= EXCLUDED.updated_at`,
		m.TeamID, m.UserID, m.Role, m.Email, m.FullName, m.Locale, m.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// UpdateTeamMemberRole changes a membership this context already knows. A
// Role is only ever changed for a member, so a change newer than a removal
// means the member was invited again and that event is still on its way.
func (r *Repository) UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE team_members
		SET role = $3, updated_at = $4, removed_at = NULL
		WHERE team_id = $1 AND user_id = $2 AND updated_at < $4`, teamID, userID, role, changedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// RemoveTeamMember tombstones the membership as of removedAt, writing the
// tombstone even for a member this context never saw so that their
// TeamMemberAdded, when it arrives late, cannot resurrect them.
func (r *Repository) RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_members (team_id, user_id, role, updated_at, removed_at)
		VALUES ($1, $2, '', $3, $3)
		ON CONFLICT (team_id, user_id) DO UPDATE
		SET updated_at = EXCLUDED.updated_at, removed_at = EXCLUDED.removed_at
		WHERE team_members.updated_at < EXCLUDED.updated_at`, teamID, userID, removedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindTeamMembership answers nil, without an error, for a non-member: the
// caller decides what a stranger is told.
func (r *Repository) FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error) {
	m := Membership{TeamID: teamID, UserID: userID}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT role FROM team_members
		WHERE team_id = $1 AND user_id = $2 AND removed_at IS NULL`, teamID, userID).Scan(&m.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &m, nil
}

// FindTeam answers a Team this context has heard of; nil for one it has not.
func (r *Repository) FindTeam(ctx context.Context, teamID uuid.UUID) (*Team, error) {
	var t Team
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT team_id, name, created_at, updated_at FROM teams WHERE team_id = $1`, teamID).
		Scan(&t.TeamID, &t.Name, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &t, nil
}

// OwnersOf answers the current owners of a Team with their contact details,
// oldest membership first; a member whose email never reached this context
// is left out, since no email can be sent to them.
func (r *Repository) OwnersOf(ctx context.Context, teamID uuid.UUID) ([]Recipient, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT user_id, email, full_name, locale FROM team_members
		WHERE team_id = $1 AND role = $2 AND removed_at IS NULL AND email <> ''
		ORDER BY created_at, user_id`, teamID, authz.RoleOwner)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	recipients := make([]Recipient, 0)
	for rows.Next() {
		var rec Recipient
		if err := rows.Scan(&rec.UserID, &rec.Email, &rec.FullName, &rec.Locale); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		recipients = append(recipients, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return recipients, nil
}
