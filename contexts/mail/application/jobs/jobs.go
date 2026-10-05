// Package jobs holds the worker's periodic jobs as plain functions that run
// once, synchronously, so the integration suite drives them directly and the
// worker command loops them under a Redis lock.
package jobs

import (
	"context"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/application/handlers"
	"github.com/piggystats/nodera/logger"
)

const (
	domainBatch   = 50
	rotationBatch = 50
	rolloverBatch = 200
	spoolBatch    = 50
	dayBatch      = 500
)

// Runner is the shape the worker loops: one run, one error.
type Runner func(ctx context.Context) error

type Jobs struct {
	log        logger.ILog
	domains    *handlers.SendingDomains
	domainList sendingdomain.IService
	rollover   *handlers.Rollover
	dispatcher *handlers.Dispatcher
	protection *handlers.Protection
	deliveries *handlers.Deliveries
	retention  *handlers.Retention
	now        handlers.Clock
}

func New(
	log logger.ILog,
	domains *handlers.SendingDomains,
	domainList sendingdomain.IService,
	rollover *handlers.Rollover,
	dispatcher *handlers.Dispatcher,
	protection *handlers.Protection,
	deliveries *handlers.Deliveries,
	retention *handlers.Retention,
	now handlers.Clock,
) *Jobs {
	return &Jobs{
		log: log, domains: domains, domainList: domainList, rollover: rollover, dispatcher: dispatcher,
		protection: protection, deliveries: deliveries, retention: retention, now: now,
	}
}

// CheckDomains looks at the Sending domains whose next check has come; one
// domain's failure never holds up the others.
func (j *Jobs) CheckDomains(ctx context.Context) error {
	due, err := j.domainList.ListDue(ctx, j.now(), domainBatch)
	if err != nil {
		return err
	}
	for _, d := range due {
		if _, err := j.domains.Check(ctx, d.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("sending_domain_id", d.ID))
		}
	}

	return nil
}

// RotateKeys starts the rotations that are due and finishes the ones whose
// wait has passed (docs/adr/0011).
func (j *Jobs) RotateKeys(ctx context.Context) error {
	now := j.now()
	due, err := j.domainList.ListRotationDue(ctx, now.Add(-sendingdomain.RotationEvery), rotationBatch)
	if err != nil {
		return err
	}
	for _, d := range due {
		if err := j.domains.Rotate(ctx, d.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("sending_domain_id", d.ID))
		}
	}

	ready, err := j.domainList.ListRotationReady(ctx, now, rotationBatch)
	if err != nil {
		return err
	}
	for _, d := range ready {
		if err := j.domains.Promote(ctx, d.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("sending_domain_id", d.ID))
		}
	}

	return nil
}

func (j *Jobs) RollPeriods(ctx context.Context) error {
	due, err := j.rollover.Due(ctx, rolloverBatch)
	if err != nil {
		return err
	}
	for _, projectID := range due {
		if err := j.rollover.Carry(ctx, projectID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("project_id", projectID))
		}
	}

	return nil
}

func (j *Jobs) DispatchSpool(ctx context.Context) error {
	return j.dispatcher.DispatchDue(ctx, spoolBatch)
}

func (j *Jobs) EvaluateDays(ctx context.Context) error {
	return j.protection.EvaluateDays(ctx, dayBatch)
}

func (j *Jobs) JudgeRates(ctx context.Context) error {
	return j.protection.JudgeRates(ctx)
}

func (j *Jobs) DeliverWebhooks(ctx context.Context) error {
	return j.deliveries.Run(ctx)
}

func (j *Jobs) Expire(ctx context.Context) error {
	return j.retention.Expire(ctx)
}
