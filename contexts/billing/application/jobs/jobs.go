// Package jobs holds the billing worker's periodic jobs as plain functions
// that run once, synchronously, so the integration suite drives them
// directly and the worker command loops them under a Redis lock.
package jobs

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/logger"
)

// Runner is the shape the worker loops: one run, one error.
type Runner func(ctx context.Context) error

// Collection is the business side the collection jobs run: the handlers own
// the rules, the jobs only decide when and for which instant.
//
//go:generate mockgen -destination=../../mock/jobs/collection.go -package=mockjobs github.com/piggystats/nodera/contexts/billing/application/jobs Collection
type Collection interface {
	ClosePeriod(ctx context.Context, period usage.Period, at time.Time) error
	CheckThresholds(ctx context.Context, at time.Time) error
	RunDunning(ctx context.Context, at time.Time) error
	Reconcile(ctx context.Context, at time.Time) error
	Sweep(ctx context.Context, at time.Time) error
	WarnExpiringCards(ctx context.Context, at time.Time) error
}

type Jobs struct {
	log           logger.ILog
	exchangeRates exchangerate.IService
	fetcher       exchangerate.Fetcher
	collection    Collection
	now           func() time.Time
}

func New(log logger.ILog, exchangeRates exchangerate.IService, fetcher exchangerate.Fetcher, collection Collection, now func() time.Time) *Jobs {
	if now == nil {
		now = time.Now
	}

	return &Jobs{log: log, exchangeRates: exchangeRates, fetcher: fetcher, collection: collection, now: now}
}

// WithClock swaps the clock, so a test can move the day.
func (j *Jobs) WithClock(now func() time.Time) *Jobs {
	j.now = now

	return j
}

// FetchExchangeRate stores the central bank's latest bulletin until today
// has a rate; once it has, a run does nothing. The worker loops it hourly, so
// a bulletin published in the afternoon, or none at all on a holiday, costs
// one request an hour and an Invoice issued meanwhile falls back to the
// latest earlier rate.
func (j *Jobs) FetchExchangeRate(ctx context.Context) error {
	today := exchangerate.Day(j.now())
	covered, err := j.exchangeRates.Exists(ctx, today)
	if err != nil {
		return err
	}
	if covered {
		return nil
	}

	rate, err := j.fetcher.Fetch(ctx)
	if err != nil {
		return err
	}
	if err := j.exchangeRates.Store(ctx, rate); err != nil {
		return err
	}
	if rate.IsFallbackFor(today) {
		j.log.InfoWithFields("exchange rate bulletin is not today's yet", logger.NewFields().
			Add("today", today.Format(time.DateOnly)).Add("bulletin", rate.RateDate.Format(time.DateOnly)))
	}

	return nil
}

// ClosePeriods issues the Period Invoice of every Team with usage in the
// Period that ended last. The worker runs it every few minutes, not once at
// midnight; issuing is idempotent per Team and Period.
func (j *Jobs) ClosePeriods(ctx context.Context) error {
	at := j.now().UTC()

	return j.collection.ClosePeriod(ctx, usage.PeriodOf(at).Previous(), at)
}

// CheckThresholds issues a Threshold Invoice for every Team whose unbilled
// usage in the open Period reached its Threshold.
func (j *Jobs) CheckThresholds(ctx context.Context) error {
	return j.collection.CheckThresholds(ctx, j.now().UTC())
}

// RunDunning retries failed Payments on their schedule, suspends, warns and
// finally requests the deletion of a Team's Droplets.
func (j *Jobs) RunDunning(ctx context.Context) error {
	return j.collection.RunDunning(ctx, j.now().UTC())
}

// ReconcilePayments settles stored webhooks and pending Payments against the
// providers' own records.
func (j *Jobs) ReconcilePayments(ctx context.Context) error {
	return j.collection.Reconcile(ctx, j.now().UTC())
}

// SweepInvoices fails lapsed confirmation links and charges issued
// Invoices nobody charged.
func (j *Jobs) SweepInvoices(ctx context.Context) error {
	return j.collection.Sweep(ctx, j.now().UTC())
}

// CardExpiring warns a Team whose default card stops working within 30
// days, once per card.
func (j *Jobs) CardExpiring(ctx context.Context) error {
	return j.collection.WarnExpiringCards(ctx, j.now().UTC())
}
