// Package jobs holds the worker's periodic jobs as plain functions that run
// once, synchronously, so the integration suite drives them directly and the
// worker command loops them under a Redis lock.
package jobs

import (
	"context"

	"github.com/piggystats/nodera/contexts/kubernetes/application/handlers"
	"github.com/piggystats/nodera/logger"
)

const (
	provisionBatch = 100
	requestBatch   = 200
	watchBatch     = 200
	releaseBatch   = 200
	deleteBatch    = 100
)

// Runner is the shape the worker loops: one run, one error.
type Runner func(ctx context.Context) error

type Jobs struct {
	log         logger.ILog
	provisioner *handlers.Provisioner
	watcher     *handlers.Watcher
	releaser    *handlers.Releaser
	deleter     *handlers.Deleter
}

func New(log logger.ILog, provisioner *handlers.Provisioner, watcher *handlers.Watcher, releaser *handlers.Releaser, deleter *handlers.Deleter) *Jobs {
	return &Jobs{log: log, provisioner: provisioner, watcher: watcher, releaser: releaser, deleter: deleter}
}

// Provision brings every pending Control plane one step further and asks the
// droplet context for the Nodes whose Cluster can now be joined.
func (j *Jobs) Provision(ctx context.Context) error {
	pending, err := j.provisioner.Pending(ctx, provisionBatch)
	if err != nil {
		return err
	}
	for _, c := range pending {
		if err := j.provisioner.Advance(ctx, c.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
		}
	}

	awaiting, err := j.provisioner.Awaiting(ctx, requestBatch)
	if err != nil {
		return err
	}
	for _, n := range awaiting {
		if err := j.provisioner.Request(ctx, n); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("node_id", n.ID))
		}
	}

	return nil
}

// Watch reads every live Cluster's Nodes from its API server.
func (j *Jobs) Watch(ctx context.Context) error {
	live, err := j.watcher.Live(ctx, watchBatch)
	if err != nil {
		return err
	}
	for _, c := range live {
		if err := j.watcher.Watch(ctx, c.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
		}
	}

	return nil
}

// Release finishes every Node's Release that is due.
func (j *Jobs) Release(ctx context.Context) error {
	due, err := j.releaser.Due(ctx, releaseBatch)
	if err != nil {
		return err
	}
	for _, n := range due {
		if err := j.releaser.Release(ctx, n); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("node_id", n.ID))
		}
	}

	return nil
}

// Delete ends every deleting Cluster whose Nodes are gone.
func (j *Jobs) Delete(ctx context.Context) error {
	due, err := j.deleter.Due(ctx, deleteBatch)
	if err != nil {
		return err
	}
	for _, c := range due {
		if err := j.deleter.Finish(ctx, c.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
		}
	}

	return nil
}
