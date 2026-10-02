// Package taxrule is the operator-managed table of what tax applies to a
// Team: a rate and a label chosen by billing country and account type, the
// most specific rule valid at the issue date winning.
package taxrule

import (
	"time"

	"github.com/shopspring/decimal"
)

// Any matches every country or account type; a rule with it is less specific
// than one naming the value.
const Any = "*"

const bpsPerUnit = 10000

type TaxRule struct {
	ID          int64
	Country     string
	AccountType string
	RateBps     int
	LabelKey    string
	ValidFrom   time.Time
	CreatedAt   time.Time
}

// Amount is the tax on a base: base × rate_bps / 10000 rounded half-up to
// cents (contract: Money rules).
func (r *TaxRule) Amount(base decimal.Decimal) decimal.Decimal {
	return base.Mul(decimal.NewFromInt(int64(r.RateBps))).Div(decimal.NewFromInt(bpsPerUnit)).Round(2)
}
