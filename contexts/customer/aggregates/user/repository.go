package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const emailUniqueConstraint = "users_email_unique"

const selectColumns = `id, email, password, full_name, locale, country, status, mfa_method,
       email_verified_at, created_at, updated_at, deleted_at`

func (r *Repository) Create(ctx context.Context, user *User) error {
	if user.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return ErrorCannotCreate.Copy().SetError(err)
		}
		user.ID = id
	}
	if user.Status == "" {
		user.Status = StatusPendingVerification
	}
	if user.MFAMethod == "" {
		user.MFAMethod = mfa.MethodEmail
	}

	query := `
		INSERT INTO users (id, email, password, full_name, locale, country, status, mfa_method)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at`

	err := r.queries.Executor(ctx).QueryRow(ctx, query,
		user.ID, user.Email, user.Password, user.FullName, string(user.Locale), user.Country,
		string(user.Status), string(user.MFAMethod),
	).Scan(&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == emailUniqueConstraint {
			return ErrorEmailTaken
		}

		return ErrorCannotCreate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+selectColumns+` FROM users WHERE id = $1 AND deleted_at IS NULL`, id)

	return scanUser(row)
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	row := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+selectColumns+` FROM users WHERE lower(email) = lower($1) AND deleted_at IS NULL`, email)

	return scanUser(row)
}

func (r *Repository) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	return r.exec(ctx, `
		UPDATE users
		SET status = $2, email_verified_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`, id, string(StatusActive))
}

func (r *Repository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return r.exec(ctx, `UPDATE users SET password = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id, passwordHash)
}

func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, fullName string, locale Locale, country string) error {
	return r.exec(ctx, `
		UPDATE users
		SET full_name = $2, locale = $3, country = $4, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`, id, fullName, string(locale), country)
}

func (r *Repository) UpdateMFAMethod(ctx context.Context, id uuid.UUID, method mfa.Method) error {
	return r.exec(ctx, `UPDATE users SET mfa_method = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id, string(method))
}

func (r *Repository) exec(ctx context.Context, query string, args ...any) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, query, args...)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

func scanUser(row pgx.Row) (*User, error) {
	var (
		u                          User
		locale, status, mfaMethod  string
		emailVerifiedAt, deletedAt *time.Time
	)

	err := row.Scan(
		&u.ID, &u.Email, &u.Password, &u.FullName, &locale, &u.Country, &status, &mfaMethod,
		&emailVerifiedAt, &u.CreatedAt, &u.UpdatedAt, &deletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, err
	}

	u.Locale = Locale(locale)
	u.Status = Status(status)
	u.MFAMethod = mfa.Method(mfaMethod)
	u.EmailVerifiedAt = emailVerifiedAt
	u.DeletedAt = deletedAt

	return &u, nil
}
