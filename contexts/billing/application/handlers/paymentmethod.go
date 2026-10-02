package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

const (
	// cardCompletePath is the web app page the provider sends the browser
	// back to (contract: {front}/billing/cards/complete?verification=<id>).
	cardCompletePath = "/billing/cards/complete?verification=%s"
	// reasonCountryChanged fails a verification started under the old
	// billing country: its provider no longer matches the Team's route.
	reasonCountryChanged = "billing_country_changed"
)

type PaymentMethod struct {
	log          logger.ILog
	authorizer   *Authorizer
	access       access.IService
	profiles     profile.IService
	methods      paymentmethod.IService
	statuses     billingstatus.IService
	registry     ProviderRegistry
	transactions postgresql.ITransactionManager
	published    StatusPublisher
	invoices     UnpaidInvoices
	frontURL     string
	now          func() time.Time
}

func NewPaymentMethod(
	log logger.ILog,
	authorizer *Authorizer,
	accessService access.IService,
	profiles profile.IService,
	methods paymentmethod.IService,
	statuses billingstatus.IService,
	registry ProviderRegistry,
	transactions postgresql.ITransactionManager,
	published StatusPublisher,
	invoices UnpaidInvoices,
	frontURL string,
	now func() time.Time,
) *PaymentMethod {
	return &PaymentMethod{
		log: log, authorizer: authorizer, access: accessService, profiles: profiles, methods: methods, statuses: statuses,
		registry: registry, transactions: transactions, published: published, invoices: invoices, frontURL: frontURL, now: now,
	}
}

func (h *PaymentMethod) List(ctx context.Context, req *paymentmethod.ListRequest) ([]*dto.PaymentMethod, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingRead); err != nil {
		return nil, err
	}

	methods, err := h.methods.FindByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return dto.ToPaymentMethods(methods), nil
}

// StartVerification opens the hosted card form of the provider the Team's
// billing country routes to. The provider and currency are pinned on the
// verification, so the card is saved under the route it was verified with.
func (h *PaymentMethod) StartVerification(ctx context.Context, req *paymentmethod.StartVerificationRequest) (*dto.CardVerification, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingWrite); err != nil {
		return nil, err
	}

	p, err := h.profiles.FindByTeamID(ctx, teamID)
	if errors.Is(err, profile.ErrorNotFound) {
		return nil, paymentmethod.ErrorProfileRequired
	}
	if err != nil {
		return nil, err
	}

	route, err := h.methods.FindRoute(ctx, p.Country)
	if err != nil {
		return nil, err
	}
	adapter, err := h.adapter(route.Provider)
	if err != nil {
		return nil, err
	}
	team, err := h.access.FindTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}

	id := uuid.New()
	started, err := adapter.StartCardVerification(ctx, providers.VerificationRequest{
		Payer:       payerOf(p, team),
		Currency:    route.Currency,
		ReturnURL:   h.frontURL + fmt.Sprintf(cardCompletePath, id),
		ReferenceID: id.String(),
	})
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("team", teamID.String()).Add("step", "start card verification"))

		return nil, paymentmethod.ErrorProviderUnavailable
	}

	v := &paymentmethod.Verification{
		ID: id, TeamID: teamID, Provider: route.Provider, Currency: route.Currency, ProviderRef: started.Ref,
		Status: paymentmethod.VerificationPending, CreatedBy: contextutil.CurrentUserID(ctx),
	}
	if err := h.methods.CreateVerification(ctx, v); err != nil {
		return nil, err
	}

	return &dto.CardVerification{VerificationID: id, RedirectURL: started.RedirectURL}, nil
}

