package main

import (
	"context"
	"os"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/logger"
)

// RunUnpause lifts a Project's Pause and tells the web app through the
// outbox; the `outbox` command relays it.
func RunUnpause(ctx context.Context, conf *config.Config, rawProjectID string) {
	log := logger.New(conf.Environment)

	projectID, err := uuid.Parse(rawProjectID)
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

	deps, pool := newDeps(ctx, conf, log, "nodera-mail-unpause", outboxInstance)
	defer pool.Close()

	lifted, err := app.NewHandlers(deps).Operations.Unpause(ctx, projectID)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	if !lifted {
		log.Infof("project %s was not paused", projectID)

		return
	}
	log.Infof("project %s can send again", projectID)
}
