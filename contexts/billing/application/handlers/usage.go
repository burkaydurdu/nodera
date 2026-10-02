package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
)

type Usage struct {
	authorizer *Authorizer
	collection *Collection
	now        func() time.Time
}

func NewUsage(authorizer *Authorizer, collection *Collection, now func() time.Time) *Usage {
	return &Usage{authorizer: authorizer, collection: collection, now: now}
}

// Get answers the open Period's draft Invoice: the hours no Invoice has
// billed yet, tax estimated with today's rule for the Billing profile (none
// without one), and the Threshold that would issue it early.
func (h *Usage) Get(ctx context.Context, req *usage.GetRequest) (*dto.Usage, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingRead); err != nil {
		return nil, err
	}

	at := h.now().UTC()
	period := usage.PeriodOf(at)
	d, err := h.collection.draft(ctx, teamID, period, at)
	if err != nil {
		return nil, err
	}

	tax := decimal.Zero
	p, err := h.collection.profileOf(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if p != nil {
		rule, err := h.collection.taxRuleFor(ctx, p, at)
		if err != nil {
			return nil, err
		}
		tax = rule.Amount(d.Subtotal)
	}
	t, err := h.collection.thresholdFor(ctx, teamID, at)
	if err != nil {
		return nil, err
	}

	return &dto.Usage{
		PeriodStart:    period.Start,
		PeriodEnd:      period.End,
		Lines:          dto.ToUsageLines(d.Lines),
		Subtotal:       d.Subtotal.StringFixed(2),
		EstimatedTax:   tax.StringFixed(2),
		EstimatedTotal: d.Subtotal.Add(tax).StringFixed(2),
		Threshold:      dto.UsageThreshold{Amount: t.Amount.StringFixed(2), Kind: string(t.Kind)},
	}, nil
}
