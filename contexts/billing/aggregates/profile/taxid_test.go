package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
)

// 10000000146 is the well-known test number that passes both check digits.
func TestIsValidTCKN(t *testing.T) {
	cases := map[string]bool{
		"10000000146": true,
		"10000000147": false, // last digit off
		"10000000156": false, // tenth digit off
		"00000000000": false, // leading zero
		"1000000014":  false, // too short
		"1000000014a": false, // not digits
		"":            false,
	}
	for value, want := range cases {
		assert.Equalf(t, want, profile.IsValidTCKN(value), "%q", value)
	}
}

func TestIsValidVKN(t *testing.T) {
	assert.True(t, profile.IsValidVKN("1234567890"))
	assert.False(t, profile.IsValidVKN("123456789"))
	assert.False(t, profile.IsValidVKN("12345678901"))
	assert.False(t, profile.IsValidVKN("12345678x0"))
}

func TestValidate_TurkishCompanyNeedsVKNAndOffice(t *testing.T) {
	p := &profile.Profile{Country: "TR", AccountType: profile.AccountCompany, TaxID: "1234567890"}
	assert.ErrorIs(t, p.Validate(), profile.ErrorInvalidTaxIdentifiers)

	p.TaxOffice = "Kadıköy"
	assert.NoError(t, p.Validate())

	p.TaxID = "123"
	assert.ErrorIs(t, p.Validate(), profile.ErrorInvalidTaxIdentifiers)
}

func TestValidate_TurkishIndividualNeedsTCKN(t *testing.T) {
	p := &profile.Profile{Country: "TR", AccountType: profile.AccountIndividual, TaxID: "10000000147"}
	assert.ErrorIs(t, p.Validate(), profile.ErrorInvalidTaxIdentifiers)

	p.TaxID = "10000000146"
	assert.NoError(t, p.Validate())
}

func TestValidate_OtherCountriesNeedNothing(t *testing.T) {
	assert.NoError(t, (&profile.Profile{Country: "DE", AccountType: profile.AccountCompany}).Validate())
	assert.NoError(t, (&profile.Profile{Country: "US", AccountType: profile.AccountIndividual}).Validate())
}
