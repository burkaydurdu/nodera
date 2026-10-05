package integrationtest

import (
	"net/http"

	"github.com/piggystats/nodera/cmd/customer/app"
	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
)

func (s *customerSuite) TestTeams_ListsTheAutoCreatedTeam() {
	tokens := s.registerAndVerify(s.uniqueEmail("teams"), "Str0ng!pass")

	res := s.authed(http.MethodGet, "/v1/teams", "", tokens.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Require().Len(res.List, 1)
	s.Equal("Test Person", res.List[0]["name"])
	s.Equal("owner", res.List[0]["role"])
}

func (s *customerSuite) TestProjects_StartEmptyThenCreateAndList() {
	tokens := s.registerAndVerify(s.uniqueEmail("projects"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	empty := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/projects", "", tokens.Access)
	s.Equal(http.StatusOK, empty.Code, empty.Raw)
	s.Len(empty.List, 0)

	created := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"First","description":"my first project"}`, tokens.Access)
	s.Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal("First", created.str("name"))
	s.Equal(teamID, created.str("team_id"))

	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/projects", "", tokens.Access)
	s.Require().Len(list.List, 1)
	s.Equal("my first project", list.List[0]["description"])
}

func (s *customerSuite) TestProjects_DuplicateNameInTeam_IsAConflict() {
	tokens := s.registerAndVerify(s.uniqueEmail("dupproject"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)
	s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"Prod","description":""}`, tokens.Access)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"prod","description":""}`, tokens.Access)

	s.Equal(project.ErrorNameTaken.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(project.ErrorNameTaken.Code, code)
}

func (s *customerSuite) TestProjects_OtherUsersTeam_IsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("owner"), "Str0ng!pass")
	ownerTeam := s.teamID(owner.Access)
	stranger := s.registerAndVerify(s.uniqueEmail("stranger"), "Str0ng!pass")

	list := s.authed(http.MethodGet, "/v1/teams/"+ownerTeam+"/projects", "", stranger.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, list.Code, list.Raw)

	create := s.authed(http.MethodPost, "/v1/teams/"+ownerTeam+"/projects", `{"name":"Sneaky","description":""}`, stranger.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, create.Code, create.Raw)
}

func (s *customerSuite) TestProjects_EmptyName_IsRejected() {
	tokens := s.registerAndVerify(s.uniqueEmail("emptyname"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"   ","description":""}`, tokens.Access)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

func (s *customerSuite) TestTeams_CreateAddsAnOwnedTeamWithItsOwnProjects() {
	tokens := s.registerAndVerify(s.uniqueEmail("newteam"), "Str0ng!pass")
	firstTeam := s.teamID(tokens.Access)

	created := s.authed(http.MethodPost, "/v1/teams", `{"name":"  Platform  "}`, tokens.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal("Platform", created.str("name"))
	s.Equal("owner", created.str("role"))
	secondTeam := created.str("id")
	s.NotEqual(firstTeam, secondTeam)

	list := s.authed(http.MethodGet, "/v1/teams", "", tokens.Access)
	s.Require().Len(list.List, 2)

	project := s.authed(http.MethodPost, "/v1/teams/"+secondTeam+"/projects", `{"name":"Only here","description":""}`, tokens.Access)
	s.Require().Equal(http.StatusCreated, project.Code, project.Raw)

	s.Len(s.authed(http.MethodGet, "/v1/teams/"+secondTeam+"/projects", "", tokens.Access).List, 1)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+firstTeam+"/projects", "", tokens.Access).List, 0, "projects belong to one Team")
}

