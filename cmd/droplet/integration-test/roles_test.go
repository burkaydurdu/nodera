package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/access"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
)

// The membership events reach this context on separate queues, so the
// scenarios stamp them explicitly: the read model must follow the newest
// event, whatever order they arrive in.
var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func stamp(d time.Duration) string {
	return t0.Add(d).Format(time.RFC3339Nano)
}

// addMember replays the customer's TeamMemberAdded for a second User.
func (s *dropletSuite) addMember(teamID uuid.UUID, member user, role string) {
	s.addMemberAt(teamID, member, role, stamp(0))
}

func (s *dropletSuite) addMemberAt(teamID uuid.UUID, member user, role, at string) {
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"added_at":%q}}`,
		teamID, member.ID, role, at))
}

func (s *dropletSuite) changeRole(teamID uuid.UUID, member user, role string) {
	s.changeRoleAt(teamID, member, role, time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *dropletSuite) changeRoleAt(teamID uuid.UUID, member user, role, at string) {
	s.consume(constants.TeamMemberRoleChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"changed_at":%q}}`,
		teamID, member.ID, role, at))
}

func (s *dropletSuite) removeMember(teamID uuid.UUID, member user) {
	s.removeMemberAt(teamID, member, time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *dropletSuite) removeMemberAt(teamID uuid.UUID, member user, at string) {
	s.consume(constants.TeamMemberRemoved, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"removed_at":%q}}`,
		teamID, member.ID, at))
}

func (s *dropletSuite) listCode(projectID uuid.UUID, u user) int {
	return s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).Code
}

func (s *dropletSuite) assertForbidden(res *response) {
	s.T().Helper()
	s.Equal(droplet.ErrorForbidden.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(droplet.Domain), domain)
	s.Equal(droplet.ErrorForbidden.Code, code)
	s.Equal(droplet.ErrorForbidden.Message, res.str("message"))
}

func (s *dropletSuite) TestRoles_FinanceReadsButNeverWrites() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	id := s.activeDroplet(owner, projectID, "books")
	finance := s.signIn()
	s.addMember(teamID, finance, "finance")

	list := s.get("/v1/projects/"+projectID.String()+"/droplets", finance.Token)
	s.Equal(http.StatusOK, list.Code, list.Raw)
	s.Len(list.List, 1)
	detail := s.get("/v1/droplets/"+id, finance.Token)
	s.Equal(http.StatusOK, detail.Code, detail.Raw)
	s.Equal("active", detail.str("status"))

	s.assertForbidden(s.createDroplet(finance, projectID, "sneaky"))
	s.assertForbidden(s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"power_off"}`, finance.Token))
	s.assertForbidden(s.request(http.MethodDelete, "/v1/droplets/"+id, "", finance.Token))

	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", owner.Token).List, 1, "nothing was created")
	after := s.get("/v1/droplets/"+id, owner.Token)
	s.Equal("active", after.str("status"), "nothing happened to it")
	s.Nil(after.Body["pending_action"])
}

func (s *dropletSuite) TestRoles_RoleChangeToDeveloperAllowsTheSameCreate() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	member := s.signIn()
	s.addMember(teamID, member, "finance")
	s.assertForbidden(s.createDroplet(member, projectID, "web-1"))

	s.changeRole(teamID, member, "developer")

	created := s.createDroplet(member, projectID, "web-1")
	s.Equal(http.StatusCreated, created.Code, created.Raw)

	s.changeRole(teamID, member, "developer")
	s.Equal(http.StatusCreated, s.createDroplet(member, projectID, "web-2").Code, "a redelivered change is harmless")

	s.changeRole(teamID, member, "finance")
	s.assertForbidden(s.request(http.MethodDelete, "/v1/droplets/"+created.str("id"), "", member.Token))
	s.Equal(http.StatusOK, s.get("/v1/droplets/"+created.str("id"), member.Token).Code, "a demoted member still reads")
}

