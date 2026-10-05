package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// IAPIKeyEntries is where a key meets its readers: the integration gateway and
// the mail service authenticate it from Redis, never from this database
// (docs/adr/0010).
//
//go:generate mockgen -destination=../../mock/services/apikey_entries.go -package=mockservices -mock_names=IAPIKeyEntries=MockIAPIKeyEntries github.com/piggystats/nodera/contexts/customer/application/handlers IAPIKeyEntries
type IAPIKeyEntries interface {
	Write(ctx context.Context, key *apikey.APIKey) error
	Delete(ctx context.Context, id string) error
	LastUsed(ctx context.Context, ids []string) (map[string]time.Time, error)
}

type APIKey struct {
	log          logger.ILog
	teams        team.IService
	projects     project.IService
	keys         apikey.IService
	transactions postgresql.ITransactionManager
	entries      IAPIKeyEntries
}

func NewAPIKey(
	log logger.ILog,
	teams team.IService,
	projects project.IService,
	keys apikey.IService,
	transactions postgresql.ITransactionManager,
	entries IAPIKeyEntries,
) *APIKey {
	return &APIKey{log: log, teams: teams, projects: projects, keys: keys, transactions: transactions, entries: entries}
}

// Every route asks apikey.write, listing too: a member who may not make keys
// is not handed the list of them either.
func (h *APIKey) List(ctx context.Context, req *apikey.ListRequest) ([]*dto.APIKey, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.APIKeyWrite)
	if err != nil {
		return nil, err
	}
	p, err := h.projects.FindInTeam(ctx, m.Team.ID, uuid.MustParse(req.ProjectID))
	if err != nil {
		return nil, err
	}

	keys, err := h.keys.FindActiveByProjectID(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	lastUsed, err := h.entries.LastUsed(ctx, ids)
	if err != nil {
		return nil, err
	}

	return dto.ToAPIKeys(keys, lastUsed), nil
}

// Create answers the whole key, the only time it is shown. The key is handed
// out only once its Redis entry exists, so it authenticates from the first
// request.
func (h *APIKey) Create(ctx context.Context, req *apikey.CreateRequest) (*dto.CreatedAPIKey, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.APIKeyWrite)
	if err != nil {
		return nil, err
	}
	permissions, err := keyPermissions(m.Role, req.Permissions)
	if err != nil {
		return nil, err
	}

	id, secret, err := apikey.NewKey()
	if err != nil {
		return nil, err
	}
	key := &apikey.APIKey{
		ID:          id,
		ProjectID:   uuid.MustParse(req.ProjectID),
		TeamID:      m.Team.ID,
		Name:        req.Name,
		SecretHash:  apikey.HashSecret(secret),
		Permissions: permissions,
		CreatedBy:   contextutil.CurrentUserID(ctx),
	}
	if err := h.store(ctx, key); err != nil {
		return nil, err
	}

	if err := h.entries.Write(ctx, key); err != nil {
		h.withdraw(ctx, key)

		return nil, apikey.ErrorCannotCreate.Copy().SetError(err)
	}

	return dto.ToCreatedAPIKey(key, apikey.FullKey(id, secret)), nil
}

// Revoke ends a key on its readers' next lookup. The entry is deleted inside
// the revoking transaction: when Redis fails, the row stays active, so the key
// is still listed and can be revoked again, rather than vanishing from the
// list while it keeps working until the next `sync-api-keys`.
func (h *APIKey) Revoke(ctx context.Context, req *apikey.RevokeRequest) error {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.APIKeyWrite)
	if err != nil {
		return err
	}
	p, err := h.projects.FindInTeam(ctx, m.Team.ID, uuid.MustParse(req.ProjectID))
	if err != nil {
		return err
	}

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	if err := h.keys.Revoke(txCtx, m.Team.ID, p.ID, req.KeyID); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}
	if err := h.entries.Delete(ctx, req.KeyID); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return apikey.ErrorCannotRevoke.Copy().SetError(err)
	}

	return h.transactions.Commit(txCtx)
}

// store inserts the key with its Project locked, so two creations racing for
// the last free place cannot both take it. Locking also proves the Project
// belongs to the Team.
func (h *APIKey) store(ctx context.Context, key *apikey.APIKey) error {
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	if err := h.projects.Lock(txCtx, key.TeamID, key.ProjectID); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}

	active, err := h.keys.CountActive(txCtx, key.ProjectID)
	if err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}
	if active >= apikey.MaxActivePerProject {
		_ = h.transactions.Rollback(txCtx)

		return apikey.ErrorLimitReached
	}
	if err := h.keys.Create(txCtx, key); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}

	return h.transactions.Commit(txCtx)
}

// withdraw revokes a key whose entry could not be written. Nobody will ever
// see its secret, so it must not hold one of the Project's places; a failure
// here is logged, since the caller already gets the write's error.
func (h *APIKey) withdraw(ctx context.Context, key *apikey.APIKey) {
	if err := h.keys.Revoke(ctx, key.TeamID, key.ProjectID, key.ID); err != nil {
		h.log.Error(err)
	}
}

// keyPermissions refuses what no key may carry before what this creator may
// not grant: the first is a malformed request whoever sends it, the second is
// about the caller's Role, which a key never exceeds.
func keyPermissions(role team.Role, requested []string) ([]authz.Permission, error) {
	permissions := make([]authz.Permission, 0, len(requested))
	for _, p := range requested {
		if !authz.IsKeyPermission(p) {
			return nil, apikey.ErrorPermissionNotAllowed
		}
		permissions = append(permissions, authz.Permission(p))
	}
	for _, p := range permissions {
		if !role.Allows(p) {
			return nil, apikey.ErrorPermissionNotHeld
		}
	}

	return permissions, nil
}
