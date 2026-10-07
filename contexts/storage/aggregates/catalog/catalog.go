// Package catalog is what a Storage is chosen from: the Regions Nodera keeps
// Storage in and the Packages it sells.
package catalog

import "github.com/shopspring/decimal"

// PriceScale is the two decimals a monthly price carries on the wire.
const PriceScale = 2

type Region struct {
	Code    string
	City    string
	Country string
}

// Package is one row of the catalog: a size and its monthly price.
type Package struct {
	Code         string
	SizeBytes    int64
	MonthlyPrice decimal.Decimal
}
