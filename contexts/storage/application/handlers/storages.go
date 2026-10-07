package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/accesskey"
	"github.com/piggystats/nodera/contexts/storage/aggregates/billing"
	"github.com/piggystats/nodera/contexts/storage/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// Storages opens, shows, re-Packages and deletes a Project's Storages.
type Storages struct {
	authorizer
	log          logger.ILog
	billing      billing.IService
	catalog      catalog.IService
	keys         accesskey.IService
	transactions postgresql.ITransactionManager
	announcer    Announcer
	engine       Engine
	accounts     *accounts
	sync         *syncer
	addresses    Addresses
	now          Clock
}

func NewStorages(
	log logger.ILog,
	accessService access.IService,
	billingService billing.IService,
	catalogService catalog.IService,
	storageService storage.IService,
	keyService accesskey.IService,
	transactions postgresql.ITransactionManager,
	announcer Announcer,
	eng Engine,
	accounts *accounts,
	sync *syncer,
	addresses Addresses,
	now Clock,
) *Storages {
	return &Storages{
		authorizer: authorizer{access: accessService, storages: storageService},
		log:        log, billing: billingService, catalog: catalogService, keys: keyService, transactions: transactions,
		announcer: announcer, engine: eng, accounts: accounts, sync: sync, addresses: addresses, now: now,
	}
}

func (h *Storages) List(ctx context.Context, req *storage.ListRequest) ([]*dto.Storage, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.project(ctx, projectID, authz.StorageRead)
	if err != nil {
		return nil, err
	}
	list, err := h.storages.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	status, err := h.billing.StatusOf(ctx, membership.TeamID)
	if err != nil {
		return nil, err
	}

	return dto.ToStorages(list, string(status), h.addresses), nil
}

func (h *Storages) Get(ctx context.Context, req *storage.GetRequest) (*dto.Storage, error) {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageRead)
	if err != nil {
		return nil, err
	}

	return h.show(ctx, s)
}

func (h *Storages) show(ctx context.Context, s *storage.Storage) (*dto.Storage, error) {
	status, err := h.billing.StatusOf(ctx, s.TeamID)
	if err != nil {
		return nil, err
	}

	return dto.ToStorage(s, string(status), h.addresses), nil
}

// Open reserves the Bucket name, has the engine make the bucket and only then
// starts the Package: a Storage is usable the moment the request returns, and
// an engine that could not be reached leaves nothing behind.
func (h *Storages) Open(ctx context.Context, req *storage.OpenRequest) (*dto.Storage, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.project(ctx, projectID, authz.StorageWrite)
	if err != nil {
		return nil, err
	}
	req.Normalize()
	if err := storage.ValidateBucketName(req.BucketName); err != nil {
		return nil, err
	}
	region, err := h.catalog.FindRegion(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	pkg, err := h.catalog.FindPackage(ctx, req.Package)
	if err != nil {
		return nil, err
	}
	if err := requireActiveBilling(ctx, h.billing, membership.TeamID); err != nil {
		return nil, err
	}
	count, err := h.storages.CountByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if count >= storage.MaxPerProject {
		return nil, storage.ErrorTooManyStorages
	}
	project, err := h.access.FindProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	now := h.now()
	s := &storage.Storage{
		ID: uuid.New(), ProjectID: projectID, TeamID: membership.TeamID, Region: region.Code, BucketName: req.BucketName,
		Status:    storage.StatusOpening,
		Package:   storage.Package{Code: pkg.Code, SizeBytes: pkg.SizeBytes, MonthlyPrice: pkg.MonthlyPrice},
		CreatedBy: contextutil.CurrentUserID(ctx), CreatedAt: now,
	}
	if err := h.storages.Reserve(ctx, s); err != nil {
		return nil, err
	}

	if err := h.open(ctx, s); err != nil {
		h.giveBack(ctx, s)

		return nil, err
	}

	startedAt := h.now()
	if err := h.storages.Activate(ctx, s.ID, startedAt); err != nil {
		return nil, err
	}
	s.Status, s.Package.StartedAt, s.EngineSizeBytes = storage.StatusActive, startedAt, s.Package.SizeBytes

	var out announcements
	out.add(func(ctx context.Context) error { return h.announcer.PackageStarted(ctx, project, s) })
	out.add(func(ctx context.Context) error {
		return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, startedAt)
	})
	out.publish(ctx, h.log)

	return h.show(ctx, s)
}

// giveBack frees a reservation the engine could not finish. The engine may
// have made the bucket before failing, so it is asked to remove it first; when
// it cannot even be asked, the reservation stays for the reconcile job, which
// removes both once nobody can be finishing it.
func (h *Storages) giveBack(ctx context.Context, s *storage.Storage) {
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err == nil {
		err = h.engine.DeleteBucket(ctx, account, s.BucketName)
	}
	if err != nil && !isNoAccount(err) {
		h.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", s.ID))

		return
	}
	if err := h.storages.Release(ctx, s.ID); err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", s.ID))
	}
}