// CompleteVerification saves the card the provider verified. The first card
// of a Team becomes the default and makes the Team active; completing the
// same verification again answers the card it already saved.
func (h *PaymentMethod) CompleteVerification(ctx context.Context, req *paymentmethod.CompleteVerificationRequest) (*dto.PaymentMethod, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingWrite); err != nil {
		return nil, err
	}

	v, err := h.methods.FindVerification(ctx, teamID, uuid.MustParse(req.VerificationID))
	if err != nil {
		return nil, err
	}
	switch v.Status {
	case paymentmethod.VerificationCompleted:
		return h.savedBy(ctx, v)
	case paymentmethod.VerificationFailed:
		return nil, paymentmethod.ErrorVerificationFail
	}

	adapter, err := h.adapter(v.Provider)
	if err != nil {
		return nil, err
	}
	card, err := adapter.CompleteCardVerification(ctx, v.ProviderRef, req.Callback)
	if err != nil {
		reason, declined := providers.Declined(err)
		if !declined && !errors.Is(err, providers.ErrUnknownRef) {
			h.log.ErrorWithFields(err, logger.NewFields().Add("verification", v.ID.String()).Add("step", "complete card verification"))

			return nil, paymentmethod.ErrorProviderUnavailable
		}
		if !declined {
			reason = providers.ReasonNotCompleted
		}
		h.fail(ctx, v, reason)

		return nil, paymentmethod.ErrorVerificationFail
	}

	var (
		saved     *paymentmethod.PaymentMethod
		activated *billingstatus.TeamStatus
		mismatch  bool
		at        = h.now().UTC()
	)
	err = inTransaction(ctx, h.transactions, func(ctx context.Context) error {
		if _, err := h.statuses.FindForUpdate(ctx, teamID, at); err != nil {
			return err
		}
		current, err := h.methods.FindVerification(ctx, teamID, v.ID)
		if err != nil {
			return err
		}
		if current.Status != paymentmethod.VerificationPending {
			return nil
		}

		p, err := h.profiles.FindByTeamID(ctx, teamID)
		if err != nil {
			return err
		}
		route, err := h.methods.FindRoute(ctx, p.Country)
		if err != nil {
			return err
		}
		if route.Provider != v.Provider || route.Currency != v.Currency {
			mismatch = true
			current.Status, current.FailureReason, current.CompletedAt = paymentmethod.VerificationFailed, reasonCountryChanged, &at

			return h.methods.SaveVerification(ctx, current)
		}

		count, err := h.methods.CountByTeamID(ctx, teamID)
		if err != nil {
			return err
		}
		saved = &paymentmethod.PaymentMethod{
			TeamID: teamID, Provider: v.Provider, Currency: v.Currency,
			ProviderCustomerRef: card.CustomerRef, ProviderCardRef: card.CardRef,
			Brand: card.Brand, Last4: card.Last4, ExpMonth: card.ExpMonth, ExpYear: card.ExpYear,
			IsDefault: count == 0, CreatedBy: contextutil.CurrentUserID(ctx),
		}
		if err := h.methods.Create(ctx, saved); err != nil {
			return err
		}
		current.Status, current.PaymentMethodID, current.CompletedAt = paymentmethod.VerificationCompleted, &saved.ID, &at
		if err := h.methods.SaveVerification(ctx, current); err != nil {
			return err
		}

		activated, err = h.statuses.Transition(ctx, teamID, []billingstatus.Status{billingstatus.NoPaymentMethod}, billingstatus.Active, at)

		return err
	})
	if err != nil {
		return nil, err
	}
	if mismatch {
		return nil, paymentmethod.ErrorVerificationFail
	}
	if saved == nil {
		// A concurrent completion saved the card first.
		current, err := h.methods.FindVerification(ctx, teamID, v.ID)
		if err != nil {
			return nil, err
		}

		return h.savedBy(ctx, current)
	}
	if activated != nil {
		h.publish(ctx, activated)
	}

	return dto.ToPaymentMethod(saved), nil
}

func (h *PaymentMethod) SetDefault(ctx context.Context, req *paymentmethod.SetDefaultRequest) (*dto.PaymentMethod, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingWrite); err != nil {
		return nil, err
	}
	id := uuid.MustParse(req.MethodID)

	err := inTransaction(ctx, h.transactions, func(ctx context.Context) error {
		if _, err := h.methods.FindByID(ctx, teamID, id); err != nil {
			return err
		}

		return h.methods.SetDefault(ctx, teamID, id)
	})
	if err != nil {
		return nil, err
	}

	m, err := h.methods.FindByID(ctx, teamID, id)
	if err != nil {
		return nil, err
	}

	return dto.ToPaymentMethod(m), nil
}