func (s *customerSuite) TestTeams_CreateWithBlankName_IsRejected() {
	tokens := s.registerAndVerify(s.uniqueEmail("blankteam"), "Str0ng!pass")

	res := s.authed(http.MethodPost, "/v1/teams", `{"name":"   "}`, tokens.Access)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

// ── Domain events ────────────────────────────────────────────────────────────

func (s *customerSuite) TestRegister_PublishesTeamCreatedAndOwnerMembership() {
	email := s.uniqueEmail("teamevents")
	tokens := s.registerAndVerify(email, "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	var userID string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID))

	s.Equal(int64(1), s.outboxCount(constants.TeamCreated))
	created := s.outboxPayload(constants.TeamCreated)
	s.Equal(teamID, created["team_id"])
	s.Equal(userID, created["owner_user_id"])
	s.Equal("Test Person", created["name"], "the first Team is named after its owner")
	s.Equal(email, created["owner_email"])
	s.Equal("Test Person", created["owner_full_name"])
	s.Equal("en", created["owner_locale"])
	s.isRFC3339UTC(created["created_at"])

	s.Equal(int64(1), s.outboxCount(constants.TeamMemberAdded))
	added := s.outboxPayload(constants.TeamMemberAdded)
	s.Equal(teamID, added["team_id"])
	s.Equal(userID, added["user_id"])
	s.Equal("owner", added["role"])
	s.Equal(email, added["email"])
	s.Equal("Test Person", added["full_name"])
	s.Equal("en", added["locale"])
	s.isRFC3339UTC(added["added_at"])

	s.Equal(int64(0), s.outboxCount(constants.ProjectCreated))
}

func (s *customerSuite) TestTeams_CreatePublishesTeamCreatedAndOwnerMembership() {
	email := s.uniqueEmail("teamcreateevents")
	tokens := s.registerAndVerify(email, "Str0ng!pass")
	s.clearOutbox()

	created := s.authed(http.MethodPost, "/v1/teams", `{"name":"Platform"}`, tokens.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)

	s.Equal(int64(1), s.outboxCount(constants.TeamCreated))
	published := s.outboxPayload(constants.TeamCreated)
	s.Equal(created.str("id"), published["team_id"])
	s.Equal(s.userID(email), published["owner_user_id"])
	s.Equal("Platform", published["name"])
	s.Equal(email, published["owner_email"])
	s.Equal("Test Person", published["owner_full_name"])
	s.Equal("en", published["owner_locale"])

	s.Equal(int64(1), s.outboxCount(constants.TeamMemberAdded))
	added := s.outboxPayload(constants.TeamMemberAdded)
	s.Equal(created.str("id"), added["team_id"])
	s.Equal(s.userID(email), added["user_id"])
	s.Equal("owner", added["role"])
	s.Equal(email, added["email"])
	s.Equal("Test Person", added["full_name"])
	s.Equal("en", added["locale"])
}

func (s *customerSuite) TestTeams_RejectedCreatePublishesNothing() {
	tokens := s.registerAndVerify(s.uniqueEmail("noevents"), "Str0ng!pass")
	s.clearOutbox()

	res := s.authed(http.MethodPost, "/v1/teams", `{"name":"   "}`, tokens.Access)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
	s.Equal(int64(0), s.outboxCount(constants.TeamCreated))
	s.Equal(int64(0), s.outboxCount(constants.TeamMemberAdded))
}

func (s *customerSuite) TestProjects_CreatePublishesProjectCreated() {
	tokens := s.registerAndVerify(s.uniqueEmail("projectevents"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)
	s.clearOutbox()

	created := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"First","description":""}`, tokens.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)

	s.Equal(int64(1), s.outboxCount(constants.ProjectCreated))
	payload := s.outboxPayload(constants.ProjectCreated)
	s.Equal(created.str("id"), payload["project_id"])
	s.Equal(teamID, payload["team_id"])
	s.Equal("First", payload["name"], "mail names the Project on Invoice lines and Notifications from the event alone")
	s.isRFC3339UTC(payload["created_at"])

	duplicate := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", `{"name":"first","description":""}`, tokens.Access)
	s.Equal(project.ErrorNameTaken.HttpStatus, duplicate.Code, duplicate.Raw)
	s.Equal(int64(1), s.outboxCount(constants.ProjectCreated), "a rejected create publishes nothing")
}

// ── replay-projects ──────────────────────────────────────────────────────────

func (s *customerSuite) TestReplayProjects_RepublishesEveryTeamMembershipAndProject() {
	ownerEmail := s.uniqueEmail("replay")
	tokens := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	firstTeam := s.teamID(tokens.Access)
	secondTeam := s.authed(http.MethodPost, "/v1/teams", `{"name":"Second"}`, tokens.Access).str("id")
	projectID := s.authed(http.MethodPost, "/v1/teams/"+firstTeam+"/projects", `{"name":"P1","description":""}`, tokens.Access).str("id")
	s.authed(http.MethodPost, "/v1/teams/"+secondTeam+"/projects", `{"name":"P2","description":""}`, tokens.Access)
	finEmail := s.uniqueEmail("replay-fin")
	s.registerAndVerify(finEmail, "Str0ng!pass")
	s.addMember(firstTeam, finEmail, authz.RoleFinance)
	s.clearOutbox()

	var teams, members, projects int64
	ctx := s.T().Context()
	s.Require().NoError(s.pgPool.QueryRow(ctx, `SELECT count(*) FROM teams WHERE deleted_at IS NULL`).Scan(&teams))
	s.Require().NoError(s.pgPool.QueryRow(ctx, `SELECT count(*) FROM team_members`).Scan(&members))
	s.Require().NoError(s.pgPool.QueryRow(ctx, `SELECT count(*) FROM projects WHERE deleted_at IS NULL`).Scan(&projects))
	s.Require().GreaterOrEqual(teams, int64(3), "the seed Team plus the two created here")

	summary, err := app.ReplayProjects(ctx, logger.Noop(), s.pgPool, s.outbox)

	s.Require().NoError(err)
	s.Equal(int(teams), summary.Teams)
	s.Equal(int(members), summary.Members)
	s.Equal(int(projects), summary.Projects)
	s.Equal(teams, s.outboxCount(constants.TeamCreated))
	s.Equal(members, s.outboxCount(constants.TeamMemberAdded))
	s.Equal(projects, s.outboxCount(constants.ProjectCreated))

	s.Contains(s.outboxPayloads(constants.ProjectCreated, "project_id"), projectID)
	s.Contains(s.outboxPayloads(constants.TeamCreated, "team_id"), firstTeam)
	s.Contains(s.outboxPayloads(constants.TeamCreated, "team_id"), secondTeam)
	s.Contains(s.outboxPayloads(constants.TeamMemberAdded, "team_id"), secondTeam)
	s.Contains(s.outboxPayloads(constants.TeamMemberAdded, "role"), "finance", "a replayed membership carries its current Role")

	replayedTeam := s.outboxPayloadWhere(constants.TeamCreated, map[string]any{"team_id": secondTeam})
	s.Equal("Second", replayedTeam["name"])
	s.Equal(s.userID(ownerEmail), replayedTeam["owner_user_id"])
	s.Equal(ownerEmail, replayedTeam["owner_email"], "a replayed Team carries its owner as the users table knows them")
	s.Equal("Test Person", replayedTeam["owner_full_name"])
	s.Equal("en", replayedTeam["owner_locale"])

	replayedProject := s.outboxPayloadWhere(constants.ProjectCreated, map[string]any{"project_id": projectID})
	s.Equal(firstTeam, replayedProject["team_id"])
	s.Equal("P1", replayedProject["name"], "a replay names the Projects that predate the name in the event")
	s.isRFC3339UTC(replayedProject["created_at"])

	replayedMember := s.outboxPayloadWhere(constants.TeamMemberAdded, map[string]any{"team_id": firstTeam, "user_id": s.userID(finEmail)})
	s.Equal("finance", replayedMember["role"])
	s.Equal(finEmail, replayedMember["email"])
	s.Equal("Test Person", replayedMember["full_name"])
	s.Equal("en", replayedMember["locale"])
}
