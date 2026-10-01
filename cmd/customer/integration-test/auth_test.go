package integrationtest

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/otp"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/pkg/apperror"
)

// ── Sign-up ──────────────────────────────────────────────────────────────────

func (s *customerSuite) TestRegister_CreatesPendingUserTeamAndQueuesVerificationCode() {
	email := s.uniqueEmail("register")

	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada Lovelace","country":"tr","locale":"tr"}`, email))

	s.Equal(http.StatusCreated, res.Code, res.Raw)

	var status, country, mfaMethod string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT status, country, mfa_method FROM users WHERE email = $1`, email).Scan(&status, &country, &mfaMethod))
	s.Equal("pending_verification", status)
	s.Equal("TR", country, "country is stored upper-cased")
	s.Equal("email", mfaMethod, "MFA is on by default")

	var teams int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(),
		`SELECT count(*) FROM team_members m JOIN users u ON u.id = m.user_id WHERE u.email = $1 AND m.role = 'owner'`, email).Scan(&teams))
	s.Equal(1, teams, "sign-up creates the User's Team with them as owner")

	s.Equal(int64(1), s.outboxCount(constants.EmailVerificationCode))
	payload := s.outboxPayload(constants.EmailVerificationCode)
	s.Equal(email, payload["email"])
	s.Equal("tr", payload["locale"])
	s.Equal(devCode, payload["code"])
}

func (s *customerSuite) TestRegister_DuplicateEmail_AnswersCreatedAndMailsTheOwner() {
	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Someone Else","country":"de","locale":"en"}`, seedEmail))

	s.Equal(http.StatusCreated, res.Code, res.Raw)
	s.Equal(int64(1), s.outboxCount(constants.EmailAlreadyRegistered))
	s.Equal(int64(0), s.outboxCount(constants.EmailVerificationCode))

	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM users WHERE email = $1`, seedEmail).Scan(&count))
	s.Equal(1, count)
}

func (s *customerSuite) TestRegister_WeakPassword_IsRejected() {
	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"short","full_name":"Ada","country":"tr","locale":"en"}`, s.uniqueEmail("weak")))

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
	s.Contains(res.Raw, "password")
}

func (s *customerSuite) TestRegister_UnknownCountry_IsRejected() {
	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"zz","locale":"en"}`, s.uniqueEmail("country")))

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
	s.Contains(res.Raw, "country")
}

// ── Verification ─────────────────────────────────────────────────────────────

func (s *customerSuite) TestVerifyEmail_CorrectCode_ActivatesAndSignsIn() {
	email := s.uniqueEmail("verify")
	tokens := s.registerAndVerify(email, "Str0ng!pass")

	s.NotEmpty(tokens.Access)
	s.NotEmpty(tokens.Refresh)

	var status string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT status FROM users WHERE email = $1`, email).Scan(&status))
	s.Equal("active", status)

	me := s.authed(http.MethodGet, "/v1/users/me", "", tokens.Access)
	s.Equal(http.StatusOK, me.Code, me.Raw)
	s.Equal(email, me.str("email"))
}

func (s *customerSuite) TestVerifyEmail_WrongCode_BurnsAnAttempt() {
	email := s.uniqueEmail("wrongcode")
	s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"tr","locale":"en"}`, email))

	res := s.post("/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":"000000"}`, email))

	s.Equal(otp.ErrorInvalidCode.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(otp.Domain), domain)
	s.Equal(otp.ErrorInvalidCode.Code, code)
}

