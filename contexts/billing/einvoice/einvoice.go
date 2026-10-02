// Package einvoice is the seam an e-invoice integrator will plug into
// (ADR-0006): it is called after an Invoice is issued and answers the legal
// document's reference. Until one is connected, None answers nothing and the
// PDF stays a statement.
package einvoice

import (
	"context"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
)

type Issuer interface {
	// Issue answers the integrator's reference for the Invoice, empty when
	// nothing was issued.
	Issue(ctx context.Context, inv *invoice.Invoice) (string, error)
}

type None struct{}

func (None) Issue(context.Context, *invoice.Invoice) (string, error) { return "", nil }