func (s *dropletSuite) TestRoles_RemovedMemberLosesTheProject() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	id := s.activeDroplet(owner, projectID, "web-1")
	member := s.signIn()
	s.addMember(teamID, member, "developer")
	s.Require().Equal(http.StatusOK, s.get("/v1/projects/"+projectID.String()+"/droplets", member.Token).Code)

	s.removeMember(teamID, member)

	list := s.get("/v1/projects/"+projectID.String()+"/droplets", member.Token)
	s.Equal(access.ErrorProjectNotFound.HttpStatus, list.Code, list.Raw)
	_, code := list.errorCode()
	s.Equal(access.ErrorProjectNotFound.Code, code)
	s.Equal(http.StatusNotFound, s.get("/v1/droplets/"+id, member.Token).Code, "a 404, never a 403: they are a stranger again")
	s.Equal(http.StatusNotFound, s.createDroplet(member, projectID, "web-2").Code)

	s.removeMember(teamID, member)
	s.Equal(http.StatusOK, s.get("/v1/projects/"+projectID.String()+"/droplets", owner.Token).Code, "the owner is untouched")
}

func (s *dropletSuite) TestRoles_UnknownRoleHoldsNothing() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	id := s.activeDroplet(owner, projectID, "web-1")
	member := s.signIn()
	s.addMember(teamID, member, "auditor")

	s.assertForbidden(s.get("/v1/projects/"+projectID.String()+"/droplets", member.Token))
	s.assertForbidden(s.get("/v1/droplets/"+id, member.Token))
	s.assertForbidden(s.createDroplet(member, projectID, "web-2"))

	s.changeRole(teamID, member, "developer")
	s.Equal(http.StatusOK, s.get("/v1/projects/"+projectID.String()+"/droplets", member.Token).Code, "the row was there all along")

	s.changeRole(teamID, member, "superuser")
	s.assertForbidden(s.get("/v1/projects/"+projectID.String()+"/droplets", member.Token))
}

func (s *dropletSuite) TestRoles_ChangeForAStrangerIsHarmless() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	stranger := s.signIn()

	s.changeRole(teamID, stranger, "developer")
	s.removeMember(teamID, stranger)

	s.Equal(http.StatusNotFound, s.get("/v1/projects/"+projectID.String()+"/droplets", stranger.Token).Code, "a change never creates a membership")
	s.Equal(http.StatusOK, s.get("/v1/projects/"+projectID.String()+"/droplets", owner.Token).Code)
}

func (s *dropletSuite) TestRoles_MalformedEventsAreRefusedAsSuch() {
	teamID := uuid.New()

	for _, payload := range []string{
		`not json`,
		`{"payload":{"team_id":"` + teamID.String() + `","role":"developer"}}`,
	} {
		err := s.consumeErr(constants.TeamMemberRoleChanged, payload)
		s.Require().Error(err)
		var rerr rerror.Error
		s.Require().ErrorAs(err, &rerr)
		s.Equal(access.ErrorMalformedEvent.Code, rerr.Code, payload)
	}

	err := s.consumeErr(constants.TeamMemberRemoved, `{"payload":{"user_id":"`+uuid.New().String()+`"}}`)
	var rerr rerror.Error
	s.Require().ErrorAs(err, &rerr)
	s.Equal(access.ErrorMalformedEvent.Code, rerr.Code)
}

// ── Ordering: the newest event wins, whatever order the queues deliver ───────

func (s *dropletSuite) TestRoles_AnOlderAddedCannotResurrectARemovedMember() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	member := s.signIn()
	s.addMemberAt(teamID, member, "developer", stamp(0))
	s.removeMemberAt(teamID, member, stamp(2*time.Second))
	s.Require().Equal(http.StatusNotFound, s.listCode(projectID, member))

	s.addMemberAt(teamID, member, "developer", stamp(1*time.Second))
	s.Equal(http.StatusNotFound, s.listCode(projectID, member), "a late copy of the old membership changes nothing")

	s.removeMemberAt(teamID, member, stamp(2*time.Second))
	s.Equal(http.StatusNotFound, s.listCode(projectID, member), "a redelivered removal is harmless")

	s.addMemberAt(teamID, member, "finance", stamp(3*time.Second))
	s.Equal(http.StatusOK, s.listCode(projectID, member), "invited again after the removal")
	s.assertForbidden(s.createDroplet(member, projectID, "web-1"))
}