func (s *customerSuite) TestVerifyEmail_FiveWrongCodes_ExhaustTheCode() {
	email := s.uniqueEmail("exhaust")
	s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"tr","locale":"en"}`, email))

	var last *response
	for i := 0; i < 5; i++ {
		last = s.post("/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":"000000"}`, email))
	}
	_, code := last.errorCode()
	s.Equal(otp.ErrorAttemptsExhausted.Code, code, last.Raw)

	// The right code no longer works either: the budget is spent.
	res := s.post("/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":%q}`, email, devCode))
	s.Equal(otp.ErrorInvalidCode.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestVerifyEmail_UnknownEmail_LooksLikeAWrongCode() {
	res := s.post("/v1/auth/verify-email", `{"email":"nobody@example.com","code":"123456"}`)

	s.Equal(otp.ErrorInvalidCode.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(otp.ErrorInvalidCode.Code, code)
}

func (s *customerSuite) TestResendVerification_WithinCooldown_AnswersOKWithoutASecondMail() {
	email := s.uniqueEmail("resend")
	s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"tr","locale":"en"}`, email))

	res := s.post("/v1/auth/verify-email/resend", fmt.Sprintf(`{"email":%q}`, email))

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(int64(1), s.outboxCount(constants.EmailVerificationCode), "cooldown holds the second mail back")
}

func (s *customerSuite) TestResendVerification_AfterCooldown_QueuesAFreshCode() {
	email := s.uniqueEmail("resend2")
	s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"tr","locale":"en"}`, email))
	s.expireKeyPattern("verify:email:cooldown:*")

	res := s.post("/v1/auth/verify-email/resend", fmt.Sprintf(`{"email":%q}`, email))

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(int64(2), s.outboxCount(constants.EmailVerificationCode))
}

func (s *customerSuite) TestResendVerification_UnknownEmail_AnswersOK() {
	res := s.post("/v1/auth/verify-email/resend", `{"email":"nobody@example.com"}`)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(int64(0), s.outboxCount(constants.EmailVerificationCode))
}

// ── Sign-in ──────────────────────────────────────────────────────────────────

func (s *customerSuite) TestLogin_WithMFA_IssuesChallengeAndQueuesCode() {
	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(true, res.Body["mfa_required"])
	s.NotEmpty(res.str("challenge_token"))
	s.Equal("email", res.str("method"))
	s.Equal(true, res.Body["supports_resend"])
	s.Equal(float64(60), res.Body["resend_after_seconds"])
	s.Nil(res.Body["session"])
	s.Equal(int64(1), s.outboxCount(constants.MFAEmailCode))
}

func (s *customerSuite) TestLogin_ThenVerifyMFA_OpensASession() {
	tokens := s.login(seedEmail, seedPassword)

	me := s.authed(http.MethodGet, "/v1/users/me", "", tokens.Access)
	s.Equal(http.StatusOK, me.Code, me.Raw)
	s.Equal(seedEmail, me.str("email"))
	team, _ := me.Body["team"].(map[string]any)
	s.Equal("owner", team["role"])
}

func (s *customerSuite) TestLogin_WrongPassword_AnswersNotFound() {
	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"wrong-Pass1!"}`, seedEmail))

	s.Equal(user.ErrorNotFound.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(user.Domain), domain)
	s.Equal(user.ErrorNotFound.Code, code)
}

func (s *customerSuite) TestLogin_UnknownEmail_AnswersTheSameNotFound() {
	res := s.post("/v1/auth/login", `{"email":"nobody@example.com","password":"wrong-Pass1!"}`)

	s.Equal(user.ErrorNotFound.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(user.ErrorNotFound.Code, code)
}

func (s *customerSuite) TestLogin_UnverifiedUser_IsToldToVerify() {
	email := s.uniqueEmail("unverified")
	s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"tr","locale":"en"}`, email))

	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"Str0ng!pass"}`, email))

	s.Equal(user.ErrorEmailNotVerified.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(user.ErrorEmailNotVerified.Code, code)
}

func (s *customerSuite) TestLogin_SuspendedUser_IsRefused() {
	email := s.uniqueEmail("suspended")
	s.registerAndVerify(email, "Str0ng!pass")
	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET status = 'suspended' WHERE email = $1`, email)
	s.Require().NoError(err)

	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"Str0ng!pass"}`, email))

	s.Equal(user.ErrorSuspended.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(user.ErrorSuspended.Code, code)
}

