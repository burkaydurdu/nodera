package integrationtest

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/providers"
)

func (s *billingSuite) TestCards_StartWithoutProfileIsConflict() {
	owner := s.signIn()
	teamID := s.team(owner)

	s.assertError(s.startVerification(teamID, owner.Token), http.StatusConflict, int(paymentmethod.Domain), paymentmethod.ErrorProfileRequired.Code)
	s.Empty(s.fake.Verifications, "the provider was never asked")
}

func (s *billingSuite) TestCards_FirstCardIsDefaultAndActivatesTheTeam() {
	owner, teamID := s.payingTeam()

	started := s.startVerification(teamID, owner.Token)
	s.Require().Equal(http.StatusCreated, started.Code, started.Raw)
	verificationID := started.str("verification_id")
	s.NotEmpty(started.str("redirect_url"))
	s.Require().Len(s.fake.Verifications, 1)
	sent := s.fake.Verifications[0]
	s.Equal(frontURL+"/billing/cards/complete?verification="+verificationID, sent.ReturnURL)
	s.Equal("TRY", sent.Currency, "a Turkish Team verifies in TRY")
	s.Equal("Acme Bilişim A.Ş.", sent.Payer.LegalName)
	s.Equal("Acme", sent.Payer.TeamName)

	s.fake.NextCard("mastercard", "0008", 7, 2031)
	completed := s.completeVerification(teamID, owner.Token, verificationID)
	s.Require().Equal(http.StatusCreated, completed.Code, completed.Raw)
	s.Equal(true, completed.Body["is_default"])
	s.Equal("mastercard", completed.str("brand"))
	s.Equal("0008", completed.str("last4"))
	s.Equal(float64(7), completed.Body["exp_month"])
	s.Equal(float64(2031), completed.Body["exp_year"])
	s.Equal("TRY", completed.str("currency"))
	s.Equal(teamID.String(), completed.str("team_id"))
	s.NotContains(strings.ToLower(completed.Raw), "craftgate", "no provider name in a DTO")
	s.NotContains(completed.Raw, "card-ver-", "no provider reference in a DTO")

	list := s.get(billingPath(teamID, "/payment-methods"), owner.Token)
	s.Require().Equal(http.StatusOK, list.Code, list.Raw)
	s.Require().Len(list.List, 1)
	s.Equal(completed.str("id"), list.List[0]["id"])

	s.Equal([]string{"active"}, s.statusEvents(teamID))
	payload := s.outboxPayloads("TeamBillingStatusChanged")[0]
	s.NotEmpty(payload["changed_at"])
	s.Equal("active", s.get(billingPath(teamID, "/status"), owner.Token).str("status"))

	var provider, customerRef string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT provider, provider_customer_ref FROM payment_methods WHERE id = $1`, completed.str("id")).Scan(&provider, &customerRef))
	s.Equal("craftgate", provider, "the routed provider is pinned on the card")
	s.Equal("cus-"+teamID.String(), customerRef)
}

func (s *billingSuite) TestCards_NonTurkishTeamIsChargedInUSD() {
	owner := s.signIn()
	teamID := s.team(owner)
	s.Require().Equal(http.StatusOK, s.putProfile(teamID, owner.Token, deCompany()).Code)

	card := s.addCard(teamID, owner.Token)

	s.Equal("USD", card.str("currency"))
	s.Equal("USD", s.fake.Verifications[0].Currency)
}

func (s *billingSuite) TestCards_CompletingTwiceAnswersTheSameCard() {
	owner, teamID := s.payingTeam()
	started := s.startVerification(teamID, owner.Token)
	first := s.completeVerification(teamID, owner.Token, started.str("verification_id"))
	s.Require().Equal(http.StatusCreated, first.Code, first.Raw)

	again := s.completeVerification(teamID, owner.Token, started.str("verification_id"))

	s.Equal(http.StatusCreated, again.Code, again.Raw)
	s.Equal(first.str("id"), again.str("id"))
	s.Len(s.get(billingPath(teamID, "/payment-methods"), owner.Token).List, 1)
	s.Equal([]string{"active"}, s.statusEvents(teamID))
}

func (s *billingSuite) TestCards_DeclinedVerificationIsPaymentRequired() {
	owner, teamID := s.payingTeam()
	started := s.startVerification(teamID, owner.Token)
	s.fake.FailNextVerification(providers.ReasonInsufficientFunds)

	failed := s.completeVerification(teamID, owner.Token, started.str("verification_id"))

	s.assertError(failed, http.StatusPaymentRequired, int(paymentmethod.Domain), paymentmethod.ErrorVerificationFail.Code)
	s.Empty(s.get(billingPath(teamID, "/payment-methods"), owner.Token).List)
	s.Empty(s.statusEvents(teamID))
	s.assertError(s.completeVerification(teamID, owner.Token, started.str("verification_id")),
		http.StatusPaymentRequired, int(paymentmethod.Domain), paymentmethod.ErrorVerificationFail.Code)
}

func (s *billingSuite) TestCards_UnknownVerificationIsNotFound() {
	owner, teamID := s.payingTeam()
	s.assertError(s.completeVerification(teamID, owner.Token, uuid.NewString()), http.StatusNotFound, int(paymentmethod.Domain), paymentmethod.ErrorUnknownVerify.Code)

	// Another Team's verification does not exist for this one.
	other, otherTeam := s.payingTeam()
	started := s.startVerification(otherTeam, other.Token)
	s.assertError(s.completeVerification(teamID, owner.Token, started.str("verification_id")),
		http.StatusNotFound, int(paymentmethod.Domain), paymentmethod.ErrorUnknownVerify.Code)
}

func (s *billingSuite) TestCards_ProviderOutageIsBadGatewayAndLeaksNothing() {
	owner, teamID := s.payingTeam()
	s.fake.FailStart(errors.New("craftgate: connection refused"))

	res := s.startVerification(teamID, owner.Token)

	s.assertError(res, http.StatusBadGateway, int(paymentmethod.Domain), paymentmethod.ErrorProviderUnavailable.Code)
	s.NotContains(strings.ToLower(res.Raw), "craftgate")
}

func (s *billingSuite) TestCards_SecondCardIsNotDefault_AndDefaultMoves() {
	owner, teamID := s.payingTeam()
	first := s.addCard(teamID, owner.Token)
	second := s.addCard(teamID, owner.Token)

	s.Equal(false, second.Body["is_default"])
	s.Equal([]string{"active"}, s.statusEvents(teamID), "only the first card changes the status")

	moved := s.request(http.MethodPut, billingPath(teamID, "/payment-methods/"+second.str("id")+"/default"), "", owner.Token)
	s.Require().Equal(http.StatusOK, moved.Code, moved.Raw)
	s.Equal(true, moved.Body["is_default"])

	list := s.get(billingPath(teamID, "/payment-methods"), owner.Token)
	s.Require().Len(list.List, 2)
	s.Equal(second.str("id"), list.List[0]["id"], "default first")
	s.Equal(true, list.List[0]["is_default"])
	s.Equal(first.str("id"), list.List[1]["id"])
	s.Equal(false, list.List[1]["is_default"])
}

func (s *billingSuite) TestCards_SetDefaultOfAnotherTeamsCardIsNotFound() {
	owner, teamID := s.payingTeam()
	other, otherTeam := s.payingTeam()
	foreign := s.addCard(otherTeam, other.Token)

	s.assertError(s.request(http.MethodPut, billingPath(teamID, "/payment-methods/"+foreign.str("id")+"/default"), "", owner.Token),
		http.StatusNotFound, int(paymentmethod.Domain), paymentmethod.ErrorNotFound.Code)
}

func (s *billingSuite) TestCards_DeleteRules() {
	owner, teamID := s.payingTeam()
	first := s.addCard(teamID, owner.Token)
	second := s.addCard(teamID, owner.Token)

	s.assertError(s.request(http.MethodDelete, billingPath(teamID, "/payment-methods/"+first.str("id")), "", owner.Token),
		http.StatusConflict, int(paymentmethod.Domain), paymentmethod.ErrorDefaultInUse.Code)

	s.Equal(http.StatusNoContent, s.request(http.MethodDelete, billingPath(teamID, "/payment-methods/"+second.str("id")), "", owner.Token).Code)
	s.Len(s.get(billingPath(teamID, "/payment-methods"), owner.Token).List, 1)
	s.Equal([]string{"active"}, s.statusEvents(teamID), "removing a non-last card changes no status")

	s.assertError(s.request(http.MethodDelete, billingPath(teamID, "/payment-methods/"+second.str("id")), "", owner.Token),
		http.StatusNotFound, int(paymentmethod.Domain), paymentmethod.ErrorNotFound.Code)
}

func (s *billingSuite) TestCards_DeletingTheLastCardBlocksTheTeamAgain() {
	owner, teamID := s.payingTeam()
	card := s.addCard(teamID, owner.Token)

	res := s.request(http.MethodDelete, billingPath(teamID, "/payment-methods/"+card.str("id")), "", owner.Token)

	s.Require().Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Empty(s.get(billingPath(teamID, "/payment-methods"), owner.Token).List)
	s.Equal([]string{"active", "no_payment_method"}, s.statusEvents(teamID))
	s.Equal("no_payment_method", s.get(billingPath(teamID, "/status"), owner.Token).str("status"))

	again := s.addCard(teamID, owner.Token)
	s.Equal(true, again.Body["is_default"])
	s.Equal([]string{"active", "no_payment_method", "active"}, s.statusEvents(teamID))
}

func (s *billingSuite) TestCards_FinanceManagesCards() {
	_, teamID := s.payingTeam()
	finance := s.signIn()
	s.addMember(teamID, finance, "finance", s.clock.Now())

	card := s.addCard(teamID, finance.Token)

	s.Equal(true, card.Body["is_default"])
}
