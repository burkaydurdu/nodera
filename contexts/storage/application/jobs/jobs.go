// Package jobs holds the worker's periodic jobs as plain functions that run
// once, synchronously, so the integration suite drives them directly and the
// worker command loops them under a Redis lock.
package jobs

import (
	"context"

	"github.com/piggystats/nodera/contexts/storage/application/handlers"
	"github.com/piggystats/nodera/logger"
)

const (
	measureBatch   = 200
	rolloverBatch  = 200
	purgeBatch     = 50
	reconcileBatch = 200
)

// Runner is the shape the worker loops: one run, one error.
type Runner func(ctx context.Context) error

type Jobs struct {
	log        logger.ILog
	measurer   *handlers.Measurer
	rollover   *handlers.Rollover
	purger     *handlers.Purger
	reconciler *handlers.Reconciler
}

func New(log logger.ILog, measurer *handlers.Measurer, rollover *handlers.Rollover, purger *handlers.Purger, reconciler *handlers.Reconciler) *Jobs {
	return &Jobs{log: log, measurer: measurer, rollover: rollover, purger: purger, reconciler: reconciler}
}

// Measure reads what every open Storage holds.
func (j *Jobs) Measure(ctx context.Context) error {
	return j.measurer.Each(ctx, measureBatch, j.measurer.Measure)
}

// Traffic reads every open Storage's usage log.
func (j *Jobs) Traffic(ctx context.Context) error {
	return j.measurer.Each(ctx, measureBatch, j.measurer.Traffic)
}

// RollPeriods applies the smaller Packages due at the Period's start.
func (j *Jobs) RollPeriods(ctx context.Context) error {
	due, err := j.rollover.Due(ctx, rolloverBatch)
	if err != nil {
		return err
	}
	for _, id := range due {
		if err := j.rollover.Carry(ctx, id); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", id))
		}
	}

	return nil
}

// Purge removes the files and buckets of the Storages being deleted.
func (j *Jobs) Purge(ctx context.Context) error {
	due, err := j.purger.Due(ctx, purgeBatch)
	if err != nil {
		return err
	}
	for _, s := range due {
		if err := j.purger.Purge(ctx, s); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", s.ID))
		}
	}

	return nil
}

// Reconcile brings the engine to what the database says, and frees the
// reservations no request is finishing.
func (j *Jobs) Reconcile(ctx context.Context) error {
	abandoned, err := j.reconciler.Abandoned(ctx, reconcileBatch)
	if err != nil {
		return err
	}
	for _, s := range abandoned {
		if err := j.reconciler.Abandon(ctx, s); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", s.ID))
		}
	}

	due, err := j.reconciler.OutOfSync(ctx, reconcileBatch)
	if err != nil {
		return err
	}
	for _, id := range due {
		if err := j.reconciler.Apply(ctx, id); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", id))
		}
	}

	return nil
}