func (s *customerSuite) TestLogin_FifthWrongPassword_LocksWithRetryAfter() {
	var last *response
	for i := 0; i < 5; i++ {
		last = s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"wrong-Pass1!"}`, seedEmail))
	}

	s.Equal(user.ErrorAccountLocked.HttpStatus, last.Code, last.Raw)
	_, code := last.errorCode()
	s.Equal(user.ErrorAccountLocked.Code, code)
	s.NotEmpty(last.Hdr.Get("Retry-After"))

	// The right password is refused while the lock holds.
	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))
	s.Equal(user.ErrorAccountLocked.HttpStatus, res.Code, res.Raw)
	retryAfter, err := strconv.Atoi(res.Hdr.Get("Retry-After"))
	s.Require().NoError(err)
	s.InDelta(60, retryAfter, 2, "the first lock tier is one minute")
}

func (s *customerSuite) TestLogin_WithMFAOff_AnswersASessionDirectly() {
	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET mfa_method = 'none' WHERE email = $1`, seedEmail)
	s.Require().NoError(err)

	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(false, res.Body["mfa_required"])
	sess, _ := res.Body["session"].(map[string]any)
	s.NotEmpty(sess["access_token"])
	s.Equal(int64(0), s.outboxCount(constants.MFAEmailCode))
}

// ── MFA step ─────────────────────────────────────────────────────────────────

func (s *customerSuite) TestVerifyMFA_WrongCode_ReportsAttemptsLeftThenExhausts() {
	login := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))
	challenge := login.str("challenge_token")

	var last *response
	for i := 0; i < 5; i++ {
		last = s.post("/v1/auth/mfa/verify", fmt.Sprintf(`{"challenge_token":%q,"code":"000000"}`, challenge))
	}
	_, code := last.errorCode()
	s.Equal(otp.ErrorAttemptsExhausted.Code, code, last.Raw)

	// The challenge itself is gone with the exhausted code.
	res := s.post("/v1/auth/mfa/verify", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, challenge, devCode))
	s.Equal(mfa.ErrorInvalidChallenge.HttpStatus, res.Code, res.Raw)
	_, code = res.errorCode()
	s.Equal(mfa.ErrorInvalidChallenge.Code, code)
}

func (s *customerSuite) TestVerifyMFA_UnknownChallenge_IsRefused() {
	res := s.post("/v1/auth/mfa/verify", `{"challenge_token":"deadbeef","code":"123456"}`)

	s.Equal(mfa.ErrorInvalidChallenge.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestVerifyMFA_ChallengeIsSingleUse() {
	login := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))
	challenge := login.str("challenge_token")

	first := s.post("/v1/auth/mfa/verify", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, challenge, devCode))
	s.Equal(http.StatusOK, first.Code, first.Raw)

	second := s.post("/v1/auth/mfa/verify", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, challenge, devCode))
	s.Equal(mfa.ErrorInvalidChallenge.HttpStatus, second.Code, second.Raw)
}

func (s *customerSuite) TestResendMFA_WithinCooldown_IsHeldBack() {
	login := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))

	res := s.post("/v1/auth/mfa/resend", fmt.Sprintf(`{"challenge_token":%q}`, login.str("challenge_token")))

	s.Equal(otp.ErrorCooldown.HttpStatus, res.Code, res.Raw)
	s.Equal("60", res.Hdr.Get("Retry-After"))
	s.Equal(int64(1), s.outboxCount(constants.MFAEmailCode))
}

func (s *customerSuite) TestResendMFA_AfterCooldown_QueuesASecondCode() {
	login := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, seedPassword))
	s.expireKeyPattern("mfa:email_code:cooldown:*")

	res := s.post("/v1/auth/mfa/resend", fmt.Sprintf(`{"challenge_token":%q}`, login.str("challenge_token")))

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(int64(2), s.outboxCount(constants.MFAEmailCode))
}

