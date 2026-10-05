package integrationtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/piggystats/nodera/cmd/customer/app"
	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
)

const fullKeyShape = `^nd_[0-9A-Za-z]{12}\.[0-9A-Za-z]{43}$`

func apiKeysPath(teamID, projectID string) string {
	return "/v1/teams/" + teamID + "/projects/" + projectID + "/api-keys"
}

func apiKeyBody(name string, permissions ...string) string {
	quoted := make([]string, 0, len(permissions))
	for _, p := range permissions {
		quoted = append(quoted, fmt.Sprintf("%q", p))
	}

	return fmt.Sprintf(`{"name":%q,"permissions":[%s]}`, name, strings.Join(quoted, ","))
}

// secretOf answers the part of a whole key after the dot, which is all the
// hash covers.
func secretOf(fullKey string) string {
	_, secret, _ := strings.Cut(fullKey, ".")

	return secret
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func (s *customerSuite) createProject(token, teamID, name string) string {
	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/projects", fmt.Sprintf(`{"name":%q,"description":""}`, name), token)
	s.Require().Equal(http.StatusCreated, res.Code, res.Raw)

	return res.str("id")
}

func (s *customerSuite) createAPIKey(token, teamID, projectID, name string) *response {
	res := s.authed(http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody(name, "mail.send"), token)
	s.Require().Equal(http.StatusCreated, res.Code, res.Raw)

	return res
}

// apiKeyEntry answers the raw record the integration gateway and the mail
// service read, and whether it exists at all.
func (s *customerSuite) apiKeyEntry(id string) (string, bool) {
	raw, err := rawRedis.Get(context.Background(), "api_key:"+id).Result()
	if err == goredis.Nil {
		return "", false
	}
	s.Require().NoError(err)

	return raw, true
}

func (s *customerSuite) expectedEntry(fullKey, projectID, teamID string) string {
	return fmt.Sprintf(`{"hash":%q,"project_id":%q,"team_id":%q,"permissions":["mail.send"]}`, sha256Hex(secretOf(fullKey)), projectID, teamID)
}

func (s *customerSuite) TestAPIKeys_Create_AnswersTheWholeKeyOnce() {
	email := s.uniqueEmail("apikey-create")
	owner := s.registerAndVerify(email, "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")

	res := s.authed(http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody("  production  ", "mail.send"), owner.Access)

	s.Require().Equal(http.StatusCreated, res.Code, res.Raw)
	key := res.str("key")
	s.Regexp(fullKeyShape, key)
	s.Equal("nd_"+res.str("id"), res.str("prefix"))
	s.True(strings.HasPrefix(key, res.str("prefix")+"."), "the key starts with the prefix a list shows")
	s.Equal("production", res.str("name"))
	s.Equal([]any{"mail.send"}, res.Body["permissions"])
	s.Equal(s.userID(email), res.str("created_by"))
	s.isRFC3339UTC(res.Body["created_at"])
	s.Contains(res.Body, "last_used_at")
	s.Nil(res.Body["last_used_at"])

	var storedHash string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT secret_hash FROM api_keys WHERE id = $1`, res.str("id")).Scan(&storedHash))
	s.Equal(sha256Hex(secretOf(key)), storedHash, "only the hash of the secret is kept")
}

func (s *customerSuite) TestAPIKeys_Create_WritesTheContractEntryWithoutExpiry() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-entry"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")

	created := s.createAPIKey(owner.Access, teamID, projectID, "production")

	raw, found := s.apiKeyEntry(created.str("id"))
	s.Require().True(found)
	s.JSONEq(s.expectedEntry(created.str("key"), projectID, teamID), raw)
	ttl, err := rawRedis.TTL(context.Background(), "api_key:"+created.str("id")).Result()
	s.Require().NoError(err)
	s.Equal(time.Duration(-1), ttl, "a key lives until it is revoked")
}

func (s *customerSuite) TestAPIKeys_List_NewestFirstWithoutSecretsAndWithTheLastUse() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-list"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	older := s.createAPIKey(owner.Access, teamID, projectID, "older")
	newer := s.createAPIKey(owner.Access, teamID, projectID, "newer")

	list := s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access)

	s.Require().Equal(http.StatusOK, list.Code, list.Raw)
	s.Require().Len(list.List, 2)
	s.Equal(newer.str("id"), list.List[0]["id"])
	s.Equal(older.str("id"), list.List[1]["id"])
	for _, item := range list.List {
		s.NotContains(item, "key")
		s.Nil(item["last_used_at"])
	}
	s.NotContains(list.Raw, secretOf(older.str("key")))
	s.NotContains(list.Raw, secretOf(newer.str("key")))
	s.Equal("nd_"+older.str("id"), list.List[1]["prefix"])
	s.Equal([]any{"mail.send"}, list.List[1]["permissions"])

	// The integration gateway and the mail listener record a use; this
	// service only reads it.
	s.Require().NoError(rawRedis.Set(context.Background(), "api_key_last_used:"+older.str("id"), "2026-10-05T09:30:00.123456789Z", 0).Err())

	used := s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access)
	s.Require().Len(used.List, 2)
	s.Nil(used.List[0]["last_used_at"])
	s.Equal("2026-10-05T09:30:00.123456789Z", used.List[1]["last_used_at"])
}

func (s *customerSuite) TestAPIKeys_Revoke_RemovesTheEntryAndTheKeyFromTheList() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-revoke"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	created := s.createAPIKey(owner.Access, teamID, projectID, "production")
	id := created.str("id")

	res := s.authed(http.MethodDelete, apiKeysPath(teamID, projectID)+"/"+id, "", owner.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Empty(res.Raw)
	_, found := s.apiKeyEntry(id)
	s.False(found, "the readers refuse the key on their next lookup")
	s.Len(s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access).List, 0)
	var revoked bool
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT revoked_at IS NOT NULL FROM api_keys WHERE id = $1`, id).Scan(&revoked))
	s.True(revoked, "the row stays, revoked")

	again := s.authed(http.MethodDelete, apiKeysPath(teamID, projectID)+"/"+id, "", owner.Access)
	s.Equal(apikey.ErrorNotFound.HttpStatus, again.Code, again.Raw)
	domain, code := again.errorCode()
	s.Equal(int(apikey.Domain), domain)
	s.Equal(apikey.ErrorNotFound.Code, code)

	unknown := s.authed(http.MethodDelete, apiKeysPath(teamID, projectID)+"/Unknown01234", "", owner.Access)
	s.Equal(apikey.ErrorNotFound.HttpStatus, unknown.Code, unknown.Raw)
	_, code = unknown.errorCode()
	s.Equal(apikey.ErrorNotFound.Code, code)
}

