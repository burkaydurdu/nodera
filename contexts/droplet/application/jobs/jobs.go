// Package jobs holds the worker's two periodic jobs as plain functions that
// run once, synchronously, so the integration suite drives them directly and
// the worker command loops them under a Redis lock.
package jobs

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/billing"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// ProvisioningTimeout is how long a Droplet may stay provisioning before it
// is failed: the Provider did not deliver.
const ProvisioningTimeout = 60 * time.Minute

type ProviderRegistry interface {
	Get(code string) (providers.Provider, error)
}

// BillingMeter announces the usage boundaries a poll made a Droplet cross;
// it is called after the commit (docs/adr/0001).
type BillingMeter interface {
	Publish(ctx context.Context, d *droplet.Droplet, events []droplet.BillingEvent)
}

// StatusAnnouncer announces a change of status a poll made; it is called
// after the commit and does nothing when the status did not move.
type StatusAnnouncer interface {
	Announce(ctx context.Context, d *droplet.Droplet, from *droplet.Status)
}

type Jobs struct {
	log          logger.ILog
	providers    provider.IService
	catalog      catalog.IService
	droplets     droplet.IService
	billing      billing.IService
	registry     ProviderRegistry
	transactions postgresql.ITransactionManager
	meter        BillingMeter
	status       StatusAnnouncer
	now          func() time.Time
}

func New(
	log logger.ILog,
	providerService provider.IService,
	catalogService catalog.IService,
	droplets droplet.IService,
	billingService billing.IService,
	registry ProviderRegistry,
	transactions postgresql.ITransactionManager,
	meter BillingMeter,
	status StatusAnnouncer,
) *Jobs {
	return &Jobs{
		log:          log,
		providers:    providerService,
		catalog:      catalogService,
		droplets:     droplets,
		billing:      billingService,
		registry:     registry,
		transactions: transactions,
		meter:        meter,
		status:       status,
		now:          time.Now,
	}
}

// WithClock swaps the clock, so a test can age a Droplet past the timeout.
func (j *Jobs) WithClock(now func() time.Time) *Jobs {
	j.now = now

	return j
}

// Runner is the shape the worker loops: one run, one error.
type Runner func(ctx context.Context) error