// ── Sessions ─────────────────────────────────────────────────────────────────

func (s *customerSuite) TestRefresh_RotatesAndRevokesTheReusedToken() {
	tokens := s.login(seedEmail, seedPassword)

	first := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Refresh))
	s.Require().Equal(http.StatusOK, first.Code, first.Raw)
	rotated := tokensOf(first.Body)
	s.NotEqual(tokens.Refresh, rotated.Refresh)

	me := s.authed(http.MethodGet, "/v1/users/me", "", rotated.Access)
	s.Equal(http.StatusOK, me.Code, "the rotated pair works")

	s.ageRotations()
	reuse := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Refresh))
	s.Equal(session.ErrorRefreshTokenReused.HttpStatus, reuse.Code, reuse.Raw)
	_, code := reuse.errorCode()
	s.Equal(session.ErrorRefreshTokenReused.Code, code)

	// The whole session died with the reuse: the rotated pair is dead too.
	after := s.authed(http.MethodGet, "/v1/users/me", "", rotated.Access)
	s.Equal(http.StatusUnauthorized, after.Code, after.Raw)
	again := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, rotated.Refresh))
	s.Equal(session.ErrorInvalidRefreshToken.HttpStatus, again.Code, again.Raw)
}

func (s *customerSuite) TestRefresh_GarbageToken_IsRefused() {
	res := s.post("/v1/auth/refresh", `{"refresh_token":"not-a-token"}`)

	s.Equal(session.ErrorInvalidRefreshToken.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestRefresh_AccessTokenInPlaceOfRefresh_IsRefused() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Access))

	s.Equal(session.ErrorInvalidRefreshToken.HttpStatus, res.Code, res.Raw)
}

func (s *customerSuite) TestLogout_EndsOnlyThisSession() {
	laptop := s.login(seedEmail, seedPassword)
	s.expireKeyPattern("mfa:email_code:cooldown:*")
	phone := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPost, "/v1/auth/logout", "", laptop.Access)
	s.Equal(http.StatusNoContent, res.Code, res.Raw)

	s.Equal(http.StatusUnauthorized, s.authed(http.MethodGet, "/v1/users/me", "", laptop.Access).Code)
	s.Equal(http.StatusOK, s.authed(http.MethodGet, "/v1/users/me", "", phone.Access).Code)
}

func (s *customerSuite) TestLogoutAll_EndsEverySession() {
	laptop := s.login(seedEmail, seedPassword)
	s.expireKeyPattern("mfa:email_code:cooldown:*")
	phone := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodPost, "/v1/auth/logout-all", "", laptop.Access)
	s.Equal(http.StatusNoContent, res.Code, res.Raw)

	s.Equal(http.StatusUnauthorized, s.authed(http.MethodGet, "/v1/users/me", "", laptop.Access).Code)
	s.Equal(http.StatusUnauthorized, s.authed(http.MethodGet, "/v1/users/me", "", phone.Access).Code)
}

func (s *customerSuite) TestAuthenticated_WithoutToken_IsUnauthorized() {
	res := s.authed(http.MethodGet, "/v1/users/me", "", "")

	s.Equal(http.StatusUnauthorized, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(apperror.Domain), domain)
	s.Equal(http.StatusUnauthorized, code)
}

func (s *customerSuite) TestAuthenticated_WithRefreshTokenAsBearer_IsUnauthorized() {
	tokens := s.login(seedEmail, seedPassword)

	res := s.authed(http.MethodGet, "/v1/users/me", "", tokens.Refresh)

	s.Equal(http.StatusUnauthorized, res.Code, res.Raw)
}

// ── Password recovery ────────────────────────────────────────────────────────

