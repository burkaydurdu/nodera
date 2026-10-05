// Package handlers holds the mail context's business rules: each handler
// authorizes, orchestrates the aggregates in one transaction and announces
// what happened only after the commit (docs/adr/0001).
package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/paging"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/utils/str"
)

// Clock is the time the handlers act at; the integration suite moves it.
type Clock func() time.Time

// Announcer publishes what this context tells others, through the outbox.
//
//go:generate mockgen -destination=../../mock/handlers/announcer.go -package=mockhandlers github.com/piggystats/nodera/contexts/mail/application/handlers Announcer
type Announcer interface {
	PackageStarted(ctx context.Context, project *access.ProjectAccess, h *packages.HeldPackage) error
	PackageStopped(ctx context.Context, teamID, projectID uuid.UUID, startedAt, stoppedAt time.Time) error
	DomainVerified(ctx context.Context, project *access.ProjectAccess, d *sendingdomain.SendingDomain, at time.Time) error
	DomainUnverified(ctx context.Context, project *access.ProjectAccess, d *sendingdomain.SendingDomain, at time.Time) error
	PackageNearLimit(ctx context.Context, project *access.ProjectAccess, code string, period *packages.Period, at time.Time) error
	PackageExhausted(ctx context.Context, project *access.ProjectAccess, code string, period *packages.Period, at time.Time) error
	RatesWarning(ctx context.Context, project *access.ProjectAccess, w limits.Window, at time.Time) error
	SendingPaused(ctx context.Context, project *access.ProjectAccess, reason limits.Reason, w limits.Window, at time.Time) error
	DomainChanged(ctx context.Context, teamID, projectID, domainID uuid.UUID, status sendingdomain.Status, at time.Time) error
	SendingChanged(ctx context.Context, teamID, projectID uuid.UUID, at time.Time) error
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
	access access.IService
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

// pageBefore reads the cursor a list handed out; a bad one is the listed
// aggregate's own error.
func pageBefore(raw string, invalid rerror.Error) (*paging.Cursor, error) {
	if str.IsEmpty(raw) {
		return nil, nil
	}
	cursor, ok := paging.Decode(raw)
	if !ok {
		return nil, invalid
	}

	return cursor, nil
}

func nextCursor(next *paging.Cursor) *string {
	if next == nil {
		return nil
	}
	encoded := next.Encode()

	return &encoded
}

// retryAfter is the wait a 429 tells the client, in whole seconds.
func retryAfter(now, until time.Time) int {
	return max(int(until.Sub(now).Seconds())+1, 1)
}
