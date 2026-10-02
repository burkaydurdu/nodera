// Package handlers holds the billing context's business rules and
// orchestrates its aggregate services, the Payment providers and the outbox.
package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// ProviderRegistry is the slice of the adapter registry the handlers consume.
//
//go:generate mockgen -destination=../../mock/handlers/handlers.go -package=mockhandlers github.com/piggystats/nodera/contexts/billing/application/handlers ProviderRegistry,StatusPublisher,UnpaidInvoices,Notifier,DeletionRequester,Renderer,ObjectStore,EInvoiceIssuer
type ProviderRegistry interface {
	Get(code string) (providers.Provider, error)
}

// StatusPublisher announces a Billing status change to the droplet context;
// it runs after the change has committed (docs/adr/0001).
type StatusPublisher interface {
	Publish(ctx context.Context, teamID uuid.UUID, status string, changedAt time.Time) error
}

// UnpaidInvoices answers whether a Team still owes an issued or
// payment_failed Invoice; the invoice service implements it.
type UnpaidInvoices interface {
	UnpaidExists(ctx context.Context, teamID uuid.UUID) (bool, error)
}

// Authorizer answers the caller's membership in a Team when their Role holds
// the Permission (docs/adr/0005). A stranger and an unknown Team are the same
// 404; a member without the Permission learns the Team exists and gets a 403.
type Authorizer struct {
	access access.IService
}

func NewAuthorizer(accessService access.IService) *Authorizer {
	return &Authorizer{access: accessService}
}

func (a *Authorizer) Authorize(ctx context.Context, teamID uuid.UUID, permission authz.Permission) (*access.Membership, error) {
	membership, err := a.access.FindTeamMembership(ctx, teamID, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if membership == nil {
		return nil, access.ErrorTeamNotFound
	}
	if !membership.Allows(permission) {
		return nil, access.ErrorForbidden
	}

	return membership, nil
}

// inTransaction runs fn in one transaction riding the context; fn's error
// rolls it back.
func inTransaction(ctx context.Context, transactions postgresql.ITransactionManager, fn func(ctx context.Context) error) error {
	txCtx, err := transactions.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(txCtx); err != nil {
		_ = transactions.Rollback(txCtx)

		return err
	}

	return transactions.Commit(txCtx)
}
