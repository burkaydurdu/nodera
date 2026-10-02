package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/recover"
)

// RunReplayStatus queues TeamBillingStatusChanged for every Team's current
// status. It only writes the outbox; the `outbox` command relays them.
func RunReplayStatus(ctx context.Context, conf *config.Config) {
	log := logger.New(conf.Environment)
	defer recover.Init(log)

	deps := newDependencies(ctx, conf, log, "nodera-billing-replay")
	defer deps.Pool.Close()

	count, err := app.ReplayStatuses(ctx, deps)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("republished the billing status of %d teams", count)
}
