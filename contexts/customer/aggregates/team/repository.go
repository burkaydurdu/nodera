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
// every live row on purpose and join the users table because the events
// carry who the person is, not just their id. The owner may have left the
// Team since, so the join is on the Team's owner, not on a membership.
func (r *Repository) FindAll(ctx context.Context) (TeamDetails, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT t.id, t.name, t.owner_user_id, t.created_at, t.updated_at, u.email, u.full_name, u.locale
		FROM teams t
		JOIN users u ON u.id = t.owner_user_id
		WHERE t.deleted_at IS NULL
		ORDER BY t.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := make(TeamDetails, 0)
	for rows.Next() {
		var t TeamDetail
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerUserID, &t.CreatedAt, &t.UpdatedAt, &t.Owner.Email, &t.Owner.FullName, &t.Owner.Locale); err != nil {
			return nil, err
		}
		teams = append(teams, &t)
	}

	return teams, rows.Err()
}

func (r *Repository) FindAllMembers(ctx context.Context) (MemberDetails, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+memberDetailColumns+`
		FROM team_members m
		JOIN teams t ON t.id = m.team_id AND t.deleted_at IS NULL
		JOIN users u ON u.id = m.user_id
		ORDER BY m.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make(MemberDetails, 0)
	for rows.Next() {
		m, err := scanMemberDetail(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
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

const memberDetailColumns = `m.team_id, m.user_id, m.role, m.created_at, u.email, u.full_name, u.locale`

func (r *Repository) FindMembers(ctx context.Context, teamID uuid.UUID) (MemberDetails, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+memberDetailColumns+`
		FROM team_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.team_id = $1
		ORDER BY m.created_at`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make(MemberDetails, 0)
	for rows.Next() {
		m, err := scanMemberDetail(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}

	return members, rows.Err()
}

func (r *Repository) FindMember(ctx context.Context, teamID, userID uuid.UUID) (*MemberDetail, error) {
	row := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT `+memberDetailColumns+`
		FROM team_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.team_id = $1 AND m.user_id = $2`, teamID, userID)

	m, err := scanMemberDetail(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorMemberNotFound
	}

	return m, err
}

// Lock takes the Team row for the rest of the transaction. It is its own
// statement on purpose: under READ COMMITTED a statement that blocks on the
// lock keeps the snapshot it started with, so a count in the same statement
// would read the world before the other transaction committed. Callers lock
// first, then read.
func (r *Repository) Lock(ctx context.Context, teamID uuid.UUID) error {
	var id uuid.UUID
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT id FROM teams WHERE id = $1 FOR UPDATE`, teamID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrorNotFound
	}
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) CountOwners(ctx context.Context, teamID uuid.UUID) (int, error) {
	var count int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT count(*) FROM team_members WHERE team_id = $1 AND role = $2`, teamID, string(RoleOwner),
	).Scan(&count)

	return count, err
}

func (r *Repository) UpdateMemberRole(ctx context.Context, teamID, userID uuid.UUID, role Role) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE team_members SET role = $3 WHERE team_id = $1 AND user_id = $2`, teamID, userID, string(role))
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorMemberNotFound
	}

	return nil
}

func (r *Repository) RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorMemberNotFound
	}

	return nil
}

func scanMemberDetail(row pgx.Row) (*MemberDetail, error) {
	var (
		m    MemberDetail
		role string
	)
	if err := row.Scan(&m.TeamID, &m.UserID, &role, &m.CreatedAt, &m.Email, &m.FullName, &m.Locale); err != nil {
		return nil, err
	}
	m.Role = Role(role)

	return &m, nil
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
