package integrationtest

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/authz"
)

const acceptPath = "/invitations/accept?token="

func (s *customerSuite) invite(ownerAccess, teamID, email string, role authz.Role) *response {
	return s.authed(http.MethodPost, "/v1/teams/"+teamID+"/invitations", fmt.Sprintf(`{"email":%q,"role":%q}`, email, role), ownerAccess)
}

// latestAcceptToken reads the token the newest TeamInvitation email carries,
// the way the invitee would from their inbox.
func (s *customerSuite) latestAcceptToken() string {
	acceptURL, _ := s.outboxPayload(constants.TeamInvitation)["accept_url"].(string)
	s.Require().True(strings.HasPrefix(acceptURL, frontURL+acceptPath), acceptURL)

	return strings.TrimPrefix(acceptURL, frontURL+acceptPath)
}

func (s *customerSuite) ageInvitation(id string, by time.Duration) {
	_, err := s.pgPool.Exec(s.T().Context(),
		`UPDATE invitations SET expires_at = expires_at - $2::interval, last_sent_at = last_sent_at - $2::interval WHERE id = $1`, id, by)
	s.Require().NoError(err)
}

// ── Create ───────────────────────────────────────────────────────────────────

func (s *customerSuite) TestInvitations_Create_AnswersInvitationAndQueuesTheEmail() {
	ownerEmail := s.uniqueEmail("inv-owner")
	owner := s.registerAndVerify(ownerEmail, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	invitee := strings.ToUpper(s.uniqueEmail("inv-new"))

	res := s.invite(owner.Access, teamID, "  "+invitee+" ", authz.RoleDeveloper)

	s.Equal(http.StatusCreated, res.Code, res.Raw)
	s.NotEmpty(res.str("id"))
	s.Equal(teamID, res.str("team_id"))
	s.Equal("Test Person", res.str("team_name"))
	s.Equal(strings.ToLower(invitee), res.str("email"), "the address is folded")
	s.Equal("developer", res.str("role"))
	invitedBy, _ := res.Body["invited_by"].(map[string]any)
	s.Equal(s.userID(ownerEmail), invitedBy["user_id"])
	s.Equal("Test Person", invitedBy["full_name"])
	s.Equal("pending", res.str("status"))
	s.Nil(res.Body["responded_at"])
	s.isRFC3339UTC(res.Body["created_at"])
	expiresAt, err := time.Parse(time.RFC3339Nano, res.str("expires_at"))
	s.Require().NoError(err)
	s.InDelta(invitation.TTL.Hours(), time.Until(expiresAt).Hours(), 0.1, "seven days")

	s.Equal(int64(1), s.outboxCount(constants.TeamInvitation))
	payload := s.outboxPayload(constants.TeamInvitation)
	recipient, _ := payload["recipient"].(map[string]any)
	s.Equal(strings.ToLower(invitee), recipient["email"])
	s.Equal("en", recipient["locale"], "no account for the address: the inviter's Locale")
	s.Equal("00000000-0000-0000-0000-000000000000", recipient["user_id"])
	s.Equal("Test Person", payload["team_name"])
	s.Equal("developer", payload["role"])
	s.Equal("Test Person", payload["inviter_name"])
	s.isRFC3339UTC(payload["expires_at"])
	token := s.latestAcceptToken()
	s.Len(token, 43, "32 random bytes, base64url without padding")
	s.NotContains(token, "=")

	var tokenHash string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT token_hash FROM invitations WHERE id = $1`, res.str("id")).Scan(&tokenHash))
	s.NotEqual(token, tokenHash, "only the hash is stored")
	s.Len(tokenHash, 64)
}

func (s *customerSuite) TestInvitations_Create_ForAnExistingUser_UsesTheirLocaleAndID() {
	owner := s.registerAndVerify(s.uniqueEmail("inv-owner-tr"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("inv-existing")
	s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ayşe Yılmaz","country":"tr","locale":"tr"}`, inviteeEmail))
	s.clearOutbox()

	res := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleFinance)

	s.Require().Equal(http.StatusCreated, res.Code, res.Raw)
	recipient, _ := s.outboxPayload(constants.TeamInvitation)["recipient"].(map[string]any)
	s.Equal(s.userID(inviteeEmail), recipient["user_id"])
	s.Equal("tr", recipient["locale"])
	s.Equal("Ayşe Yılmaz", recipient["full_name"])
}

