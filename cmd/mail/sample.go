package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/logger"
)

// RunVerifySendingDomain verifies a Sending domain as if its DNS records were
// published, so dev can send: its zone is not public and the real check never
// passes there. The change goes through the outbox like any other; the
// `outbox` command relays it to the web app and the notification context.
func RunVerifySendingDomain(ctx context.Context, conf *config.Config, rawSendingDomainID string) {
	log := logger.New(conf.Environment)
	if !conf.IsDev() {
		log.Fatal(fmt.Errorf("verifying a sending domain without DNS is for dev only, not %q", conf.Environment))
	}
	sendingDomainID, err := uuid.Parse(strings.TrimSpace(rawSendingDomainID))
	if err != nil {
		log.Fatal(fmt.Errorf("--sending-domain must be a Sending domain id: %w", err))
	}

	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()
	outboxInstance, err := newOutbox(mongoClient, newRabbitMQClient(ctx, conf, log), log)
	if err != nil {
		log.Fatal(err)
	}

	deps, pool := newDeps(ctx, conf, log, "nodera-mail-sample", outboxInstance)
	defer pool.Close()

	checked, err := app.VerifyAsPublished(ctx, deps, sendingDomainID)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("sending domain %s of project %s is %s until the daily check reads DNS again", checked.Name, checked.ProjectID, checked.Status)
}
