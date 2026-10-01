package team

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) Create(ctx context.Context, team *Team) error {
	if team.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return ErrorCannotCreate.Copy().SetError(err)
		}
		team.ID = id
	}

	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO teams (id, name, owner_user_id)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at`,
		team.ID, team.Name, team.OwnerUserID,
	).Scan(&team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		return ErrorCannotCreate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) AddMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (*Member, error) {
	member := &Member{TeamID: teamID, UserID: userID, Role: role}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO team_members (team_id, user_id, role)
		VALUES ($1, $2, $3)
		RETURNING created_at`, teamID, userID, string(role),
	).Scan(&member.CreatedAt)
	if err != nil {
		return nil, ErrorCannotCreate.Copy().SetError(err)
	}

	return member, nil
}

// FindAll and FindAllMembers feed the replay of domain events; they walk
// every live row on purpose.
func (r *Repository) FindAll(ctx context.Context) (Teams, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT id, name, owner_user_id, created_at, updated_at
		FROM teams
		WHERE deleted_at IS NULL
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := make(Teams, 0)
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerUserID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, &t)
	}

	return teams, rows.Err()
}

func (r *Repository) FindAllMembers(ctx context.Context) (Members, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT m.team_id, m.user_id, m.role, m.created_at
		FROM team_members m
		JOIN teams t ON t.id = m.team_id AND t.deleted_at IS NULL
		ORDER BY m.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make(Members, 0)
	for rows.Next() {
		var (
			m    Member
			role string
		)
		if err := rows.Scan(&m.TeamID, &m.UserID, &role, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		members = append(members, &m)
	}

	return members, rows.Err()
}

const membershipColumns = `t.id, t.name, t.owner_user_id, t.created_at, t.updated_at, m.role`

func (r *Repository) FindMembershipsByUserID(ctx context.Context, userID uuid.UUID) (Memberships, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+membershipColumns+`
		FROM team_members m
		JOIN teams t ON t.id = m.team_id AND t.deleted_at IS NULL
		WHERE m.user_id = $1
		ORDER BY t.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memberships := make(Memberships, 0)
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, m)
	}

	return memberships, rows.Err()
}

func (r *Repository) FindMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error) {
	row := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT `+membershipColumns+`
		FROM team_members m
		JOIN teams t ON t.id = m.team_id AND t.deleted_at IS NULL
		WHERE m.team_id = $1 AND m.user_id = $2`, teamID, userID)

	m, err := scanMembership(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotMember
	}

	return m, err
}

func scanMembership(row pgx.Row) (*Membership, error) {
	var (
		m    Membership
		role string
	)
	if err := row.Scan(&m.Team.ID, &m.Team.Name, &m.Team.OwnerUserID, &m.Team.CreatedAt, &m.Team.UpdatedAt, &role); err != nil {
		return nil, err
	}
	m.Role = Role(role)

	return &m, nil
}
