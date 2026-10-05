package integrationtest

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
)

func (s *billingSuite) TestSamplePaymentMethod_ActivatesTheTeamWithoutAskingTheProvider() {
	owner, teamID := s.payingTeam()

	m, status, err := app.SamplePaymentMethod(context.Background(), s.deps, teamID)

	s.Require().NoError(err)
	s.Equal(billingstatus.Active, status)
	s.Empty(s.fake.Verifications, "no provider was asked")
	s.Equal([]string{"active"}, s.statusEvents(teamID), "the other contexts learn it through the outbox")
	s.Equal("active", s.get(billingPath(teamID, "/status"), owner.Token).str("status"))

	list := s.get(billingPath(teamID, "/payment-methods"), owner.Token)
	s.Require().Equal(http.StatusOK, list.Code, list.Raw)
	s.Require().Len(list.List, 1)
	s.Equal(m.ID.String(), list.List[0]["id"])
	s.Equal(true, list.List[0]["is_default"])
	s.Equal("TRY", list.List[0]["currency"], "routed like the Team's real card would be")
}

func (s *billingSuite) TestSamplePaymentMethod_ATeamWithoutABillingProfileIsRefused() {
	owner := s.signIn()
	teamID := s.team(owner)

	_, _, err := app.SamplePaymentMethod(context.Background(), s.deps, teamID)

	s.ErrorIs(err, paymentmethod.ErrorProfileRequired)
	s.Empty(s.statusEvents(teamID))
}

func (s *billingSuite) TestSamplePaymentMethod_AnUnknownTeamIsNotFound() {
	_, _, err := app.SamplePaymentMethod(context.Background(), s.deps, uuid.New())

	s.ErrorIs(err, access.ErrorTeamNotFound)
}
