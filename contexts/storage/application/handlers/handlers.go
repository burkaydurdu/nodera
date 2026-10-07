// Package handlers holds the storage context's business rules: each handler
// authorizes, changes the database in one transaction, announces what
// happened after the commit (docs/adr/0001) and only then asks the engine to
// follow, so that an engine that cannot be reached never leaves the database
// and the customer disagreeing.
package handlers

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/billing"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/pkg/secrets"
)

// Clock is the time the handlers act at; the integration suite moves it.
type Clock func() time.Time

// Announcer publishes what this context tells others, through the outbox.
//
//go:generate mockgen -destination=../../mock/handlers/announcer.go -package=mockhandlers github.com/piggystats/nodera/contexts/storage/application/handlers Announcer
type Announcer interface {
	PackageStarted(ctx context.Context, project *access.ProjectAccess, s *storage.Storage) error
	PackageStopped(ctx context.Context, teamID, storageID uuid.UUID, startedAt, stoppedAt time.Time) error
	NearlyFull(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, at time.Time) error
	Full(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, at time.Time) error
	Paused(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, p storage.Pause) error
	DowngradeSkipped(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, skipped *storage.Scheduled, at time.Time) error
	StorageChanged(ctx context.Context, teamID, projectID, storageID uuid.UUID, at time.Time) error
}

// announcements are collected inside a transaction and published after its
// commit; a failure is logged, never returned, because the change they
// announce has already happened.
type announcements []func(context.Context) error

func (a *announcements) add(fn func(context.Context) error) {
	*a = append(*a, fn)
}

func (a announcements) publish(ctx context.Context, log logger.ILog) {
	for _, fn := range a {
		if err := fn(ctx); err != nil {
			log.Error(err)
		}
	}
}

// authorizer answers the caller's membership in a Project's Team when their
// Role holds the Permission (docs/adr/0005). A stranger and an unknown Project
// are the same 404; a member without the Permission gets a 403.
type authorizer struct {
	access   access.IService
	storages storage.IService
}

func (a authorizer) project(ctx context.Context, projectID uuid.UUID, permission authz.Permission) (*access.Membership, error) {
	membership, err := a.access.FindProjectMembership(ctx, projectID, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if !membership.Allows(permission) {
		return nil, access.ErrorForbidden
	}

	return membership, nil
}

// storage is a Storage of a Project the caller may act on with the Permission.
func (a authorizer) storage(ctx context.Context, projectID, storageID uuid.UUID, permission authz.Permission) (*storage.Storage, error) {
	if _, err := a.project(ctx, projectID, permission); err != nil {
		return nil, err
	}

	return a.storages.Find(ctx, projectID, storageID)
}

// requireActiveBilling is the gate on anything that would be charged.
func requireActiveBilling(ctx context.Context, billingService billing.IService, teamID uuid.UUID) error {
	status, err := billingService.StatusOf(ctx, teamID)
	if err != nil {
		return err
	}

	switch status {
	case billing.StatusActive:
		return nil
	case billing.StatusSuspended:
		return billing.ErrorTeamSuspended
	default:
		return billing.ErrorPaymentMethodRequired
	}
}

// engineError is what a customer learns of an engine that failed: that
// Storage is unavailable and the request may be repeated, or that it refused
// and repeating will not help; never why.
func engineError(err error) error {
	if errors.Is(err, engine.ErrUnavailable) {
		return storage.ErrorUnavailable.Copy().SetError(err)
	}

	return storage.ErrorEngineRefused.Copy().SetError(err)
}

// accounts is each Project's account in the engine, made the first time the
// Project opens a Storage and kept with its root key sealed.
type accounts struct {
	storages storage.IService
	engine   Engine
	sealer   *secrets.Sealer
}

// ensure answers the Project's account, making it if the Project has none.
// Two requests racing to open a Project's first Storage both ask the engine,
// which answers the same account; the first row written wins.
func (a *accounts) ensure(ctx context.Context, projectID uuid.UUID) (*engine.Account, error) {
	account, err := a.find(ctx, projectID)
	if err == nil || !errors.Is(err, errNoAccount) {
		return account, err
	}

	created, err := a.engine.CreateAccount(ctx, projectID)
	if err != nil {
		return nil, engineError(err)
	}
	sealed, err := a.sealer.Seal([]byte(created.SecretKey))
	if err != nil {
		return nil, err
	}
	if err := a.storages.SaveAccount(ctx, &storage.Account{ProjectID: projectID, AccountID: created.ID, AccessKey: created.AccessKey, SealedSecret: sealed}); err != nil {
		return nil, err
	}

	return a.find(ctx, projectID)
}

var errNoAccount = errors.New("the project has no account yet")

func (a *accounts) find(ctx context.Context, projectID uuid.UUID) (*engine.Account, error) {
	row, err := a.storages.FindAccount(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errNoAccount
	}
	secret, err := a.sealer.Open(row.SealedSecret)
	if err != nil {
		return nil, err
	}

	return &engine.Account{ID: row.AccountID, AccessKey: row.AccessKey, SecretKey: string(secret)}, nil
}

// Addresses is where a Storage is reached: the Region's S3 endpoint and the
// bucket's own URL under it, as a subdomain or, in development, a path.
type Addresses struct {
	endpoint  string
	pathStyle bool
}

func NewAddresses(endpoint string, pathStyle bool) Addresses {
	return Addresses{endpoint: strings.TrimRight(endpoint, "/"), pathStyle: pathStyle}
}

func (a Addresses) Endpoint(_ string) string {
	return a.endpoint
}

func (a Addresses) BucketURL(_, bucket string) string {
	if a.pathStyle {
		return a.endpoint + "/" + bucket
	}
	u, err := url.Parse(a.endpoint)
	if err != nil {
		return a.endpoint + "/" + bucket
	}
	u.Host = bucket + "." + u.Host

	return u.String()
}

// Accounts is the Projects' accounts helper every handler that reaches the
// engine shares.
type Accounts = accounts

func NewAccounts(storageService storage.IService, eng Engine, sealer *secrets.Sealer) *Accounts {
	return &accounts{storages: storageService, engine: eng, sealer: sealer}
}

// Syncer is the engine follower the handlers and the reconcile job share.
type Syncer = syncer

func NewSyncer(log logger.ILog, storageService storage.IService, accounts *Accounts, eng Engine, transactions postgresql.ITransactionManager) *Syncer {
	return &syncer{log: log, storages: storageService, accounts: accounts, engine: eng, transactions: transactions}
}