func (h *Storages) open(ctx context.Context, s *storage.Storage) error {
	account, err := h.accounts.ensure(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if err := h.engine.OpenBucket(ctx, account, s.BucketName, s.Package.SizeBytes); err != nil {
		return engineError(err)
	}

	return nil
}

// ChoosePackage moves a Storage to a larger Package at once, schedules a
// smaller one for the next Period if the Storage fits it, or undoes a
// scheduled move. Every choice follows the Team's Billing status.
func (h *Storages) ChoosePackage(ctx context.Context, req *storage.ChoosePackageRequest) (*dto.Storage, error) {
	projectID, storageID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID)
	membership, err := h.project(ctx, projectID, authz.StorageWrite)
	if err != nil {
		return nil, err
	}
	req.Normalize()
	target, err := h.catalog.FindPackage(ctx, req.Code)
	if err != nil {
		return nil, err
	}

	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	s, err := h.storages.FindForUpdate(txCtx, projectID, storageID)
	if err != nil {
		return nil, err
	}
	if err := h.choose(txCtx, s, membership.TeamID, target, &out); err != nil {
		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}
	out.publish(ctx, h.log)
	h.sync.follow(ctx, s.ID)

	return h.show(ctx, s)
}

func (h *Storages) choose(ctx context.Context, s *storage.Storage, teamID uuid.UUID, target *catalog.Package, out *announcements) error {
	if s.Status == storage.StatusDeleting {
		return storage.ErrorDeleting
	}
	kind := storage.Decide(s.Package, s.Scheduled, target.Code, target.SizeBytes)
	if kind == storage.ChangeNothing {
		return nil
	}
	if err := requireActiveBilling(ctx, h.billing, teamID); err != nil {
		return err
	}
	now := h.now()

	switch kind {
	case storage.ChangeUpgrade:
		project, err := h.access.FindProject(ctx, s.ProjectID)
		if err != nil {
			return err
		}
		old := s.Package
		s.Hold(storage.Package{Code: target.Code, SizeBytes: target.SizeBytes, MonthlyPrice: target.MonthlyPrice, StartedAt: now})
		held := *s
		out.add(func(ctx context.Context) error {
			return h.announcer.PackageStopped(ctx, s.TeamID, s.ID, old.StartedAt, now)
		})
		out.add(func(ctx context.Context) error { return h.announcer.PackageStarted(ctx, project, &held) })
	case storage.ChangeScheduled:
		if !s.Fits(target.SizeBytes) {
			return storage.ErrorDoesNotFit
		}
		s.Scheduled = &storage.Scheduled{Code: target.Code, SizeBytes: target.SizeBytes, MonthlyPrice: target.MonthlyPrice, At: now}
	case storage.ChangeUnscheduled:
		s.Scheduled = nil
	}

	if err := h.storages.Save(ctx, s); err != nil {
		return err
	}
	out.add(func(ctx context.Context) error {
		return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, now)
	})

	return nil
}

// Delete ends the Storage at once for billing and its Access keys, and
// leaves the files to the purge job. It is allowed whatever the Team's
// Billing status and whether or not the Storage is Paused.
func (h *Storages) Delete(ctx context.Context, req *storage.DeleteRequest) (*dto.Storage, error) {
	projectID, storageID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID)
	if _, err := h.project(ctx, projectID, authz.StorageWrite); err != nil {
		return nil, err
	}
	req.Normalize()

	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	s, err := h.storages.FindForUpdate(txCtx, projectID, storageID)
	if err != nil {
		return nil, err
	}
	if s.BucketName != req.BucketName {
		return nil, storage.ErrorNameMismatch
	}
	if s.Status == storage.StatusDeleting {
		return nil, storage.ErrorDeleting
	}
	if err := h.markDeleting(txCtx, s, h.now(), &out); err != nil {
		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}
	out.publish(ctx, h.log)
	h.stopNow(ctx, s)

	return h.show(ctx, s)
}

// markDeleting runs inside the caller's transaction; stoppedAt is when the
// Package ends, never before it started.
func (h *Storages) markDeleting(ctx context.Context, s *storage.Storage, stoppedAt time.Time, out *announcements) error {
	if stoppedAt.Before(s.Package.StartedAt) {
		stoppedAt = s.Package.StartedAt
	}
	s.Status, s.DeletedAt, s.Scheduled = storage.StatusDeleting, &stoppedAt, nil
	if err := h.keys.DeleteByStorage(ctx, s.ID); err != nil {
		return err
	}
	if err := h.storages.Save(ctx, s); err != nil {
		return err
	}

	startedAt := s.Package.StartedAt
	out.add(func(ctx context.Context) error {
		return h.announcer.PackageStopped(ctx, s.TeamID, s.ID, startedAt, stoppedAt)
	})
	out.add(func(ctx context.Context) error {
		return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, stoppedAt)
	})

	return nil
}

// stopNow makes a deleted Storage's keys stop before the purge job gets to
// its files; the purge removes everything anyway, so a failure only logs.
func (h *Storages) stopNow(ctx context.Context, s *storage.Storage) {
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err == nil {
		err = h.engine.Stop(ctx, account, s.BucketName)
	}
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", s.ID))
	}
}