func (s *customerSuite) TestForgotPassword_KnownEmail_QueuesAResetLink() {
	res := s.post("/v1/auth/forgot-password", fmt.Sprintf(`{"email":%q}`, seedEmail))

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(int64(1), s.outboxCount(constants.PasswordReset))
	payload := s.outboxPayload(constants.PasswordReset)
	resetURL, _ := payload["reset_url"].(string)
	s.True(strings.HasPrefix(resetURL, frontURL+"/reset-password?token="), resetURL)
}

func (s *customerSuite) TestForgotPassword_UnknownEmail_AnswersOKAndSendsNothing() {
	res := s.post("/v1/auth/forgot-password", `{"email":"nobody@example.com"}`)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(int64(0), s.outboxCount(constants.PasswordReset))
}

func (s *customerSuite) TestResetPassword_TokenWorksOnceAndSignsOutEverywhere() {
	tokens := s.login(seedEmail, seedPassword)
	s.post("/v1/auth/forgot-password", fmt.Sprintf(`{"email":%q}`, seedEmail))
	resetURL, _ := s.outboxPayload(constants.PasswordReset)["reset_url"].(string)
	token := strings.TrimPrefix(resetURL, frontURL+"/reset-password?token=")

	const newPassword = "N3w-Str0ng!pass"
	res := s.post("/v1/auth/reset-password", fmt.Sprintf(
		`{"token":%q,"password":%q,"password_confirm":%q}`, token, newPassword, newPassword))
	s.Equal(http.StatusOK, res.Code, res.Raw)

	s.Equal(http.StatusUnauthorized, s.authed(http.MethodGet, "/v1/users/me", "", tokens.Access).Code, "old sessions are gone")

	again := s.post("/v1/auth/reset-password", fmt.Sprintf(
		`{"token":%q,"password":%q,"password_confirm":%q}`, token, newPassword, newPassword))
	s.Equal(user.ErrorInvalidOrExpiredToken.HttpStatus, again.Code, again.Raw)

	s.expireKeyPattern("mfa:email_code:cooldown:*")
	login := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, seedEmail, newPassword))
	s.Equal(http.StatusOK, login.Code, "the new password signs in")

	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET password = (SELECT password FROM users WHERE email = $1) WHERE email = $1`, seedEmail)
	s.Require().NoError(err)
	s.restoreSeedPassword()
}

func (s *customerSuite) TestResetPassword_MismatchedConfirmation_IsRejected() {
	res := s.post("/v1/auth/reset-password", `{"token":"x","password":"N3w-Str0ng!pass","password_confirm":"other-Pass1!"}`)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

// ── Rate limit ───────────────────────────────────────────────────────────────

func (s *customerSuite) TestRegister_SixthAttemptFromOneAddress_IsRateLimited() {
	var last *response
	for i := 0; i < 6; i++ {
		last = s.post("/v1/auth/register", fmt.Sprintf(
			`{"email":%q,"password":"Str0ng!pass","full_name":"Ada","country":"tr","locale":"en"}`, s.uniqueEmail("burst")))
		time.Sleep(time.Millisecond)
	}

	s.Equal(apperror.ErrorRateLimited.HttpStatus, last.Code, last.Raw)
	domain, code := last.errorCode()
	s.Equal(int(apperror.Domain), domain)
	s.Equal(apperror.ErrorRateLimited.Code, code)
	s.NotEmpty(last.Hdr.Get("Retry-After"))
}

// Two tabs refreshing at once must not sign each other out: the token a
// rotation just replaced stays accepted for a short grace.
func (s *customerSuite) TestRefresh_PreviousTokenWithinGrace_IsStillAccepted() {
	tokens := s.login(seedEmail, seedPassword)

	first := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Refresh))
	s.Require().Equal(http.StatusOK, first.Code, first.Raw)

	second := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Refresh))
	s.Equal(http.StatusOK, second.Code, second.Raw)

	// The original token is now two rotations back: that is a reuse.
	third := s.post("/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, tokens.Refresh))
	s.Equal(session.ErrorRefreshTokenReused.HttpStatus, third.Code, third.Raw)
}
