package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/authz"
)

type Profile struct {
	authorizer   *Authorizer
	profiles     profile.IService
	methods      paymentmethod.IService
	statuses     billingstatus.IService
	transactions postgresql.ITransactionManager
	now          func() time.Time
}

func NewProfile(
	authorizer *Authorizer,
	profiles profile.IService,
	methods paymentmethod.IService,
	statuses billingstatus.IService,
	transactions postgresql.ITransactionManager,
	now func() time.Time,
) *Profile {
	return &Profile{authorizer: authorizer, profiles: profiles, methods: methods, statuses: statuses, transactions: transactions, now: now}
}

func (h *Profile) Get(ctx context.Context, req *profile.GetRequest) (*dto.Profile, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingRead); err != nil {
		return nil, err
	}

	p, err := h.profiles.FindByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return dto.ToProfile(p), nil
}

// Put creates or replaces the Team's Billing profile. A change of country is
// refused while a card exists: the card was saved with the old country's
// Payment provider. The Team's status row is locked first so a card
// completing at the same moment cannot slip past the check.
func (h *Profile) Put(ctx context.Context, req *profile.PutRequest) (*dto.Profile, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingWrite); err != nil {
		return nil, err
	}

	p := &profile.Profile{
		TeamID:       teamID,
		AccountType:  profile.AccountType(req.AccountType),
		LegalName:    req.LegalName,
		Country:      req.Country,
		AddressLine1: req.AddressLine1,
		AddressLine2: req.AddressLine2,
		City:         req.City,
		PostalCode:   req.PostalCode,
		TaxID:        req.TaxID,
		TaxOffice:    req.TaxOffice,
		VATID:        req.VATID,
		InvoiceEmail: req.InvoiceEmail,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}

	err := inTransaction(ctx, h.transactions, func(ctx context.Context) error {
		if _, err := h.statuses.FindForUpdate(ctx, teamID, h.now().UTC()); err != nil {
			return err
		}

		existing, err := h.profiles.FindByTeamID(ctx, teamID)
		if err != nil && !errors.Is(err, profile.ErrorNotFound) {
			return err
		}
		if existing != nil && existing.Country != p.Country {
			count, err := h.methods.CountByTeamID(ctx, teamID)
			if err != nil {
				return err
			}
			if count > 0 {
				return profile.ErrorCountryLocked
			}
		}

		return h.profiles.Upsert(ctx, p)
	})
	if err != nil {
		return nil, err
	}

	return dto.ToProfile(p), nil
}