func (s *customerSuite) TestAPIKeys_Roles_FinanceIsForbidden_DeveloperCreates_StrangerIsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-roles-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	ownerKey := s.createAPIKey(owner.Access, teamID, projectID, "owner's")
	devEmail := s.uniqueEmail("apikey-roles-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	finEmail := s.uniqueEmail("apikey-roles-fin")
	finance := s.registerAndVerify(finEmail, "Str0ng!pass")
	s.addMember(teamID, finEmail, authz.RoleFinance)
	stranger := s.registerAndVerify(s.uniqueEmail("apikey-roles-stranger"), "Str0ng!pass")

	byDeveloper := s.authed(http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody("ci", "mail.send"), developer.Access)
	s.Equal(http.StatusCreated, byDeveloper.Code, byDeveloper.Raw)
	s.Equal(s.userID(devEmail), byDeveloper.str("created_by"))
	s.Len(s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", developer.Access).List, 2)

	for _, attempt := range []struct{ method, path, body string }{
		{http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody("finance", "mail.send")},
		{http.MethodGet, apiKeysPath(teamID, projectID), ""},
		{http.MethodDelete, apiKeysPath(teamID, projectID) + "/" + ownerKey.str("id"), ""},
	} {
		forbidden := s.authed(attempt.method, attempt.path, attempt.body, finance.Access)
		s.Equal(team.ErrorForbidden.HttpStatus, forbidden.Code, "%s %s: %s", attempt.method, attempt.path, forbidden.Raw)
		s.Equal(http.StatusForbidden, forbidden.Code)
		domain, code := forbidden.errorCode()
		s.Equal(int(team.Domain), domain)
		s.Equal(team.ErrorForbidden.Code, code)

		notFound := s.authed(attempt.method, attempt.path, attempt.body, stranger.Access)
		s.Equal(team.ErrorNotMember.HttpStatus, notFound.Code, "%s %s: %s", attempt.method, attempt.path, notFound.Raw)
		s.Equal(http.StatusNotFound, notFound.Code)
		_, code = notFound.errorCode()
		s.Equal(team.ErrorNotMember.Code, code)
	}

	_, found := s.apiKeyEntry(ownerKey.str("id"))
	s.True(found, "neither the finance member nor the stranger revoked anything")
}

func (s *customerSuite) TestAPIKeys_AProjectOfAnotherTeam_IsNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-otherteam"), "Str0ng!pass")
	firstTeam := s.teamID(owner.Access)
	secondTeam := s.authed(http.MethodPost, "/v1/teams", `{"name":"Second"}`, owner.Access).str("id")
	elsewhere := s.createProject(owner.Access, secondTeam, "Elsewhere")
	key := s.createAPIKey(owner.Access, secondTeam, elsewhere, "production")

	for _, attempt := range []struct{ method, path, body string }{
		{http.MethodPost, apiKeysPath(firstTeam, elsewhere), apiKeyBody("sneaky", "mail.send")},
		{http.MethodGet, apiKeysPath(firstTeam, elsewhere), ""},
		{http.MethodDelete, apiKeysPath(firstTeam, elsewhere) + "/" + key.str("id"), ""},
	} {
		res := s.authed(attempt.method, attempt.path, attempt.body, owner.Access)

		s.Equal(project.ErrorNotFound.HttpStatus, res.Code, "%s %s: %s", attempt.method, attempt.path, res.Raw)
		s.Equal(http.StatusNotFound, res.Code)
		domain, code := res.errorCode()
		s.Equal(int(project.Domain), domain)
		s.Equal(project.ErrorNotFound.Code, code)
	}

	s.Len(s.authed(http.MethodGet, apiKeysPath(secondTeam, elsewhere), "", owner.Access).List, 1, "the key is untouched in its own Team")
}

