package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/accesskey"
	"github.com/piggystats/nodera/contexts/storage/aggregates/billing"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// AccessKeys makes, lists and revokes a Storage's Access keys. The engine is
// asked before the database is written, so a key the customer is shown
// exists, and one that is gone from the list is gone from the engine.
type AccessKeys struct {
	authorizer
	log       logger.ILog
	keys      accesskey.IService
	engine    Engine
	accounts  *accounts
	announcer Announcer
	now       Clock
}

func NewAccessKeys(log logger.ILog, accessService access.IService, storageService storage.IService, keyService accesskey.IService, eng Engine, accounts *accounts, announcer Announcer, now Clock) *AccessKeys {
	return &AccessKeys{
		authorizer: authorizer{access: accessService, storages: storageService},
		log:        log, keys: keyService, engine: eng, accounts: accounts, announcer: announcer, now: now,
	}
}

func (h *AccessKeys) List(ctx context.Context, req *accesskey.ListRequest) ([]dto.AccessKey, error) {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageRead)
	if err != nil {
		return nil, err
	}
	list, err := h.keys.ListByStorage(ctx, s.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToAccessKeys(list), nil
}

// Create answers the secret key this once. A stopped Storage takes no new
// key: it would not work, and a suspended Team is not handed anything new.
func (h *AccessKeys) Create(ctx context.Context, req *accesskey.CreateRequest) (*dto.CreatedAccessKey, error) {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageWrite)
	if err != nil {
		return nil, err
	}
	req.Normalize()
	if err := usable(s); err != nil {
		return nil, err
	}
	count, err := h.keys.CountByStorage(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	if count >= accesskey.MaxPerStorage {
		return nil, accesskey.ErrorTooMany
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return nil, err
	}

	key := &accesskey.AccessKey{
		ID: uuid.New(), StorageID: s.ID, Name: req.Name, Permission: accesskey.Permission(req.Permission),
		CreatedBy: contextutil.CurrentUserID(ctx), CreatedAt: h.now(),
	}
	credentials, err := h.engine.CreateAccessKey(ctx, account, s.BucketName, key.ID, engine.Permission(key.Permission))
	if err != nil {
		return nil, engineError(err)
	}
	key.AccessKeyID = credentials.AccessKeyID
	if err := h.keys.Insert(ctx, key); err != nil {
		// A key nobody will ever see must not stay in the engine.
		if dropped := h.engine.DeleteAccessKey(ctx, account, s.BucketName, key.ID); dropped != nil {
			h.log.ErrorWithFields(dropped, logger.NewFields().Add("access_key_id", key.ID))
		}

		return nil, err
	}
	h.changed(ctx, s)

	return &dto.CreatedAccessKey{AccessKey: dto.ToAccessKey(key), SecretKey: credentials.SecretKey}, nil
}

// Revoke removes the key from the engine first; a key the engine still holds
// stays listed, so the customer knows it is not yet gone.
func (h *AccessKeys) Revoke(ctx context.Context, req *accesskey.RevokeRequest) error {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageWrite)
	if err != nil {
		return err
	}
	key, err := h.keys.Find(ctx, s.ID, uuid.MustParse(req.AccessKeyID))
	if err != nil {
		return err
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if err := h.engine.DeleteAccessKey(ctx, account, s.BucketName, key.ID); err != nil {
		return engineError(err)
	}
	if err := h.keys.Delete(ctx, key.ID); err != nil {
		return err
	}
	h.changed(ctx, s)

	return nil
}

func (h *AccessKeys) changed(ctx context.Context, s *storage.Storage) {
	if err := h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, h.now()); err != nil {
		h.log.Error(err)
	}
}

// usable is the Storage as a key or the browser would find it: the first
// refusal the engine would give.
func usable(s *storage.Storage) error {
	switch {
	case s.Status == storage.StatusDeleting:
		return storage.ErrorDeleting
	case s.TeamSuspended:
		return billing.ErrorTeamSuspended
	case s.Pause != nil:
		return storage.ErrorPaused
	default:
		return nil
	}
}
