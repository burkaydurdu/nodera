package catalog

import "github.com/shopspring/decimal"

const (
	// MonthlyCapHours is the number of hours a month's bill caps at.
	MonthlyCapHours = 672
	// HourlyScale and MonthlyScale are the decimals a price is shown with.
	HourlyScale  = 4
	MonthlyScale = 2

	basisPointsPerUnit = 10_000
)

// HourlyPrice is the Plan price for an Offer price and a Commission in basis
// points: offer × (1 + bps/10000), rounded up so the Commission is never
// eaten by rounding.
func HourlyPrice(offer decimal.Decimal, commissionBps int) decimal.Decimal {
	factor := decimal.NewFromInt(int64(basisPointsPerUnit + commissionBps)).Div(decimal.NewFromInt(basisPointsPerUnit))

	return offer.Mul(factor).RoundUp(HourlyScale)
}

// MonthlyPrice is what a full month caps at: hourly × capHours, rounded up
// to cents.
func MonthlyPrice(hourly decimal.Decimal, capHours int) decimal.Decimal {
	return hourly.Mul(decimal.NewFromInt(int64(capHours))).RoundUp(MonthlyScale)
}

// EffectiveCommissionBps is the Plan's override when set, else the Provider's
// default.
func EffectiveCommissionBps(override *int, providerDefault int) int {
	if override != nil {
		return *override
	}

	return providerDefault
}