func (s *customerSuite) TestAPIKeys_APermissionNoKeyMayCarry_IsABadRequest() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-badperm"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")

	res := s.authed(http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody("too wide", "mail.send", "mail.write"), owner.Access)

	s.Equal(apikey.ErrorPermissionNotAllowed.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusBadRequest, res.Code)
	domain, code := res.errorCode()
	s.Equal(int(apikey.Domain), domain)
	s.Equal(apikey.ErrorPermissionNotAllowed.Code, code)
	s.Len(s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access).List, 0)
}

func (s *customerSuite) TestAPIKeys_BlankNameOrNoPermission_IsRejected() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-blank"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")

	for _, body := range []string{
		apiKeyBody("   ", "mail.send"),
		apiKeyBody(strings.Repeat("k", 65), "mail.send"),
		apiKeyBody("no permission"),
		`{"name":"missing permissions"}`,
		apiKeyBody("twice", "mail.send", "mail.send"),
	} {
		res := s.authed(http.MethodPost, apiKeysPath(teamID, projectID), body, owner.Access)

		s.Equal(http.StatusBadRequest, res.Code, "%s: %s", body, res.Raw)
	}
	s.Len(s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access).List, 0)
}

func (s *customerSuite) TestAPIKeys_TheTwentyFirstActiveKey_IsAConflict() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-limit"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	first := s.createAPIKey(owner.Access, teamID, projectID, "key 1")
	for i := 2; i <= apikey.MaxActivePerProject; i++ {
		s.createAPIKey(owner.Access, teamID, projectID, fmt.Sprintf("key %d", i))
	}

	res := s.authed(http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody("key 21", "mail.send"), owner.Access)

	s.Equal(apikey.ErrorLimitReached.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	domain, code := res.errorCode()
	s.Equal(int(apikey.Domain), domain)
	s.Equal(apikey.ErrorLimitReached.Code, code)

	// Only active keys count: a revoked one frees its place.
	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodDelete, apiKeysPath(teamID, projectID)+"/"+first.str("id"), "", owner.Access).Code)
	s.createAPIKey(owner.Access, teamID, projectID, "key 21")
	s.Len(s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access).List, apikey.MaxActivePerProject)
}

