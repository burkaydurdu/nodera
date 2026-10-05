package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

const (
	sampleBrand = "visa"
	sampleLast4 = "4242"
	// sampleRefPrefix marks references no Payment provider issued.
	sampleRefPrefix = "sample_"
	// sampleYears is how far off the sample expires; a known expiry keeps
	// the expiry check from asking a provider about it.
	sampleYears = 5
)

// SamplePaymentMethod saves a Payment method no Payment provider holds, for a
// dev environment without provider credentials, where the hosted card form
// cannot open. It keeps a verified one's rules: the Billing profile's country
// routes it, the Team's first becomes the default and makes the Team active,
// and that change is published after the commit. A charge on it never
// reaches a provider, so its Payment stays pending.
type SamplePaymentMethod struct {
	log          logger.ILog
	access       access.IService
	profiles     profile.IService
	methods      paymentmethod.IService
	statuses     billingstatus.IService
	transactions postgresql.ITransactionManager
	published    StatusPublisher
	now          func() time.Time
}

func NewSamplePaymentMethod(
	log logger.ILog,
	accessService access.IService,
	profiles profile.IService,
	methods paymentmethod.IService,
	statuses billingstatus.IService,
	transactions postgresql.ITransactionManager,
	published StatusPublisher,
	now func() time.Time,
) *SamplePaymentMethod {
	return &SamplePaymentMethod{
		log: log, access: accessService, profiles: profiles, methods: methods, statuses: statuses,
		transactions: transactions, published: published, now: now,
	}
}

// Run answers the saved Payment method and the Team's Billing status after
// it: a suspended Team stays suspended until its debt is paid.
func (h *SamplePaymentMethod) Run(ctx context.Context, teamID uuid.UUID) (*paymentmethod.PaymentMethod, billingstatus.Status, error) {
	team, err := h.access.FindTeam(ctx, teamID)
	if err != nil {
		return nil, "", err
	}
	if team == nil {
		return nil, "", access.ErrorTeamNotFound
	}

	var (
		saved     *paymentmethod.PaymentMethod
		status    billingstatus.Status
		activated *billingstatus.TeamStatus
		at        = h.now().UTC()
	)
	err = inTransaction(ctx, h.transactions, func(ctx context.Context) error {
		current, err := h.statuses.FindForUpdate(ctx, teamID, at)
		if err != nil {
			return err
		}
		p, err := h.profiles.FindByTeamID(ctx, teamID)
		if errors.Is(err, profile.ErrorNotFound) {
			return paymentmethod.ErrorProfileRequired
		}
		if err != nil {
			return err
		}
		route, err := h.methods.FindRoute(ctx, p.Country)
		if err != nil {
			return err
		}

		// No User adds a sample, so CreatedBy stays the nil id.
		ref := sampleRefPrefix + uuid.NewString()
		saved = &paymentmethod.PaymentMethod{
			TeamID: teamID, Provider: route.Provider, Currency: route.Currency,
			ProviderCustomerRef: ref, ProviderCardRef: ref,
			Brand: sampleBrand, Last4: sampleLast4, ExpMonth: int(at.Month()), ExpYear: at.Year() + sampleYears,
		}
		if activated, err = addPaymentMethod(ctx, h.methods, h.statuses, saved, at); err != nil {
			return err
		}
		status = current.Status

		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if activated != nil {
		publishStatus(ctx, h.log, h.published, activated)
		status = activated.Status
	}

	return saved, status, nil
}
