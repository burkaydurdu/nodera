package main

import (
	"context"
	"os"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/cmd/storage/app"
	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/logger"
)

// RunPause stops a Storage after a complaint and tells the Team through the
// outbox; the `outbox` command relays it.
func RunPause(ctx context.Context, conf *config.Config, rawStorageID, reason string) {
	operate(ctx, conf, rawStorageID, func(ops *app.Handlers, storageID uuid.UUID) (bool, string, string, error) {
		paused, err := ops.Operations.Pause(ctx, storageID, storage.Reason(reason))

		return paused, "storage %s is paused", "storage %s was already paused", err
	})
}

// RunUnpause lifts a Storage's Pause.
func RunUnpause(ctx context.Context, conf *config.Config, rawStorageID string) {
	operate(ctx, conf, rawStorageID, func(ops *app.Handlers, storageID uuid.UUID) (bool, string, string, error) {
		lifted, err := ops.Operations.Unpause(ctx, storageID)

		return lifted, "storage %s is open again", "storage %s was not paused", err
	})
}

func operate(ctx context.Context, conf *config.Config, rawStorageID string, fn func(h *app.Handlers, storageID uuid.UUID) (bool, string, string, error)) {
	log := logger.New(conf.Environment)

	storageID, err := uuid.Parse(rawStorageID)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()
	outboxInstance, err := newOutbox(mongoClient, newRabbitMQClient(ctx, conf, log), log)
	if err != nil {
		log.Fatal(err)
	}

	deps, pool := newDeps(ctx, conf, log, "nodera-storage-operator", outboxInstance)
	defer pool.Close()

	done, did, didNot, err := fn(app.NewHandlers(deps), storageID)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	if done {
		log.Infof(did, storageID)
	} else {
		log.Infof(didNot, storageID)
	}
}
