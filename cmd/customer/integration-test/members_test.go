package integrationtest

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/pkg/authz"
)

// ── Role matrix on Team routes ───────────────────────────────────────────────

func (s *customerSuite) TestRoles_DeveloperCreatesProject_FinanceIsForbidden_StrangerIsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("roles-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("roles-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	finEmail := s.uniqueEmail("roles-fin")
	finance := s.registerAndVerify(finEmail, "Str0ng!pass")
	s.addMember(teamID, finEmail, authz.RoleFinance)
	stranger := s.registerAndVerify(s.uniqueEmail("roles-stranger"), "Str0ng!pass")

	created := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"By dev","description":""}`, developer.Access)
	s.Equal(http.StatusCreated, created.Code, created.Raw)

	forbidden := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"By finance","description":""}`, finance.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, forbidden.Code, forbidden.Raw)
	s.Equal(http.StatusForbidden, forbidden.Code)
	domain, code := forbidden.errorCode()
	s.Equal(int(team.Domain), domain)
	s.Equal(team.ErrorForbidden.Code, code)

	// finance holds team.read, so the list is open to them.
	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/projects", "", finance.Access)
	s.Equal(http.StatusOK, list.Code, list.Raw)
	s.Len(list.List, 1)

	notFound := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"Sneaky","description":""}`, stranger.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, notFound.Code, notFound.Raw)
	s.Equal(http.StatusNotFound, notFound.Code)
}

func (s *customerSuite) TestRoles_FinanceCannotCreateOrDeleteSSHKeys_ButCanList() {
	owner := s.registerAndVerify(s.uniqueEmail("sshroles-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	finEmail := s.uniqueEmail("sshroles-fin")
	finance := s.registerAndVerify(finEmail, "Str0ng!pass")
	s.addMember(teamID, finEmail, authz.RoleFinance)
	line, _ := s.ed25519Key("")

	created := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("owner", line), owner.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)

	otherLine, _ := s.ed25519Key("")
	forbidden := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("finance", otherLine), finance.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, forbidden.Code, forbidden.Raw)
	_, code := forbidden.errorCode()
	s.Equal(team.ErrorForbidden.Code, code)

	del := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/ssh-keys/"+created.str("id"), "", finance.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, del.Code, del.Raw)

	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/ssh-keys", "", finance.Access)
	s.Equal(http.StatusOK, list.Code, list.Raw)
	s.Len(list.List, 1)
}

// ── Access claims ────────────────────────────────────────────────────────────

func (s *customerSuite) TestAccessClaims_CarryTeamsWithRoleAndPermissionVersion() {
	email := s.uniqueEmail("claims")
	tokens := s.registerAndVerify(email, "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	claims := s.accessClaims(tokens.Access)

	s.Equal("owner", claims.Teams[teamID])
	s.Len(claims.Teams, 1)
	s.Equal(s.permVersion(s.userID(email)), claims.PV)
	s.GreaterOrEqual(claims.PV, int64(1), "sign-up created a Team, which bumped the version")
}

func (s *customerSuite) TestTeams_CreateBumpsThePermissionVersion() {
	email := s.uniqueEmail("pvteam")
	tokens := s.registerAndVerify(email, "Str0ng!pass")
	before := s.permVersion(s.userID(email))

	created := s.authed(http.MethodPost, "/v1/teams", `{"name":"Second"}`, tokens.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)

	s.Equal(before+1, s.permVersion(s.userID(email)))

	refreshed := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Refresh))
	s.Require().Equal(http.StatusOK, refreshed.Code, refreshed.Raw)
	claims := s.accessClaims(tokensOf(refreshed.Body).Access)
	s.Equal("owner", claims.Teams[created.str("id")])
	s.Len(claims.Teams, 2)
	s.Equal(before+1, claims.PV)
}

func (s *customerSuite) TestRefresh_AfterRoleChange_CarriesTheNewRoleAndAHigherVersion() {
	owner := s.registerAndVerify(s.uniqueEmail("refresh-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("refresh-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	devID := s.userID(devEmail)
	// The membership was inserted behind the service's back, so the token
	// minted at verification predates it: a refresh is where it arrives.
	first := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, developer.Refresh))
	s.Require().Equal(http.StatusOK, first.Code, first.Raw)
	developer = tokensOf(first.Body)
	s.Equal("developer", s.accessClaims(developer.Access).Teams[teamID])
	before := s.accessClaims(developer.Access).PV

	changed := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+devID+"/role", `{"role":"finance"}`, owner.Access)
	s.Require().Equal(http.StatusOK, changed.Code, changed.Raw)

	s.Equal(before+1, s.permVersion(devID))
	second := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, developer.Refresh))
	s.Require().Equal(http.StatusOK, second.Code, second.Raw)
	claims := s.accessClaims(tokensOf(second.Body).Access)
	s.Equal("finance", claims.Teams[teamID])
	s.Equal(before+1, claims.PV)
}

// ── Members ──────────────────────────────────────────────────────────────────

func (s *customerSuite) TestMembers_ListShowsEveryMemberWithRole() {
	ownerEmail := s.uniqueEmail("members-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	finEmail := s.uniqueEmail("members-fin")
	finance := s.registerAndVerify(finEmail, "Str0ng!pass")
	s.addMember(teamID, finEmail, authz.RoleFinance)

	// finance holds team.read: every member may see who is in the Team.
	res := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", finance.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Require().Len(res.List, 2)
	byEmail := map[string]map[string]any{}
	for _, m := range res.List {
		email, _ := m["email"].(string)
		byEmail[email] = m
	}
	s.Equal("owner", byEmail[ownerEmail]["role"])
	s.Equal("Test Person", byEmail[ownerEmail]["full_name"])
	s.Equal(s.userID(ownerEmail), byEmail[ownerEmail]["user_id"])
	s.Equal("finance", byEmail[finEmail]["role"])
	s.isRFC3339UTC(byEmail[finEmail]["joined_at"])
}

func (s *customerSuite) TestMembers_StrangerListingMembers_IsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("ml-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	stranger := s.registerAndVerify(s.uniqueEmail("ml-stranger"), "Str0ng!pass")

	res := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", stranger.Access)

	s.Equal(team.ErrorNotMember.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestMembers_ChangeRole_AnswersMemberPublishesEventAndBumpsVersion() {
	ownerEmail := s.uniqueEmail("role-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("role-dev")
	s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	devID := s.userID(devEmail)
	before := s.permVersion(devID)
	s.clearOutbox()

	res := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+devID+"/role", `{"role":"owner"}`, owner.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(devID, res.str("user_id"))
	s.Equal("owner", res.str("role"))
	s.Equal(devEmail, res.str("email"))
	s.NotEmpty(res.str("full_name"))
	s.isRFC3339UTC(res.Body["joined_at"])

	s.Equal(before+1, s.permVersion(devID))
	s.Equal(int64(1), s.outboxCount(constants.TeamMemberRoleChanged))
	payload := s.outboxPayload(constants.TeamMemberRoleChanged)
	s.Equal(teamID, payload["team_id"])
	s.Equal(devID, payload["user_id"])
	s.Equal("owner", payload["role"])
	s.isRFC3339UTC(payload["changed_at"])
	s.Equal(s.userID(ownerEmail), payload["changed_by"], "the person told knows it was not their own doing")

	var stored string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT role FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, devID).Scan(&stored))
	s.Equal("owner", stored)
}

func (s *customerSuite) TestMembers_ChangeRole_UnknownRole_IsRejected() {
	owner := s.registerAndVerify(s.uniqueEmail("badrole-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("badrole-dev")
	s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)

	res := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+s.userID(devEmail)+"/role", `{"role":"admin"}`, owner.Access)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

func (s *customerSuite) TestMembers_ChangeOwnRole_IsAConflict() {
	ownerEmail := s.uniqueEmail("ownrole")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	secondEmail := s.uniqueEmail("ownrole-second")
	s.registerAndVerify(secondEmail, "Str0ng!pass")
	s.addMember(teamID, secondEmail, authz.RoleOwner)

	// Even with another owner present the rule holds: someone else must do it.
	res := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+s.userID(ownerEmail)+"/role", `{"role":"developer"}`, owner.Access)

	s.Equal(team.ErrorOwnRole.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	_, code := res.errorCode()
	s.Equal(team.ErrorOwnRole.Code, code)
}

func (s *customerSuite) TestMembers_DemotedOwner_LosesManagementAtOnce() {
	ownerEmail := s.uniqueEmail("demote-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	secondEmail := s.uniqueEmail("demote-second")
	second := s.registerAndVerify(secondEmail, "Str0ng!pass")
	s.addMember(teamID, secondEmail, authz.RoleOwner)

	demoted := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+s.userID(ownerEmail)+"/role", `{"role":"developer"}`, second.Access)
	s.Require().Equal(http.StatusOK, demoted.Code, demoted.Raw)
	s.Equal("developer", demoted.str("role"))

	// The service reads the Role from the database, so the stale token does
	// not keep the demoted owner in charge.
	refused := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+s.userID(secondEmail)+"/role", `{"role":"finance"}`, owner.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, refused.Code, refused.Raw)
}

func (s *customerSuite) TestMembers_RemoveMember_PublishesEventBumpsVersionAndEndsAccess() {
	ownerEmail := s.uniqueEmail("remove-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("remove-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	devID := s.userID(devEmail)
	s.Require().Equal(http.StatusOK, s.authed(http.MethodGet, "/v1/teams/"+teamID+"/projects", "", developer.Access).Code)
	before := s.permVersion(devID)
	s.clearOutbox()

	res := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/members/"+devID, "", owner.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Empty(res.Raw)
	s.Equal(before+1, s.permVersion(devID))
	s.Equal(int64(1), s.outboxCount(constants.TeamMemberRemoved))
	payload := s.outboxPayload(constants.TeamMemberRemoved)
	s.Equal(teamID, payload["team_id"])
	s.Equal(devID, payload["user_id"])
	s.isRFC3339UTC(payload["removed_at"])
	s.Equal(s.userID(ownerEmail), payload["removed_by"])

	after := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/projects", "", developer.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, after.Code, "the removed member is a stranger now")
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).List, 1)
}

func (s *customerSuite) TestMembers_RemoveTheLastOwner_IsAConflict() {
	ownerEmail := s.uniqueEmail("rmlast-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)

	// Removing oneself is leaving; the sole owner may do neither.
	res := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/members/"+s.userID(ownerEmail), "", owner.Access)

	s.Equal(team.ErrorLastOwner.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	_, code := res.errorCode()
	s.Equal(team.ErrorLastOwner.Code, code)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).List, 1)
}

func (s *customerSuite) TestMembers_OwnerRemovesThemselfWhenAnotherOwnerRemains() {
	ownerEmail := s.uniqueEmail("rmself-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	secondEmail := s.uniqueEmail("rmself-second")
	second := s.registerAndVerify(secondEmail, "Str0ng!pass")
	s.addMember(teamID, secondEmail, authz.RoleOwner)

	res := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/members/"+s.userID(ownerEmail), "", owner.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", second.Access).List, 1)
}

func (s *customerSuite) TestMembers_UnknownMember_IsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("unknown-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	outsiderEmail := s.uniqueEmail("unknown-outsider")
	s.registerAndVerify(outsiderEmail, "Str0ng!pass")

	role := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+s.userID(outsiderEmail)+"/role", `{"role":"developer"}`, owner.Access)
	s.Equal(team.ErrorMemberNotFound.HttpStatus, role.Code, role.Raw)
	s.Equal(http.StatusNotFound, role.Code)
	_, code := role.errorCode()
	s.Equal(team.ErrorMemberNotFound.Code, code)

	del := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/members/"+s.userID(outsiderEmail), "", owner.Access)
	s.Equal(team.ErrorMemberNotFound.HttpStatus, del.Code, del.Raw)
}

func (s *customerSuite) TestMembers_DeveloperCannotManageMembers() {
	owner := s.registerAndVerify(s.uniqueEmail("devmanage-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("devmanage-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	ownerID := s.userID(s.emailOf(owner))

	role := s.authed(http.MethodPut, "/v1/teams/"+teamID+"/members/"+ownerID+"/role", `{"role":"developer"}`, developer.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, role.Code, role.Raw)

	del := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/members/"+ownerID, "", developer.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, del.Code, del.Raw)
}

func (s *customerSuite) TestMembers_Leave_PublishesRemovedAndTheTeamIsGoneForThem() {
	owner := s.registerAndVerify(s.uniqueEmail("leave-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	finEmail := s.uniqueEmail("leave-fin")
	finance := s.registerAndVerify(finEmail, "Str0ng!pass")
	s.addMember(teamID, finEmail, authz.RoleFinance)
	finID := s.userID(finEmail)
	before := s.permVersion(finID)
	s.clearOutbox()

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/members/leave", "", finance.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Equal(before+1, s.permVersion(finID))
	s.Equal(int64(1), s.outboxCount(constants.TeamMemberRemoved))
	s.Equal(finID, s.outboxPayload(constants.TeamMemberRemoved)["user_id"])
	s.Equal(finID, s.outboxPayload(constants.TeamMemberRemoved)["removed_by"], "leaving is removing oneself")

	teams := s.authed(http.MethodGet, "/v1/teams", "", finance.Access)
	s.Require().Len(teams.List, 1, "only their own Team is left")
	s.NotEqual(teamID, teams.List[0]["id"])
	again := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/members/leave", "", finance.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, again.Code, again.Raw)
}

func (s *customerSuite) TestMembers_LastOwnerCannotLeave() {
	owner := s.registerAndVerify(s.uniqueEmail("leave-last"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/members/leave", "", owner.Access)

	s.Equal(team.ErrorLastOwner.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(team.ErrorLastOwner.Code, code)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).List, 1)
}

func (s *customerSuite) TestMembers_OwnerLeavesWhenAnotherOwnerRemains() {
	ownerEmail := s.uniqueEmail("leave-two")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	secondEmail := s.uniqueEmail("leave-two-second")
	s.registerAndVerify(secondEmail, "Str0ng!pass")
	s.addMember(teamID, secondEmail, authz.RoleOwner)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/members/leave", "", owner.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Equal(team.ErrorNotMember.HttpStatus, s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).Code)
}

// Two owners leaving at the same instant must not both see "another owner
// remains": the Team row is locked first, so the second reads a fresh count.
func (s *customerSuite) TestMembers_TwoOwnersLeaveConcurrently_ExactlyOneSucceeds() {
	ownerEmail := s.uniqueEmail("race-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	secondEmail := s.uniqueEmail("race-second")
	second := s.registerAndVerify(secondEmail, "Str0ng!pass")
	s.addMember(teamID, secondEmail, authz.RoleOwner)

	results := make([]*response, 2)
	var wg sync.WaitGroup
	for i, t := range []tokens{owner, second} {
		wg.Add(1)
		go func(i int, t tokens) {
			defer wg.Done()
			results[i] = s.request(http.MethodPost, "/v1/teams/"+teamID+"/members/leave", "", t.Access)
		}(i, t)
	}
	wg.Wait()

	codes := []int{results[0].Code, results[1].Code}
	s.ElementsMatch([]int{http.StatusNoContent, team.ErrorLastOwner.HttpStatus}, codes, "%s | %s", results[0].Raw, results[1].Raw)

	var owners int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT count(*) FROM team_members WHERE team_id = $1 AND role = 'owner'`, teamID).Scan(&owners))
	s.Equal(1, owners, "the Team keeps exactly one owner")
}
