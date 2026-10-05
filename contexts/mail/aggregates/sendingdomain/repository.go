package sendingdomain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const (
	constraintProjectName = "sending_domains_project_id_name_key"
	constraintToken       = "sending_domains_token_key"
)

const columns = `id, project_id, team_id, name, token, status, active_selector, return_path_valid, dkim_a_valid,
	dkim_b_valid, dmarc_found, last_checked_at, next_check_at, verified_at, rotation_selector, rotation_ready_at,
	keys_rotated_at, created_at`

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) Create(ctx context.Context, d *SendingDomain, keys []*Key) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO sending_domains (id, project_id, team_id, name, token, status, active_selector, next_check_at,
			keys_rotated_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		d.ID, d.ProjectID, d.TeamID, d.Name, d.Token, d.Status, d.ActiveSelector, d.NextCheckAt, d.KeysRotatedAt, d.CreatedAt)
	if constraint, ok := postgresql.UniqueViolationConstraint(err); ok {
		if constraint == constraintToken {
			return ErrorTokenCollision
		}
		if constraint == constraintProjectName {
			return ErrorAlreadyAdded
		}
	}
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	for _, key := range keys {
		if err := r.ReplaceKey(ctx, key); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT count(*) FROM sending_domains WHERE project_id = $1`, projectID).Scan(&n)

	return n, err
}

// Find scopes the lookup to the Project, so another Project's domain id is
// the same 404 as an unknown one.
func (r *Repository) Find(ctx context.Context, projectID, id uuid.UUID) (*SendingDomain, error) {
	return r.one(ctx, `SELECT `+columns+` FROM sending_domains WHERE id = $1 AND project_id = $2`, id, projectID)
}

func (r *Repository) FindForUpdate(ctx context.Context, id uuid.UUID) (*SendingDomain, error) {
	return r.one(ctx, `SELECT `+columns+` FROM sending_domains WHERE id = $1 FOR UPDATE`, id)
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*SendingDomain, error) {
	return r.one(ctx, `SELECT `+columns+` FROM sending_domains WHERE id = $1`, id)
}

func (r *Repository) ListByProject(ctx context.Context, projectID uuid.UUID) (SendingDomains, error) {
	return r.many(ctx, `SELECT `+columns+` FROM sending_domains WHERE project_id = $1 ORDER BY name`, projectID)
}

func (r *Repository) FindVerifiedByName(ctx context.Context, projectID uuid.UUID, name string) (*SendingDomain, error) {
	return r.one(ctx, `SELECT `+columns+` FROM sending_domains WHERE project_id = $1 AND name = $2 AND status = 'verified'`, projectID, name)
}

func (r *Repository) Save(ctx context.Context, d *SendingDomain) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE sending_domains
		SET status = $2, active_selector = $3, return_path_valid = $4, dkim_a_valid = $5, dkim_b_valid = $6,
			dmarc_found = $7, last_checked_at = $8, next_check_at = $9, verified_at = $10, rotation_selector = $11,
			rotation_ready_at = $12, keys_rotated_at = $13
		WHERE id = $1`,
		d.ID, d.Status, d.ActiveSelector, d.ReturnPathValid, d.DKIMAValid, d.DKIMBValid, d.DMARCFound, d.LastCheckedAt,
		d.NextCheckAt, d.VerifiedAt, d.RotationSelector, d.RotationReadyAt, d.KeysRotatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM sending_domains WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

func (r *Repository) ListDue(ctx context.Context, now time.Time, limit int) (SendingDomains, error) {
	return r.many(ctx, `SELECT `+columns+` FROM sending_domains WHERE next_check_at <= $1 ORDER BY next_check_at LIMIT $2`, now, limit)
}

func (r *Repository) ListRotationDue(ctx context.Context, rotatedBefore time.Time, limit int) (SendingDomains, error) {
	return r.many(ctx, `
		SELECT `+columns+` FROM sending_domains
		WHERE status = 'verified' AND rotation_selector IS NULL AND keys_rotated_at < $1
		ORDER BY keys_rotated_at LIMIT $2`, rotatedBefore, limit)
}

func (r *Repository) ListRotationReady(ctx context.Context, now time.Time, limit int) (SendingDomains, error) {
	return r.many(ctx, `
		SELECT `+columns+` FROM sending_domains
		WHERE rotation_selector IS NOT NULL AND rotation_ready_at <= $1
		ORDER BY rotation_ready_at LIMIT $2`, now, limit)
}

func (r *Repository) FindKey(ctx context.Context, domainID uuid.UUID, selector Selector) (*Key, error) {
	k := Key{SendingDomainID: domainID, Selector: selector}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT private_key, public_key FROM dkim_keys WHERE sending_domain_id = $1 AND selector = $2`, domainID, selector).
		Scan(&k.PrivateKey, &k.PublicKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return &k, nil
}

func (r *Repository) ReplaceKey(ctx context.Context, key *Key) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO dkim_keys (sending_domain_id, selector, private_key, public_key, created_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (sending_domain_id, selector) DO UPDATE
		SET private_key = EXCLUDED.private_key, public_key = EXCLUDED.public_key, created_at = EXCLUDED.created_at`,
		key.SendingDomainID, key.Selector, key.PrivateKey, key.PublicKey)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// ListPublishedKeys is every key the zone serves, whatever the domain's
// status: a pending domain's keys must already answer for its CNAMEs to
// resolve.
func (r *Repository) ListPublishedKeys(ctx context.Context) ([]*PublishedKey, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT d.token, k.selector, k.public_key FROM dkim_keys k JOIN sending_domains d ON d.id = k.sending_domain_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*PublishedKey
	for rows.Next() {
		var k PublishedKey
		if err := rows.Scan(&k.Token, &k.Selector, &k.PublicKey); err != nil {
			return nil, err
		}
		keys = append(keys, &k)
	}

	return keys, rows.Err()
}

func (r *Repository) FindPublishedKey(ctx context.Context, token string, selector Selector) (*PublishedKey, error) {
	k := PublishedKey{Token: token, Selector: selector}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT k.public_key FROM dkim_keys k JOIN sending_domains d ON d.id = k.sending_domain_id
		WHERE d.token = $1 AND k.selector = $2`, token, selector).Scan(&k.PublicKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return &k, nil
}

func (r *Repository) one(ctx context.Context, query string, args ...any) (*SendingDomain, error) {
	d, err := scan(r.queries.Executor(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return d, err
}

func (r *Repository) many(ctx context.Context, query string, args ...any) (SendingDomains, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := SendingDomains{}
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}

	return out, rows.Err()
}

func scan(row pgx.Row) (*SendingDomain, error) {
	var d SendingDomain
	err := row.Scan(&d.ID, &d.ProjectID, &d.TeamID, &d.Name, &d.Token, &d.Status, &d.ActiveSelector, &d.ReturnPathValid,
		&d.DKIMAValid, &d.DKIMBValid, &d.DMARCFound, &d.LastCheckedAt, &d.NextCheckAt, &d.VerifiedAt, &d.RotationSelector,
		&d.RotationReadyAt, &d.KeysRotatedAt, &d.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &d, nil
}
