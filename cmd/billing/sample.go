package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/recover"
)

// RunSamplePaymentMethod saves a Payment method no Payment provider holds for
// a Team. Dev has no provider credentials, so the web app's card form cannot
// open there, and a Team without a Payment method can neither create a
// Droplet nor choose a Mail Package. It only writes the outbox; the `outbox`
// command relays the Billing status change to the other contexts.
func RunSamplePaymentMethod(ctx context.Context, conf *config.Config, rawTeamID string) {
	log := logger.New(conf.Environment)
	defer recover.Init(log)
	if !conf.IsDev() {
		log.Fatal(fmt.Errorf("sample payment methods are for dev only, not %q", conf.Environment))
	}
	teamID, err := uuid.Parse(strings.TrimSpace(rawTeamID))
	if err != nil {
		log.Fatal(fmt.Errorf("--team must be a Team id: %w", err))
	}

	deps := newDependencies(ctx, conf, log, "nodera-billing-sample")
	defer deps.Pool.Close()

	m, status, err := app.SamplePaymentMethod(ctx, deps, teamID)
	switch {
	case errors.Is(err, paymentmethod.ErrorProfileRequired):
		log.Fatal(fmt.Errorf("%w: fill in the Team's Billing profile on the web app's Billing page first", err))
	case errors.Is(err, access.ErrorTeamNotFound):
		log.Fatal(fmt.Errorf("%w: the id is on the web app's Team page; a Team older than the billing context arrives with `go run ./cmd/customer replay-projects --env=dev`", err))
	case err != nil:
		log.Fatal(err)
	}
	log.Infof("saved a sample Payment method, %s ending %s (default: %t), for team %s; its Billing status is %s",
		m.Brand, m.Last4, m.IsDefault, teamID, status)
}
