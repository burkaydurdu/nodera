package profile

import (
	"strings"

	"github.com/piggystats/nodera/pkg/country"
)

type GetRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

// PutRequest carries every Profile field; the tax identifier rules by country
// and account type are the aggregate's (Profile.Validate), not the tags'.
type PutRequest struct {
	TeamID       string `param:"teamID" json:"-" validate:"required,uuid"`
	AccountType  string `json:"account_type" validate:"required,oneof=individual company"`
	LegalName    string `json:"legal_name" validate:"required,max=200"`
	Country      string `json:"country" validate:"required,country"`
	AddressLine1 string `json:"address_line1" validate:"required,max=200"`
	AddressLine2 string `json:"address_line2" validate:"max=200"`
	City         string `json:"city" validate:"required,max=100"`
	PostalCode   string `json:"postal_code" validate:"max=20"`
	TaxID        string `json:"tax_id" validate:"max=32"`
	TaxOffice    string `json:"tax_office" validate:"max=100"`
	VATID        string `json:"vat_id" validate:"max=32"`
	InvoiceEmail string `json:"invoice_email" validate:"required,email,max=254"`
}

func (r *PutRequest) Normalize() {
	r.AccountType = strings.ToLower(strings.TrimSpace(r.AccountType))
	r.LegalName = strings.TrimSpace(r.LegalName)
	r.Country = country.Normalize(strings.TrimSpace(r.Country))
	r.AddressLine1 = strings.TrimSpace(r.AddressLine1)
	r.AddressLine2 = strings.TrimSpace(r.AddressLine2)
	r.City = strings.TrimSpace(r.City)
	r.PostalCode = strings.TrimSpace(r.PostalCode)
	r.TaxID = strings.TrimSpace(r.TaxID)
	r.TaxOffice = strings.TrimSpace(r.TaxOffice)
	r.VATID = strings.ToUpper(strings.TrimSpace(r.VATID))
	r.InvoiceEmail = strings.ToLower(strings.TrimSpace(r.InvoiceEmail))
}