func (s *customerSuite) TestInvitations_Create_DuplicatePending_IsAConflict() {
	owner := s.registerAndVerify(s.uniqueEmail("inv-dup-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	invitee := s.uniqueEmail("inv-dup")
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, invitee, authz.RoleDeveloper).Code)

	res := s.invite(owner.Access, teamID, strings.ToUpper(invitee), authz.RoleFinance)

	s.Equal(invitation.ErrorAlreadyPending.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	domain, code := res.errorCode()
	s.Equal(int(invitation.Domain), domain)
	s.Equal(invitation.ErrorAlreadyPending.Code, code)
	s.Equal(int64(1), s.outboxCount(constants.TeamInvitation))
}

func (s *customerSuite) TestInvitations_Create_AlreadyMember_IsAConflict() {
	owner := s.registerAndVerify(s.uniqueEmail("inv-mem-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("inv-mem-dev")
	s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)

	res := s.invite(owner.Access, teamID, devEmail, authz.RoleFinance)

	s.Equal(invitation.ErrorAlreadyMember.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	_, code := res.errorCode()
	s.Equal(invitation.ErrorAlreadyMember.Code, code)
}

func (s *customerSuite) TestInvitations_Create_NeedsTeamManage() {
	owner := s.registerAndVerify(s.uniqueEmail("inv-perm-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("inv-perm-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	stranger := s.registerAndVerify(s.uniqueEmail("inv-perm-stranger"), "Str0ng!pass")

	forbidden := s.invite(developer.Access, teamID, s.uniqueEmail("x"), authz.RoleDeveloper)
	s.Equal(team.ErrorForbidden.HttpStatus, forbidden.Code, forbidden.Raw)

	notFound := s.invite(stranger.Access, teamID, s.uniqueEmail("y"), authz.RoleDeveloper)
	s.Equal(team.ErrorNotMember.HttpStatus, notFound.Code, notFound.Raw)

	unknownRole := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/invitations", `{"email":"a@example.com","role":"admin"}`, owner.Access)
	s.Equal(http.StatusBadRequest, unknownRole.Code, unknownRole.Raw)
}

func (s *customerSuite) TestInvitations_Create_TwentyFirstInATeamWithinAnHour_IsRateLimited() {
	owner := s.registerAndVerify(s.uniqueEmail("inv-burst-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)

	var last *response
	for i := 0; i < 21; i++ {
		last = s.invite(owner.Access, teamID, s.uniqueEmail(fmt.Sprintf("burst-%d", i)), authz.RoleDeveloper)
	}

	s.Equal(apperror.ErrorRateLimited.HttpStatus, last.Code, last.Raw)
	_, code := last.errorCode()
	s.Equal(apperror.ErrorRateLimited.Code, code)
	s.NotEmpty(last.Hdr.Get("Retry-After"))
	s.Equal(int64(20), s.outboxCount(constants.TeamInvitation))
}

// ── Accept ───────────────────────────────────────────────────────────────────

func (s *customerSuite) TestInvitations_AcceptByToken_JoinsWithTheRoleBumpsVersionAndPublishes() {
	owner := s.registerAndVerify(s.uniqueEmail("acc-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("acc-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	inviteeID := s.userID(inviteeEmail)
	created := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleFinance)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	token := s.latestAcceptToken()
	before := s.permVersion(inviteeID)
	s.clearOutbox()

	res := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, token), invitee.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	joined, _ := res.Body["team"].(map[string]any)
	s.Equal(teamID, joined["id"])
	s.Equal("Test Person", joined["name"])
	s.Equal("finance", joined["role"])

	var role, status, acceptedBy string
	var respondedAt *time.Time
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT m.role, i.status, i.accepted_by, i.responded_at FROM team_members m JOIN invitations i ON i.team_id = m.team_id AND i.accepted_by = m.user_id WHERE m.team_id = $1 AND m.user_id = $2`,
		teamID, inviteeID).Scan(&role, &status, &acceptedBy, &respondedAt))
	s.Equal("finance", role)
	s.Equal("accepted", status)
	s.Equal(inviteeID, acceptedBy)
	s.NotNil(respondedAt)

	s.Equal(before+1, s.permVersion(inviteeID))
	s.Equal(int64(1), s.outboxCount(constants.TeamMemberAdded))
	added := s.outboxPayload(constants.TeamMemberAdded)
	s.Equal(teamID, added["team_id"])
	s.Equal(inviteeID, added["user_id"])
	s.Equal("finance", added["role"])
	s.Equal(inviteeEmail, added["email"])
	s.Equal("Test Person", added["full_name"])
	s.Equal("en", added["locale"])
	s.isRFC3339UTC(added["added_at"])

	refreshed := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, invitee.Refresh))
	s.Require().Equal(http.StatusOK, refreshed.Code, refreshed.Raw)
	claims := s.accessClaims(tokensOf(refreshed.Body).Access)
	s.Equal("finance", claims.Teams[teamID])
	s.Equal(before+1, claims.PV)

	teams := s.authed(http.MethodGet, "/v1/teams", "", tokensOf(refreshed.Body).Access)
	s.Len(teams.List, 2)

	// The owner sees the answer; the token is spent.
	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access)
	s.Require().Len(list.List, 1)
	s.Equal("accepted", list.List[0]["status"])
	s.NotNil(list.List[0]["responded_at"])
	again := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, token), invitee.Access)
	s.Equal(invitation.ErrorNoLongerValid.HttpStatus, again.Code, again.Raw)
}

func (s *customerSuite) TestInvitations_AcceptByToken_ForAnotherEmail_IsForbidden() {
	owner := s.registerAndVerify(s.uniqueEmail("acc-wrong-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, s.uniqueEmail("acc-wrong-invitee"), authz.RoleDeveloper).Code)
	token := s.latestAcceptToken()
	other := s.registerAndVerify(s.uniqueEmail("acc-wrong-other"), "Str0ng!pass")
	s.clearOutbox()

	res := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, token), other.Access)

	s.Equal(invitation.ErrorForAnotherEmail.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusForbidden, res.Code)
	_, code := res.errorCode()
	s.Equal(invitation.ErrorForAnotherEmail.Code, code)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).List, 1)
	s.Equal(int64(0), s.outboxCount(constants.TeamMemberAdded))
	s.Equal("pending", s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access).List[0]["status"], "the offer stays open for its real invitee")
}

func (s *customerSuite) TestInvitations_Accept_RevokedOrExpired_IsGone() {
	owner := s.registerAndVerify(s.uniqueEmail("acc-gone-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("acc-gone-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")

	revoked := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper)
	s.Require().Equal(http.StatusCreated, revoked.Code, revoked.Raw)
	revokedToken := s.latestAcceptToken()
	del := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/invitations/"+revoked.str("id"), "", owner.Access)
	s.Require().Equal(http.StatusNoContent, del.Code, del.Raw)

	res := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, revokedToken), invitee.Access)
	s.Equal(invitation.ErrorNoLongerValid.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusGone, res.Code)
	_, code := res.errorCode()
	s.Equal(invitation.ErrorNoLongerValid.Code, code)

	// The revoked one no longer blocks a new offer to the same address.
	expired := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper)
	s.Require().Equal(http.StatusCreated, expired.Code, expired.Raw)
	expiredToken := s.latestAcceptToken()
	s.ageInvitation(expired.str("id"), invitation.TTL+time.Minute)

	res = s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, expiredToken), invitee.Access)
	s.Equal(invitation.ErrorNoLongerValid.HttpStatus, res.Code, res.Raw)

	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access)
	s.Require().Len(list.List, 2)
	s.Equal("expired", list.List[0]["status"], "newest first; expiry is computed, not stored")
	s.Equal("revoked", list.List[1]["status"])
	s.Len(s.authed(http.MethodGet, "/v1/users/me/invitations", "", invitee.Access).List, 0)

	// An expired offer does not block a fresh one either.
	s.Equal(http.StatusCreated, s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper).Code)
}

func (s *customerSuite) TestInvitations_AcceptByToken_AlreadyAMember_MarksAcceptedAndAnswersOK() {
	owner := s.registerAndVerify(s.uniqueEmail("acc-already-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("acc-already-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, inviteeEmail, authz.RoleFinance).Code)
	token := s.latestAcceptToken()
	// They were seated in the meantime, as a developer.
	s.addMember(teamID, inviteeEmail, authz.RoleDeveloper)
	s.clearOutbox()

	res := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, token), invitee.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	joined, _ := res.Body["team"].(map[string]any)
	s.Equal("developer", joined["role"], "the existing Role stands")
	s.Equal(int64(0), s.outboxCount(constants.TeamMemberAdded))
	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access)
	s.Equal("accepted", list.List[0]["status"])
}

func (s *customerSuite) TestInvitations_AcceptByToken_UnknownToken_IsNotFound() {
	invitee := s.registerAndVerify(s.uniqueEmail("acc-unknown"), "Str0ng!pass")

	res := s.authed(http.MethodPost, "/v1/invitations/accept", `{"token":"bm90LWEtcmVhbC10b2tlbg"}`, invitee.Access)

	s.Equal(invitation.ErrorNotFound.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(invitation.ErrorNotFound.Code, code)
}

func (s *customerSuite) TestInvitations_AcceptByToken_TwentyFirstAttemptFromOneAddress_IsRateLimited() {
	invitee := s.registerAndVerify(s.uniqueEmail("acc-burst"), "Str0ng!pass")

	var last *response
	for i := 0; i < 21; i++ {
		last = s.authed(http.MethodPost, "/v1/invitations/accept", `{"token":"bm90LWEtcmVhbC10b2tlbg"}`, invitee.Access)
	}

	s.Equal(apperror.ErrorRateLimited.HttpStatus, last.Code, last.Raw)
}

func (s *customerSuite) TestInvitations_AcceptByID_FromTheInAppList_JoinsTheTeam() {
	owner := s.registerAndVerify(s.uniqueEmail("accid-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("accid-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper).Code)

	mine := s.authed(http.MethodGet, "/v1/users/me/invitations", "", invitee.Access)
	s.Require().Equal(http.StatusOK, mine.Code, mine.Raw)
	s.Require().Len(mine.List, 1)
	id, _ := mine.List[0]["id"].(string)
	s.Equal("Test Person", mine.List[0]["team_name"])
	s.Equal("developer", mine.List[0]["role"])

	res := s.authed(http.MethodPost, "/v1/users/me/invitations/"+id+"/accept", "", invitee.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	joined, _ := res.Body["team"].(map[string]any)
	s.Equal(teamID, joined["id"])
	s.Equal("developer", joined["role"])
	s.Len(s.authed(http.MethodGet, "/v1/users/me/invitations", "", invitee.Access).List, 0)
}

func (s *customerSuite) TestInvitations_AcceptByID_SomeoneElses_IsForbidden() {
	owner := s.registerAndVerify(s.uniqueEmail("accid-wrong-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	created := s.invite(owner.Access, teamID, s.uniqueEmail("accid-wrong-invitee"), authz.RoleDeveloper)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	other := s.registerAndVerify(s.uniqueEmail("accid-wrong-other"), "Str0ng!pass")

	res := s.authed(http.MethodPost, "/v1/users/me/invitations/"+created.str("id")+"/accept", "", other.Access)

	s.Equal(invitation.ErrorForAnotherEmail.HttpStatus, res.Code, res.Raw)
}

// ── Decline ──────────────────────────────────────────────────────────────────

func (s *customerSuite) TestInvitations_DeclineByID_IsListedAsDeclinedForTheOwner() {
	owner := s.registerAndVerify(s.uniqueEmail("dec-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("dec-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	created := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.clearOutbox()

	res := s.authed(http.MethodPost, "/v1/users/me/invitations/"+created.str("id")+"/decline", "", invitee.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access)
	s.Require().Len(list.List, 1)
	s.Equal("declined", list.List[0]["status"])
	s.NotNil(list.List[0]["responded_at"])
	s.Len(s.authed(http.MethodGet, "/v1/users/me/invitations", "", invitee.Access).List, 0)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).List, 1)
	s.Equal(int64(0), s.outboxCount(constants.TeamMemberAdded))

	again := s.authed(http.MethodPost, "/v1/users/me/invitations/"+created.str("id")+"/accept", "", invitee.Access)
	s.Equal(invitation.ErrorNoLongerValid.HttpStatus, again.Code, again.Raw)
}

func (s *customerSuite) TestInvitations_DeclineByToken_Answers204() {
	owner := s.registerAndVerify(s.uniqueEmail("dect-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("dect-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper).Code)
	token := s.latestAcceptToken()

	res := s.authed(http.MethodPost, "/v1/invitations/decline", fmt.Sprintf(`{"token":%q}`, token), invitee.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access)
	s.Equal("declined", list.List[0]["status"])
}

// ── Resend and revoke ────────────────────────────────────────────────────────

func (s *customerSuite) TestInvitations_Resend_WithinCooldownIs429_ThenIssuesAFreshToken() {
	owner := s.registerAndVerify(s.uniqueEmail("rs-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("rs-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	created := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	firstToken := s.latestAcceptToken()
	path := "/v1/teams/" + teamID + "/invitations/" + created.str("id") + "/resend"

	cooling := s.authed(http.MethodPost, path, "", owner.Access)
	s.Equal(invitation.ErrorResendCooldown.HttpStatus, cooling.Code, cooling.Raw)
	s.Equal(http.StatusTooManyRequests, cooling.Code)
	_, code := cooling.errorCode()
	s.Equal(invitation.ErrorResendCooldown.Code, code)
	s.NotEmpty(cooling.Hdr.Get("Retry-After"))
	s.Equal(int64(1), s.outboxCount(constants.TeamInvitation))

	s.ageInvitation(created.str("id"), invitation.ResendCooldown)
	res := s.authed(http.MethodPost, path, "", owner.Access)
	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(created.str("id"), res.str("id"))
	s.Equal("pending", res.str("status"))
	s.Equal(int64(2), s.outboxCount(constants.TeamInvitation))
	secondToken := s.latestAcceptToken()
	s.NotEqual(firstToken, secondToken)

	old := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, firstToken), invitee.Access)
	s.Equal(invitation.ErrorNotFound.HttpStatus, old.Code, "the replaced token is gone")
	fresh := s.authed(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, secondToken), invitee.Access)
	s.Equal(http.StatusOK, fresh.Code, fresh.Raw)
}

func (s *customerSuite) TestInvitations_Resend_NotPending_IsGone() {
	owner := s.registerAndVerify(s.uniqueEmail("rsgone-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	created := s.invite(owner.Access, teamID, s.uniqueEmail("rsgone-invitee"), authz.RoleDeveloper)
	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/invitations/"+created.str("id"), "", owner.Access).Code)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/invitations/"+created.str("id")+"/resend", "", owner.Access)

	s.Equal(invitation.ErrorNoLongerValid.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestInvitations_Revoke_FromAnotherTeam_IsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("rv-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	created := s.invite(owner.Access, teamID, s.uniqueEmail("rv-invitee"), authz.RoleDeveloper)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	otherOwner := s.registerAndVerify(s.uniqueEmail("rv-other"), "Str0ng!pass")
	otherTeam := s.teamID(otherOwner.Access)

	res := s.authed(http.MethodDelete, "/v1/teams/"+otherTeam+"/invitations/"+created.str("id"), "", otherOwner.Access)

	s.Equal(invitation.ErrorNotFound.HttpStatus, res.Code, res.Raw)
	s.Equal("pending", s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access).List[0]["status"])
}

// ── Lists ────────────────────────────────────────────────────────────────────

func (s *customerSuite) TestInvitations_ListForTeam_NeedsTeamManage_NewestFirst() {
	owner := s.registerAndVerify(s.uniqueEmail("lt-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	devEmail := s.uniqueEmail("lt-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	first := s.invite(owner.Access, teamID, s.uniqueEmail("lt-first"), authz.RoleDeveloper)
	time.Sleep(2 * time.Millisecond)
	second := s.invite(owner.Access, teamID, s.uniqueEmail("lt-second"), authz.RoleFinance)

	forbidden := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", developer.Access)
	s.Equal(team.ErrorForbidden.HttpStatus, forbidden.Code, forbidden.Raw)

	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access)
	s.Equal(http.StatusOK, list.Code, list.Raw)
	s.Require().Len(list.List, 2)
	s.Equal(second.str("id"), list.List[0]["id"])
	s.Equal(first.str("id"), list.List[1]["id"])
	_, hasTokenHash := list.List[0]["token_hash"]
	s.False(hasTokenHash, "the hash never leaves the service")
}

func (s *customerSuite) TestInvitations_ListMine_ShowsPendingOnesForMyEmailOnly() {
	ownerA := s.registerAndVerify(s.uniqueEmail("lm-a"), "Str0ng!pass")
	teamA := s.teamID(ownerA.Access)
	ownerB := s.registerAndVerify(s.uniqueEmail("lm-b"), "Str0ng!pass")
	teamB := s.teamID(ownerB.Access)
	ownerC := s.registerAndVerify(s.uniqueEmail("lm-c"), "Str0ng!pass")
	teamC := s.teamID(ownerC.Access)
	inviteeEmail := s.uniqueEmail("lm-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")

	s.Require().Equal(http.StatusCreated, s.invite(ownerA.Access, teamA, inviteeEmail, authz.RoleDeveloper).Code)
	declined := s.invite(ownerB.Access, teamB, inviteeEmail, authz.RoleFinance)
	s.Require().Equal(http.StatusCreated, declined.Code)
	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodPost, "/v1/users/me/invitations/"+declined.str("id")+"/decline", "", invitee.Access).Code)
	expired := s.invite(ownerC.Access, teamC, inviteeEmail, authz.RoleFinance)
	s.Require().Equal(http.StatusCreated, expired.Code)
	s.ageInvitation(expired.str("id"), invitation.TTL+time.Minute)
	s.Require().Equal(http.StatusCreated, s.invite(ownerA.Access, teamA, s.uniqueEmail("lm-someone-else"), authz.RoleDeveloper).Code)

	res := s.authed(http.MethodGet, "/v1/users/me/invitations", "", invitee.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Require().Len(res.List, 1)
	s.Equal(teamA, res.List[0]["team_id"])
	s.Equal("pending", res.List[0]["status"])
	invitedBy, _ := res.List[0]["invited_by"].(map[string]any)
	s.Equal("Test Person", invitedBy["full_name"])
}

// ── Public lookup ────────────────────────────────────────────────────────────

func (s *customerSuite) TestInvitations_Lookup_IsPublic_ShowsTheAddressOnlyWhilePending() {
	owner := s.registerAndVerify(s.uniqueEmail("lk-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	created := s.invite(owner.Access, teamID, "burkay@example.com", authz.RoleFinance)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	token := s.latestAcceptToken()

	res := s.request(http.MethodGet, "/v1/invitations/lookup?token="+token, "", "")

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal("Test Person", res.str("team_name"))
	s.Equal("finance", res.str("role"))
	s.Equal("Test Person", res.str("invited_by_name"))
	s.Equal("b***@example.com", res.str("email_hint"))
	// The token holder is the invitee: the sign-up form prefills and locks
	// the real address from here, for as long as the offer is open.
	s.Equal("burkay@example.com", res.str("email"))
	s.Equal("pending", res.str("status"))
	s.isRFC3339UTC(res.Body["expires_at"])

	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/invitations/"+created.str("id"), "", owner.Access).Code)
	revoked := s.request(http.MethodGet, "/v1/invitations/lookup?token="+token, "", "")
	s.Equal("revoked", revoked.str("status"))
	s.Equal("b***@example.com", revoked.str("email_hint"))
	_, hasEmail := revoked.Body["email"]
	s.False(hasEmail, "a dead link no longer reveals the address")

	unknown := s.request(http.MethodGet, "/v1/invitations/lookup?token=bm90LWEtcmVhbC10b2tlbg", "", "")
	s.Equal(invitation.ErrorNotFound.HttpStatus, unknown.Code, unknown.Raw)

	missing := s.request(http.MethodGet, "/v1/invitations/lookup", "", "")
	s.Equal(http.StatusBadRequest, missing.Code, missing.Raw)
}

func (s *customerSuite) TestInvitations_Lookup_TwentyFirstFromOneAddress_IsRateLimited() {
	var last *response
	for i := 0; i < 21; i++ {
		last = s.request(http.MethodGet, "/v1/invitations/lookup?token=bm90LWEtcmVhbC10b2tlbg", "", "")
	}

	s.Equal(apperror.ErrorRateLimited.HttpStatus, last.Code, last.Raw)
}

func (s *customerSuite) TestInvitations_AcceptConcurrently_OneJoinsOneIsGone() {
	owner := s.registerAndVerify(s.uniqueEmail("race-acc-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("race-acc-invitee")
	invitee := s.registerAndVerify(inviteeEmail, "Str0ng!pass")
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper).Code)
	token := s.latestAcceptToken()
	s.clearOutbox()

	results := make([]*response, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.request(http.MethodPost, "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, token), invitee.Access)
		}(i)
	}
	wg.Wait()

	codes := []int{results[0].Code, results[1].Code}
	s.ElementsMatch([]int{http.StatusOK, invitation.ErrorNoLongerValid.HttpStatus}, codes, "%s | %s", results[0].Raw, results[1].Raw)

	var members int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT count(*) FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, s.userID(inviteeEmail)).Scan(&members))
	s.Equal(1, members)
	s.Equal(int64(1), s.outboxCount(constants.TeamMemberAdded))
}

// ── Sign-up from an Invitation ───────────────────────────────────────────────

func (s *customerSuite) TestRegister_WithInvitationToken_VerificationJoinsTheTeam() {
	owner := s.registerAndVerify(s.uniqueEmail("su-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("su-invitee")
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper).Code)
	token := s.latestAcceptToken()

	registered := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"New Hire","country":"tr","locale":"en","invitation_token":%q}`, strings.ToUpper(inviteeEmail), token))
	s.Require().Equal(http.StatusCreated, registered.Code, registered.Raw)
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/members", "", owner.Access).List, 1, "nothing joins before the address is proven")
	s.clearOutbox()

	verified := s.post("/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":%q}`, inviteeEmail, devCode))
	s.Require().Equal(http.StatusOK, verified.Code, verified.Raw)
	invitee := tokensOf(verified.Body)

	teams := s.authed(http.MethodGet, "/v1/teams", "", invitee.Access)
	s.Require().Len(teams.List, 2, "their own Team plus the one they were invited to")
	roles := map[string]any{}
	for _, t := range teams.List {
		id, _ := t["id"].(string)
		roles[id] = t["role"]
	}
	s.Equal("developer", roles[teamID])

	claims := s.accessClaims(invitee.Access)
	s.Equal("developer", claims.Teams[teamID])
	s.Len(claims.Teams, 2)

	added := s.outboxPayloadWhere(constants.TeamMemberAdded, map[string]any{"team_id": teamID, "user_id": s.userID(inviteeEmail)})
	s.Equal("developer", added["role"])
	s.Equal(inviteeEmail, added["email"], "the address goes out normalized, as the users table holds it")
	s.Equal("New Hire", added["full_name"])
	s.Equal("en", added["locale"])
	var status string
	var pending *string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT i.status, u.pending_invitation_id::text FROM users u JOIN invitations i ON i.email = u.email WHERE u.email = $1`, inviteeEmail).Scan(&status, &pending))
	s.Equal("accepted", status)
	s.Nil(pending, "the reminder is cleared once the Invitation completed")
}

func (s *customerSuite) TestRegister_WithInvitationToken_ForAnotherEmail_IsRejected() {
	owner := s.registerAndVerify(s.uniqueEmail("sum-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	s.Require().Equal(http.StatusCreated, s.invite(owner.Access, teamID, s.uniqueEmail("sum-invitee"), authz.RoleDeveloper).Code)
	token := s.latestAcceptToken()
	other := s.uniqueEmail("sum-other")

	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Wrong Address","country":"tr","locale":"en","invitation_token":%q}`, other, token))

	s.Equal(invitation.ErrorEmailMismatch.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusBadRequest, res.Code)
	_, code := res.errorCode()
	s.Equal(invitation.ErrorEmailMismatch.Code, code)
	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM users WHERE email = $1`, other).Scan(&count))
	s.Equal(0, count, "no account is created from a mismatched link")
}

func (s *customerSuite) TestRegister_WithRevokedInvitationToken_IsGone() {
	owner := s.registerAndVerify(s.uniqueEmail("sur-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	inviteeEmail := s.uniqueEmail("sur-invitee")
	created := s.invite(owner.Access, teamID, inviteeEmail, authz.RoleDeveloper)
	token := s.latestAcceptToken()
	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/invitations/"+created.str("id"), "", owner.Access).Code)

	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Late","country":"tr","locale":"en","invitation_token":%q}`, inviteeEmail, token))

	s.Equal(invitation.ErrorNoLongerValid.HttpStatus, res.Code, res.Raw)
}

// A sign-up whose Invitation could not be completed at verification (a
// transient failure) completes at the next sign-in instead.
func (s *customerSuite) TestLogin_WithAPendingInvitationLeftOver_CompletesIt() {
	owner := s.registerAndVerify(s.uniqueEmail("retry-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	memberEmail := s.uniqueEmail("retry-member")
	s.registerAndVerify(memberEmail, "Str0ng!pass")
	created := s.invite(owner.Access, teamID, memberEmail, authz.RoleFinance)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET pending_invitation_id = $2 WHERE email = $1`, memberEmail, created.str("id"))
	s.Require().NoError(err)
	s.expireKeyPattern("mfa:email_code:cooldown:*")

	member := s.login(memberEmail, "Str0ng!pass")

	s.Equal("finance", s.accessClaims(member.Access).Teams[teamID], "the first token after sign-in already carries the Team")
	var pending *string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT pending_invitation_id::text FROM users WHERE email = $1`, memberEmail).Scan(&pending))
	s.Nil(pending)
	s.Equal("accepted", s.authed(http.MethodGet, "/v1/teams/"+teamID+"/invitations", "", owner.Access).List[0]["status"])
}

func (s *customerSuite) TestLogin_WithARevokedInvitationLeftOver_ClearsItWithoutJoining() {
	owner := s.registerAndVerify(s.uniqueEmail("retry-rv-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	memberEmail := s.uniqueEmail("retry-rv-member")
	s.registerAndVerify(memberEmail, "Str0ng!pass")
	created := s.invite(owner.Access, teamID, memberEmail, authz.RoleFinance)
	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/invitations/"+created.str("id"), "", owner.Access).Code)
	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET pending_invitation_id = $2 WHERE email = $1`, memberEmail, created.str("id"))
	s.Require().NoError(err)
	s.expireKeyPattern("mfa:email_code:cooldown:*")

	member := s.login(memberEmail, "Str0ng!pass")

	s.NotContains(s.accessClaims(member.Access).Teams, teamID)
	var pending *string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT pending_invitation_id::text FROM users WHERE email = $1`, memberEmail).Scan(&pending))
	s.Nil(pending, "a dead offer is forgotten, not retried forever")
}
