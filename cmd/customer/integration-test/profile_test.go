package integrationtest

import (
	"fmt"
	"net/http"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
)

func (s *customerSuite) TestGetMe_AnswersProfileWithTeam() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodGet, "/v1/users/me", "", tokens.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal("Nodera Dev", res.str("full_name"))
	s.Equal("en", res.str("locale"))
	s.Equal("TR", res.str("country"))
	s.Equal("active", res.str("status"))
	s.Equal("email", res.str("mfa_method"))
	s.NotNil(res.Body["email_verified_at"])
	team, _ := res.Body["team"].(map[string]any)
	s.Equal("Nodera Dev", team["name"])
}

func (s *customerSuite) TestUpdateMe_ChangesNameLocaleAndCountry() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPut, "/v1/users/me", `{"full_name":"Dev Renamed","locale":"tr","country":"de"}`, tokens.Access)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal("Dev Renamed", res.str("full_name"))
	s.Equal("tr", res.str("locale"))
	s.Equal("DE", res.str("country"))
	s.Equal(int64(1), s.outboxCount(constants.UserProfileUpdated))
	updated := s.outboxPayload(constants.UserProfileUpdated)
	s.Equal(s.userID(seedEmail), updated["user_id"])
	s.Equal("Dev Renamed", updated["full_name"])
	s.Equal("tr", updated["locale"])
	s.isRFC3339UTC(updated["updated_at"])

	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET full_name = 'Nodera Dev', locale = 'en', country = 'TR' WHERE email = $1`, seedEmail)
	s.Require().NoError(err)
}

func (s *customerSuite) TestUpdateMe_UnknownLocale_IsRejected() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPut, "/v1/users/me", `{"full_name":"Dev","locale":"fr","country":"de"}`, tokens.Access)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

func (s *customerSuite) TestChangePassword_KeepsCurrentSessionEndsOthers() {
	laptop := s.login(seedEmail, seedPassword)
	s.expireKeyPattern("mfa:email_code:cooldown:*")
	phone := s.login(seedEmail, seedPassword)

	const newPassword = "Ch4nged!pass"
	res := s.authed(http.MethodPost, "/v1/users/me/password", fmt.Sprintf(
		`{"current_password":%q,"new_password":%q,"new_password_confirm":%q}`, seedPassword, newPassword, newPassword), laptop.Access)

	s.Equal(http.StatusNoContent, res.Code, res.Raw)
	s.Equal(http.StatusOK, s.authed(http.MethodGet, "/v1/users/me", "", laptop.Access).Code, "the calling session survives")
	s.Equal(http.StatusUnauthorized, s.authed(http.MethodGet, "/v1/users/me", "", phone.Access).Code, "the other session is gone")

	s.restoreSeedPassword()
}

func (s *customerSuite) TestChangePassword_WrongCurrent_IsForbidden() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPost, "/v1/users/me/password",
		`{"current_password":"nope-Pass1!","new_password":"Ch4nged!pass","new_password_confirm":"Ch4nged!pass"}`, tokens.Access)

	s.Equal(user.ErrorIncorrectPassword.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(user.ErrorIncorrectPassword.Code, code)
}

func (s *customerSuite) TestUpdateMFA_TurnsOffWithPasswordThenLoginSkipsTheCode() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPut, "/v1/users/me/mfa", fmt.Sprintf(`{"method":"none","password":%q}`, seedPassword), tokens.Access)
	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal("none", res.str("mfa_method"))

	login := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))
	s.Equal(http.StatusOK, login.Code, login.Raw)
	s.Equal(false, login.Body["mfa_required"])
}

func (s *customerSuite) TestUpdateMFA_SameMethod_IsAConflict() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPut, "/v1/users/me/mfa", fmt.Sprintf(`{"method":"email","password":%q}`, seedPassword), tokens.Access)

	s.Equal(user.ErrorMFAMethodAlreadySet.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestUpdateMFA_WrongPassword_IsForbidden() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPut, "/v1/users/me/mfa", `{"method":"none","password":"nope-Pass1!"}`, tokens.Access)

	s.Equal(user.ErrorIncorrectPassword.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestSessions_ListFlagsCurrentAndDeleteEndsTheOther() {
	laptop := s.login(seedEmail, seedPassword)
	s.expireKeyPattern("mfa:email_code:cooldown:*")
	phone := s.login(seedEmail, seedPassword)

	list := s.authed(http.MethodGet, "/v1/users/me/sessions", "", laptop.Access)
	s.Require().Equal(http.StatusOK, list.Code, list.Raw)
	s.Require().Len(list.List, 2)

	var current, other string
	for _, item := range list.List {
		id, _ := item["id"].(string)
		if item["current"] == true {
			current = id
		} else {
			other = id
		}
		s.Equal("integration-suite", item["user_agent"])
	}
	s.NotEmpty(current)
	s.NotEmpty(other)

	del := s.authed(http.MethodDelete, "/v1/users/me/sessions/"+other, "", laptop.Access)
	s.Equal(http.StatusNoContent, del.Code, del.Raw)
	s.Equal(http.StatusUnauthorized, s.authed(http.MethodGet, "/v1/users/me", "", phone.Access).Code)
	s.Equal(http.StatusOK, s.authed(http.MethodGet, "/v1/users/me", "", laptop.Access).Code)
}

func (s *customerSuite) TestSessions_DeleteSomeoneElsesSession_IsNotFound() {
	victim := s.registerAndVerify(s.uniqueEmail("victim"), "Str0ng!pass")
	victimSessions := s.authed(http.MethodGet, "/v1/users/me/sessions", "", victim.Access)
	victimSessionID, _ := victimSessions.List[0]["id"].(string)

	attacker := s.login(seedEmail, seedPassword)
	res := s.authed(http.MethodDelete, "/v1/users/me/sessions/"+victimSessionID, "", attacker.Access)

	s.Equal(session.ErrorNotFound.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusOK, s.authed(http.MethodGet, "/v1/users/me", "", victim.Access).Code, "the victim's session is untouched")
}
