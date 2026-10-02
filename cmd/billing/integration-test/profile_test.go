package integrationtest

import (
	"net/http"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
)

func rabbitMessage(routingKey, payload string) *rabbitmq.Message {
	return &rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload)}
}

func (s *billingSuite) TestProfile_NoneYetIsNotFound() {
	owner := s.signIn()
	teamID := s.team(owner)

	s.assertError(s.get(billingPath(teamID, "/profile"), owner.Token), http.StatusNotFound, int(profile.Domain), profile.ErrorNotFound.Code)
}

func (s *billingSuite) TestProfile_PutThenGetNormalizes() {
	owner := s.signIn()
	teamID := s.team(owner)

	put := s.putProfile(teamID, owner.Token, trCompany())
	s.Require().Equal(http.StatusOK, put.Code, put.Raw)

	got := s.get(billingPath(teamID, "/profile"), owner.Token)
	s.Require().Equal(http.StatusOK, got.Code, got.Raw)
	s.Equal(teamID.String(), got.str("team_id"))
	s.Equal("company", got.str("account_type"))
	s.Equal("TR", got.str("country"))
	s.Equal(validVKN, got.str("tax_id"))
	s.Equal("Kadıköy", got.str("tax_office"))
	s.Equal("muhasebe@acme.example", got.str("invoice_email"))
	s.NotEmpty(got.str("created_at"))
	s.NotEmpty(got.str("updated_at"))
}

// The tax identifiers a country and account type require (spec: Billing
// profile); every refusal is 400 30-10004.
func (s *billingSuite) TestProfile_ValidationMatrix() {
	owner := s.signIn()
	teamID := s.team(owner)

	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"TR company with VKN and office", trCompany(), true},
		{"TR company without office", `{"account_type":"company","legal_name":"A","country":"TR","address_line1":"x","city":"y","tax_id":"` + validVKN + `","invoice_email":"a@b.example"}`, false},
		{"TR company with a 9-digit VKN", `{"account_type":"company","legal_name":"A","country":"TR","address_line1":"x","city":"y","tax_id":"123456789","tax_office":"Moda","invoice_email":"a@b.example"}`, false},
		{"TR company without VKN", `{"account_type":"company","legal_name":"A","country":"TR","address_line1":"x","city":"y","tax_office":"Moda","invoice_email":"a@b.example"}`, false},
		{"TR individual with a valid TCKN", `{"account_type":"individual","legal_name":"Ayşe","country":"tr","address_line1":"x","city":"y","tax_id":"` + validTCKN + `","invoice_email":"a@b.example"}`, true},
		{"TR individual with a bad checksum", `{"account_type":"individual","legal_name":"Ayşe","country":"TR","address_line1":"x","city":"y","tax_id":"10000000147","invoice_email":"a@b.example"}`, false},
		{"TR individual without TCKN", `{"account_type":"individual","legal_name":"Ayşe","country":"TR","address_line1":"x","city":"y","invoice_email":"a@b.example"}`, false},
		{"DE company with nothing tax-related", `{"account_type":"company","legal_name":"A GmbH","country":"DE","address_line1":"x","city":"y","invoice_email":"a@b.example"}`, true},
		{"US individual with nothing tax-related", `{"account_type":"individual","legal_name":"Jane","country":"US","address_line1":"x","city":"y","invoice_email":"a@b.example"}`, true},
	}

	for _, c := range cases {
		res := s.putProfile(teamID, owner.Token, c.body)
		if c.ok {
			s.Equal(http.StatusOK, res.Code, "%s: %s", c.name, res.Raw)

			continue
		}
		s.assertError(res, http.StatusBadRequest, int(profile.Domain), profile.ErrorInvalidTaxIdentifiers.Code)
	}
}

func (s *billingSuite) TestProfile_FieldValidationIsBadRequest() {
	owner := s.signIn()
	teamID := s.team(owner)

	for _, body := range []string{
		`{"account_type":"partnership","legal_name":"A","country":"DE","address_line1":"x","city":"y","invoice_email":"a@b.example"}`,
		`{"account_type":"company","legal_name":"A","country":"XX","address_line1":"x","city":"y","invoice_email":"a@b.example"}`,
		`{"account_type":"company","legal_name":"A","country":"DE","address_line1":"x","city":"y","invoice_email":"not-an-email"}`,
		`{"account_type":"company","country":"DE","address_line1":"x","city":"y","invoice_email":"a@b.example"}`,
	} {
		s.Equal(http.StatusBadRequest, s.putProfile(teamID, owner.Token, body).Code, body)
	}
}

func (s *billingSuite) TestProfile_CountryChangeIsBlockedWhileACardExists() {
	owner, teamID := s.payingTeam()
	card := s.addCard(teamID, owner.Token)

	s.assertError(s.putProfile(teamID, owner.Token, deCompany()), http.StatusConflict, int(profile.Domain), profile.ErrorCountryLocked.Code)
	s.Equal("TR", s.get(billingPath(teamID, "/profile"), owner.Token).str("country"), "nothing changed")

	sameCountry := s.putProfile(teamID, owner.Token, trCompany())
	s.Equal(http.StatusOK, sameCountry.Code, "other fields stay editable: %s", sameCountry.Raw)

	s.Require().Equal(http.StatusNoContent, s.request(http.MethodDelete, billingPath(teamID, "/payment-methods/"+card.str("id")), "", owner.Token).Code)
	moved := s.putProfile(teamID, owner.Token, deCompany())
	s.Equal(http.StatusOK, moved.Code, moved.Raw)
	s.Equal("DE", moved.str("country"))
}