// Creations racing for the last free place must not all see it free: the
// Project row is locked before the count.
func (s *customerSuite) TestAPIKeys_CreationsRacingForTheLastPlace_OnlyOneWins() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-race"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	for i := 1; i < apikey.MaxActivePerProject; i++ {
		s.createAPIKey(owner.Access, teamID, projectID, fmt.Sprintf("key %d", i))
	}

	const racers = 5
	results := make([]*response, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.request(http.MethodPost, apiKeysPath(teamID, projectID), apiKeyBody(fmt.Sprintf("racer %d", i), "mail.send"), owner.Access)
		}(i)
	}
	wg.Wait()

	codes := make([]int, 0, racers)
	for _, res := range results {
		codes = append(codes, res.Code)
	}
	s.ElementsMatch([]int{http.StatusCreated, http.StatusConflict, http.StatusConflict, http.StatusConflict, http.StatusConflict}, codes)
	var active int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT count(*) FROM api_keys WHERE project_id = $1 AND revoked_at IS NULL`, projectID).Scan(&active))
	s.Equal(apikey.MaxActivePerProject, active)
}

func (s *customerSuite) TestAPIKeys_ARemovedMembersKeyKeepsWorking() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-leaver-owner"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	devEmail := s.uniqueEmail("apikey-leaver-dev")
	developer := s.registerAndVerify(devEmail, "Str0ng!pass")
	s.addMember(teamID, devEmail, authz.RoleDeveloper)
	created := s.createAPIKey(developer.Access, teamID, projectID, "production")
	before, found := s.apiKeyEntry(created.str("id"))
	s.Require().True(found)

	removed := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/members/"+s.userID(devEmail), "", owner.Access)
	s.Require().Equal(http.StatusNoContent, removed.Code, removed.Raw)

	after, found := s.apiKeyEntry(created.str("id"))
	s.True(found, "production sending never stops because a member left")
	s.JSONEq(before, after)
	s.JSONEq(s.expectedEntry(created.str("key"), projectID, teamID), after)
	list := s.authed(http.MethodGet, apiKeysPath(teamID, projectID), "", owner.Access)
	s.Require().Len(list.List, 1)
	s.Equal(created.str("id"), list.List[0]["id"])
	s.Equal(s.userID(devEmail), list.List[0]["created_by"])
}

func (s *customerSuite) TestAPIKeys_WithoutSession_IsUnauthorized() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-nosession"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")

	res := s.request(http.MethodGet, apiKeysPath(teamID, projectID), "", "")

	s.Equal(http.StatusUnauthorized, res.Code, res.Raw)
}

// ── sync-api-keys ────────────────────────────────────────────────────────────

func (s *customerSuite) TestSyncAPIKeys_RestoresALostEntryAndDeletesARevokedOne() {
	owner := s.registerAndVerify(s.uniqueEmail("apikey-sync"), "Str0ng!pass")
	teamID := s.teamID(owner.Access)
	projectID := s.createProject(owner.Access, teamID, "Storefront")
	kept := s.createAPIKey(owner.Access, teamID, projectID, "kept")
	gone := s.createAPIKey(owner.Access, teamID, projectID, "gone")
	s.Require().Equal(http.StatusNoContent, s.authed(http.MethodDelete, apiKeysPath(teamID, projectID)+"/"+gone.str("id"), "", owner.Access).Code)
	// Redis drifted from the database: the active key's entry was lost and the
	// revoked one's came back.
	ctx := context.Background()
	s.Require().NoError(rawRedis.Del(ctx, "api_key:"+kept.str("id")).Err())
	s.Require().NoError(rawRedis.Set(ctx, "api_key:"+gone.str("id"), s.expectedEntry(gone.str("key"), projectID, teamID), 0).Err())

	summary, err := app.SyncAPIKeys(s.T().Context(), logger.Noop(), s.pgPool, s.redis)

	s.Require().NoError(err)
	s.Equal(1, summary.Written)
	s.Equal(1, summary.Deleted)
	restored, found := s.apiKeyEntry(kept.str("id"))
	s.Require().True(found)
	s.JSONEq(s.expectedEntry(kept.str("key"), projectID, teamID), restored)
	_, found = s.apiKeyEntry(gone.str("id"))
	s.False(found, "a revoked key never authenticates again")
}
