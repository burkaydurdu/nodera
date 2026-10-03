package integrationtest

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
)

func mustDecimal(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func (s *billingSuite) TestAccess_FinanceReadsAndWritesTheProfile() {
	owner, teamID := s.payingTeam()
	finance := s.signIn()
	s.addMember(teamID, finance, "finance", time.Now())

	read := s.get(billingPath(teamID, "/profile"), finance.Token)
	s.Equal(http.StatusOK, read.Code, read.Raw)
	s.Equal("TR", read.str("country"))

	write := s.putProfile(teamID, finance.Token, trCompany())
	s.Equal(http.StatusOK, write.Code, write.Raw)
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/profile"), owner.Token).Code)
}

func (s *billingSuite) TestAccess_DeveloperIsForbidden() {
	_, teamID := s.payingTeam()
	developer := s.signIn()
	s.addMember(teamID, developer, "developer", time.Now())

	s.assertError(s.get(billingPath(teamID, "/profile"), developer.Token), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
	s.assertError(s.putProfile(teamID, developer.Token, trCompany()), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
	s.assertError(s.get(billingPath(teamID, "/payment-methods"), developer.Token), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
	s.assertError(s.startVerification(teamID, developer.Token), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
}

// Every member may see whether the Team can spend; only billing.read sees
// the money matters behind it.
func (s *billingSuite) TestAccess_DeveloperSeesTheStatusButNotTheProfile() {
	_, teamID := s.payingTeam()
	developer := s.signIn()
	s.addMember(teamID, developer, "developer", time.Now())

	status := s.get(billingPath(teamID, "/status"), developer.Token)
	s.Equal(http.StatusOK, status.Code, status.Raw)
	s.Equal("no_payment_method", status.str("status"))

	s.assertError(s.get(billingPath(teamID, "/profile"), developer.Token), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
}

func (s *billingSuite) TestAccess_OutsiderAndUnknownTeamAreNotFound() {
	_, teamID := s.payingTeam()
	outsider := s.signIn()
	s.team(outsider)

	s.assertError(s.get(billingPath(teamID, "/profile"), outsider.Token), http.StatusNotFound, int(access.Domain), access.ErrorTeamNotFound.Code)
	s.assertError(s.get(billingPath(teamID, "/status"), outsider.Token), http.StatusNotFound, int(access.Domain), access.ErrorTeamNotFound.Code)
	s.assertError(s.get(billingPath(uuid.New(), "/profile"), outsider.Token), http.StatusNotFound, int(access.Domain), access.ErrorTeamNotFound.Code)
}

func (s *billingSuite) TestAccess_WithoutASessionIsUnauthorized() {
	_, teamID := s.payingTeam()

	s.Equal(http.StatusUnauthorized, s.get(billingPath(teamID, "/profile"), "").Code)
}

func (s *billingSuite) TestAccess_RemovedMemberIsNotFound_AndALateAddCannotBringThemBack() {
	_, teamID := s.payingTeam()
	finance := s.signIn()
	t0 := time.Now().Add(-time.Hour)
	s.addMember(teamID, finance, "finance", t0)
	s.Require().Equal(http.StatusOK, s.get(billingPath(teamID, "/profile"), finance.Token).Code)

	s.removeMember(teamID, finance, t0.Add(30*time.Minute))
	s.assertError(s.get(billingPath(teamID, "/profile"), finance.Token), http.StatusNotFound, int(access.Domain), access.ErrorTeamNotFound.Code)

	s.addMember(teamID, finance, "finance", t0)
	s.assertError(s.get(billingPath(teamID, "/profile"), finance.Token), http.StatusNotFound, int(access.Domain), access.ErrorTeamNotFound.Code)

	s.addMember(teamID, finance, "finance", t0.Add(45*time.Minute))
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/profile"), finance.Token).Code, "invited again later")
}

func (s *billingSuite) TestAccess_RoleChangesFollowTheNewestEvent() {
	_, teamID := s.payingTeam()
	member := s.signIn()
	t0 := time.Now().Add(-time.Hour)
	s.addMember(teamID, member, "developer", t0)
	s.Require().Equal(http.StatusForbidden, s.get(billingPath(teamID, "/profile"), member.Token).Code)

	s.changeRole(teamID, member, "finance", t0.Add(10*time.Minute))
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/profile"), member.Token).Code)

	s.changeRole(teamID, member, "developer", t0.Add(5*time.Minute))
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/profile"), member.Token).Code, "an older change arriving late changes nothing")
}

// The owner arrives with TeamCreated; a replay from a producer older than the
// Team name never blanks the known one.
func (s *billingSuite) TestAccess_TeamCreatedWithoutTheNameKeepsTheKnownOne() {
	owner := s.signIn()
	teamID := uuid.New()
	now := stamp(time.Now())
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"name":"Acme","owner_user_id":%q,"created_at":%q}}`, teamID, owner.ID, now))

	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"created_at":%q}}`, teamID, owner.ID, now))

	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/status"), owner.Token).Code)
	team, err := s.services.Access.FindTeam(context.Background(), teamID)
	s.Require().NoError(err)
	s.Equal("Acme", team.Name)
}

func (s *billingSuite) TestAccess_MalformedEventIsTerminal() {
	err := s.consumer.Consume(rabbitMessage(constants.TeamMemberAdded, `{"payload":{"team_id":"not-a-uuid"}}`))
	s.Require().Error(err)
	s.ErrorIs(err, access.ErrorMalformedEvent)
}