func (s *dropletSuite) TestRoles_ARemovalThatArrivesFirstStillWins() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	member := s.signIn()

	s.removeMemberAt(teamID, member, stamp(2*time.Second))
	s.addMemberAt(teamID, member, "developer", stamp(1*time.Second))

	s.Equal(http.StatusNotFound, s.listCode(projectID, member))
	s.Equal(http.StatusNotFound, s.createDroplet(member, projectID, "web-1").Code)
}

func (s *dropletSuite) TestRoles_AnOlderRoleChangeIsIgnored() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	member := s.signIn()
	s.addMemberAt(teamID, member, "finance", stamp(0))
	s.changeRoleAt(teamID, member, "developer", stamp(2*time.Second))

	s.changeRoleAt(teamID, member, "finance", stamp(1*time.Second))
	s.Equal(http.StatusCreated, s.createDroplet(member, projectID, "web-1").Code, "the newer change stands")

	s.changeRoleAt(teamID, member, "developer", stamp(2*time.Second))
	s.Equal(http.StatusCreated, s.createDroplet(member, projectID, "web-2").Code, "a redelivered change is harmless")

	s.addMemberAt(teamID, member, "finance", stamp(0))
	s.Equal(http.StatusCreated, s.createDroplet(member, projectID, "web-3").Code, "a replayed older membership does not roll the Role back")
}

func (s *dropletSuite) TestRoles_ARoleChangeAfterARemovalMeansReinvited() {
	// The customer re-invited the member and changed their Role, and the two
	// later events overtook the removal's effect in different orders.
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	member := s.signIn()
	s.addMemberAt(teamID, member, "developer", stamp(0))
	s.removeMemberAt(teamID, member, stamp(1*time.Second))
	s.changeRoleAt(teamID, member, "finance", stamp(3*time.Second))
	s.addMemberAt(teamID, member, "developer", stamp(2*time.Second))

	s.Equal(http.StatusOK, s.listCode(projectID, member))
	s.assertForbidden(s.createDroplet(member, projectID, "web-1"))
}

func (s *dropletSuite) TestRoles_ARoleChangeOlderThanTheRemovalIsIgnored() {
	owner := s.signIn()
	teamID, projectID := s.grantProject(owner)
	member := s.signIn()
	s.addMemberAt(teamID, member, "finance", stamp(0))
	s.removeMemberAt(teamID, member, stamp(2*time.Second))

	s.changeRoleAt(teamID, member, "developer", stamp(1*time.Second))

	s.Equal(http.StatusNotFound, s.listCode(projectID, member))
}

func (s *dropletSuite) TestRoles_TeamCreatedAloneGrantsNothing() {
	creator := s.signIn()
	teamID, projectID := uuid.New(), uuid.New()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"created_at":%q}}`, teamID, creator.ID, now))
	s.consume(constants.ProjectCreated, fmt.Sprintf(`{"payload":{"project_id":%q,"team_id":%q,"created_at":%q}}`, projectID, teamID, now))

	s.Equal(http.StatusNotFound, s.listCode(projectID, creator), "only TeamMemberAdded grants access")
}

func (s *dropletSuite) TestRoles_AReplayedTeamCreatedDoesNotRestoreADepartedCreator() {
	creator := s.signIn()
	teamID, projectID := s.grantProject(creator)
	s.removeMember(teamID, creator)
	s.Require().Equal(http.StatusNotFound, s.listCode(projectID, creator))

	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"created_at":%q}}`,
		teamID, creator.ID, time.Now().UTC().Format(time.RFC3339Nano)))

	s.Equal(http.StatusNotFound, s.listCode(projectID, creator))
}
