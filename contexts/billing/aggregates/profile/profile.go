// Package profile is the Billing profile: who a Team's Invoice is made out
// to. A Team must have one before it can hold a Payment method, and its
// country decides the Payment provider and the Tax rule.
package profile

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/str"
)

type AccountType string

const (
	AccountIndividual AccountType = "individual"
	AccountCompany    AccountType = "company"

	CountryTR = "TR"
)

type Profile struct {
	TeamID       uuid.UUID
	AccountType  AccountType
	LegalName    string
	Country      string
	AddressLine1 string
	AddressLine2 string
	City         string
	PostalCode   string
	TaxID        string
	TaxOffice    string
	VATID        string
	InvoiceEmail string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Validate applies the rules the country and account type impose on the tax
// identifiers (spec: Billing profile). Anything else is the request's own
// field validation.
func (p *Profile) Validate() error {
	if p.Country != CountryTR {
		return nil
	}

	switch p.AccountType {
	case AccountCompany:
		if !IsValidVKN(p.TaxID) {
			return ErrorInvalidTaxIdentifiers.Copy().SetMessage("a Turkish company needs a 10-digit tax number (VKN)")
		}
		if str.IsBlank(p.TaxOffice) {
			return ErrorInvalidTaxIdentifiers.Copy().SetMessage("a Turkish company needs its tax office")
		}
	case AccountIndividual:
		if !IsValidTCKN(p.TaxID) {
			return ErrorInvalidTaxIdentifiers.Copy().SetMessage("a Turkish individual needs a valid 11-digit identity number (TCKN)")
		}
	}

	return nil
}