// Delete removes a card. The default goes last, so a Team with cards always
// has one to charge; the last card stays while an Invoice is unpaid, and its
// removal makes the Team unable to create Droplets again.
func (h *PaymentMethod) Delete(ctx context.Context, req *paymentmethod.DeleteRequest) error {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingWrite); err != nil {
		return err
	}
	id := uuid.MustParse(req.MethodID)

	var (
		deactivated *billingstatus.TeamStatus
		at          = h.now().UTC()
	)
	err := inTransaction(ctx, h.transactions, func(ctx context.Context) error {
		if _, err := h.statuses.FindForUpdate(ctx, teamID, at); err != nil {
			return err
		}
		methods, err := h.methods.FindByTeamID(ctx, teamID)
		if err != nil {
			return err
		}

		var target *paymentmethod.PaymentMethod
		for _, m := range methods {
			if m.ID == id {
				target = m
			}
		}
		if target == nil {
			return paymentmethod.ErrorNotFound
		}
		last := len(methods) == 1
		if target.IsDefault && !last {
			return paymentmethod.ErrorDefaultInUse
		}
		if last {
			unpaid, err := h.invoices.UnpaidExists(ctx, teamID)
			if err != nil {
				return err
			}
			if unpaid {
				return paymentmethod.ErrorLastCardOwed
			}
		}

		if err := h.methods.Delete(ctx, teamID, id, at); err != nil {
			return err
		}
		if !last {
			return nil
		}
		deactivated, err = h.statuses.Transition(ctx, teamID, []billingstatus.Status{billingstatus.Active}, billingstatus.NoPaymentMethod, at)

		return err
	})
	if err != nil {
		return err
	}
	if deactivated != nil {
		h.publish(ctx, deactivated)
	}

	return nil
}

func (h *PaymentMethod) savedBy(ctx context.Context, v *paymentmethod.Verification) (*dto.PaymentMethod, error) {
	if v.PaymentMethodID == nil {
		return nil, paymentmethod.ErrorVerificationFail
	}
	m, err := h.methods.FindByID(ctx, v.TeamID, *v.PaymentMethodID)
	if errors.Is(err, paymentmethod.ErrorNotFound) {
		// The card was saved and has been deleted since.
		return nil, paymentmethod.ErrorUnknownVerify
	}
	if err != nil {
		return nil, err
	}

	return dto.ToPaymentMethod(m), nil
}

func (h *PaymentMethod) fail(ctx context.Context, v *paymentmethod.Verification, reason string) {
	at := h.now().UTC()
	v.Status, v.FailureReason, v.CompletedAt = paymentmethod.VerificationFailed, reason, &at
	if err := h.methods.SaveVerification(ctx, v); err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("verification", v.ID.String()))
	}
}

func (h *PaymentMethod) adapter(code string) (providers.Provider, error) {
	adapter, err := h.registry.Get(code)
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("provider", code))

		return nil, paymentmethod.ErrorProviderNotConfig
	}

	return adapter, nil
}

// publish runs after commit with the row's own change time; a failure is
// logged, the status row already holds the truth and replay-status can
// republish it.
func (h *PaymentMethod) publish(ctx context.Context, changed *billingstatus.TeamStatus) {
	if err := h.published.Publish(ctx, changed.TeamID, string(changed.Status), changed.ChangedAt); err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("team", changed.TeamID.String()).Add("status", string(changed.Status)))
	}
}

func payerOf(p *profile.Profile, team *access.Team) providers.Payer {
	payer := providers.Payer{
		TeamID: p.TeamID, LegalName: p.LegalName, Email: p.InvoiceEmail, Country: p.Country,
		City: p.City, AddressLine1: p.AddressLine1, PostalCode: p.PostalCode, TaxID: p.TaxID,
	}
	if team != nil {
		payer.TeamName = team.Name
	}

	return payer
}
