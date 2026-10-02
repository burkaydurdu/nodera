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

// OwnersOf is how collection addresses the billing emails.
func (s *billingSuite) TestAccess_OwnersOfAnswersTheOwnersInTheirLocale() {
	owner, teamID := s.payingTeam()
	coOwner := s.signIn()
	s.addMember(teamID, coOwner, "owner", time.Now())
	finance := s.signIn()
	s.addMember(teamID, finance, "finance", time.Now())

	owners, err := s.services.Access.OwnersOf(context.Background(), teamID)
	s.Require().NoError(err)
	s.Require().Len(owners, 2)
	s.Equal(owner.ID, owners[0].UserID)
	s.Equal(owner.Email, owners[0].Email)
	s.Equal("Owner Person", owners[0].FullName)
	s.Equal("tr", owners[0].Locale)
	s.Equal(coOwner.ID, owners[1].UserID)
	s.Equal("en", owners[1].Locale)

	team, err := s.services.Access.FindTeam(context.Background(), teamID)
	s.Require().NoError(err)
	s.Equal("Acme", team.Name)
}

// A producer from before the enrichment sends neither the name nor the
// owner's details; the owner still arrives through TeamMemberAdded, and an
// enrichment-free replay never blanks what is known.
func (s *billingSuite) TestAccess_TeamCreatedWithoutEnrichedFieldsIsTolerated() {
	owner := s.signIn()
	teamID := uuid.New()
	now := stamp(time.Now())
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"created_at":%q}}`, teamID, owner.ID, now))

	owners, err := s.services.Access.OwnersOf(context.Background(), teamID)
	s.Require().NoError(err)
	s.Empty(owners)

	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":"owner","email":%q,"full_name":"Late Owner","locale":"en","added_at":%q}}`,
		teamID, owner.ID, owner.Email, now))
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"created_at":%q}}`, teamID, owner.ID, now))

	owners, err = s.services.Access.OwnersOf(context.Background(), teamID)
	s.Require().NoError(err)
	s.Require().Len(owners, 1)
	s.Equal(owner.Email, owners[0].Email)
	s.Equal("Late Owner", owners[0].FullName)
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/status"), owner.Token).Code)
}

func (s *billingSuite) TestAccess_MalformedEventIsTerminal() {
	err := s.consumer.Consume(rabbitMessage(constants.TeamMemberAdded, `{"payload":{"team_id":"not-a-uuid"}}`))
	s.Require().Error(err)
	s.ErrorIs(err, access.ErrorMalformedEvent)
}
