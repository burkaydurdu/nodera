package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/lockout"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/otp"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/reset"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/contexts/customer/async/events"
	"github.com/piggystats/nodera/contexts/customer/config"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/pkg/ratelimit"
	"github.com/piggystats/nodera/utils/hash"
)

// Rate-limit budgets on the public endpoints, per source address and per email.
var (
	ruleRegisterIP  = ratelimit.Rule{Window: 10 * time.Minute, Limit: 5}
	ruleVerifyIP    = ratelimit.Rule{Window: 10 * time.Minute, Limit: 20}
	ruleVerifyEmail = ratelimit.Rule{Window: 10 * time.Minute, Limit: 10}
	ruleResendEmail = ratelimit.Rule{Window: 10 * time.Minute, Limit: 5}
	ruleLoginIP     = ratelimit.Rule{Window: 10 * time.Minute, Limit: 20}
	ruleLoginEmail  = ratelimit.Rule{Window: 10 * time.Minute, Limit: 10}
	ruleMFAIP       = ratelimit.Rule{Window: 10 * time.Minute, Limit: 20}
	ruleForgotIP    = ratelimit.Rule{Window: 10 * time.Minute, Limit: 10}
	ruleForgotEmail = ratelimit.Rule{Window: 1 * time.Hour, Limit: 3}
	ruleResetIP     = ratelimit.Rule{Window: 10 * time.Minute, Limit: 10}
)

//go:generate mockgen -destination=../../mock/producers/email_verification_code.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers IEmailVerificationCodeProducer
type IEmailVerificationCodeProducer interface {
	Publish(ctx context.Context, recipient events.Recipient, code string) error
}

//go:generate mockgen -destination=../../mock/producers/email_already_registered.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers IEmailAlreadyRegisteredProducer
type IEmailAlreadyRegisteredProducer interface {
	Publish(ctx context.Context, recipient events.Recipient) error
}

//go:generate mockgen -destination=../../mock/producers/password_reset.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers IPasswordResetProducer
type IPasswordResetProducer interface {
	Publish(ctx context.Context, recipient events.Recipient, resetURL string) error
}

type Auth struct {
	log                logger.ILog
	config             config.Auth
	users              user.IService
	teams              team.IService
	transactions       postgresql.ITransactionManager
	verificationCodes  *otp.Store
	verificationMailer IEmailVerificationCodeProducer
	registeredMailer   IEmailAlreadyRegisteredProducer
	mfaRegistry        *mfa.Registry
	challenges         *mfa.ChallengeStore
	lockouts           *lockout.Store
	sessions           session.IManager
	resets             *reset.Store
	resetMailer        IPasswordResetProducer
	limiter            ratelimit.ILimiter
	teamCreated        ITeamCreatedProducer
	memberAdded        ITeamMemberAddedProducer
}

func NewAuth(
	log logger.ILog,
	cfg config.Auth,
	users user.IService,
	teams team.IService,
	transactions postgresql.ITransactionManager,
	verificationCodes *otp.Store,
	verificationMailer IEmailVerificationCodeProducer,
	registeredMailer IEmailAlreadyRegisteredProducer,
	mfaRegistry *mfa.Registry,
	challenges *mfa.ChallengeStore,
	lockouts *lockout.Store,
	sessions session.IManager,
	resets *reset.Store,
	resetMailer IPasswordResetProducer,
	limiter ratelimit.ILimiter,
	teamCreated ITeamCreatedProducer,
	memberAdded ITeamMemberAddedProducer,
) *Auth {
	return &Auth{
		log:                log,
		config:             cfg,
		users:              users,
		teams:              teams,
		transactions:       transactions,
		verificationCodes:  verificationCodes,
		verificationMailer: verificationMailer,
		registeredMailer:   registeredMailer,
		mfaRegistry:        mfaRegistry,
		challenges:         challenges,
		lockouts:           lockouts,
		sessions:           sessions,
		resets:             resets,
		resetMailer:        resetMailer,
		limiter:            limiter,
		teamCreated:        teamCreated,
		memberAdded:        memberAdded,
	}
}

// Register creates the User, their Team and the owner membership in one
// transaction, then sends the verification code. A duplicate email is not an
// error: the address's owner is told by mail, and the caller sees the same
// 201 as everyone else.
func (h *Auth) Register(ctx context.Context, req *user.RegisterRequest) error {
	if err := h.limiter.Allow(ctx, "register:ip", contextutil.ClientIP(ctx), ruleRegisterIP); err != nil {
		return err
	}

	existing, err := h.users.FindByEmail(ctx, req.Email)
	if err != nil && !errors.Is(err, user.ErrorNotFound) {
		return err
	}
	if existing != nil {
		if err := h.registeredMailer.Publish(ctx, recipientOf(existing)); err != nil {
			h.log.Error(err)
		}

		return nil
	}

	passwordHash, err := hash.GeneratePasswordHash(req.Password)
	if err != nil {
		return user.ErrorCannotCreate.Copy().SetError(err)
	}

	newUser := &user.User{
		Email:     req.Email,
		Password:  passwordHash,
		FullName:  req.FullName,
		Locale:    user.Locale(req.Locale),
		Country:   req.Country,
		Status:    user.StatusPendingVerification,
		MFAMethod: mfa.MethodEmail,
	}

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}

	if err := h.users.Create(txCtx, newUser); err != nil {
		_ = h.transactions.Rollback(txCtx)
		// A double submit lost the race to the unique index: same answer as
		// the pre-check, the owner already has an account.
		if errors.Is(err, user.ErrorEmailTaken) {
			return nil
		}

		return err
	}

	newTeam := &team.Team{Name: req.FullName, OwnerUserID: newUser.ID}
	if err := h.teams.Create(txCtx, newTeam); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}
	owner, err := h.teams.AddMember(txCtx, newTeam.ID, newUser.ID, team.RoleOwner)
	if err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}

	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}

	publishTeamOpened(ctx, h.log, h.teamCreated, h.memberAdded, newTeam, owner)

	return h.sendVerificationCode(ctx, newUser, true)
}

// VerifyEmail activates the User and opens their first session, so the code
// that just proved the address doubles as the MFA step of that sign-in.
func (h *Auth) VerifyEmail(ctx context.Context, req *user.VerifyEmailRequest) (*dto.Session, error) {
	if err := h.limiter.Allow(ctx, "verify:ip", contextutil.ClientIP(ctx), ruleVerifyIP); err != nil {
		return nil, err
	}
	if err := h.limiter.Allow(ctx, "verify:email", req.Email, ruleVerifyEmail); err != nil {
		return nil, err
	}

	u, err := h.users.FindByEmail(ctx, req.Email)
	if err != nil || !u.IsPendingVerification() {
		return nil, otp.ErrorInvalidCode
	}

	if err := h.verificationCodes.Verify(ctx, u.ID.String(), req.Code); err != nil {
		return nil, err
	}

	if err := h.users.MarkEmailVerified(ctx, u.ID); err != nil {
		return nil, err
	}
	_ = h.verificationCodes.Clear(ctx, u.ID.String())
	u.Status = user.StatusActive

	return h.openSession(ctx, u)
}

// ResendVerification answers 200 whatever it finds, so the endpoint cannot be
// used to discover accounts. The cooldown is enforced silently for the same
// reason; the web app keeps its own countdown.
func (h *Auth) ResendVerification(ctx context.Context, req *user.ResendVerificationRequest) error {
	if err := h.limiter.Allow(ctx, "resend:email", req.Email, ruleResendEmail); err != nil {
		return err
	}

	u, err := h.users.FindByEmail(ctx, req.Email)
	if err != nil || !u.IsPendingVerification() {
		return nil
	}

	if err := h.sendVerificationCode(ctx, u, false); err != nil && !errors.Is(err, otp.ErrorCooldown) {
		return err
	}

	return nil
}

// Login is step one of sign-in: password, then either a Login challenge for
// the User's MFA method or, with MFA off, the session itself.
func (h *Auth) Login(ctx context.Context, req *user.LoginRequest) (*dto.LoginResponse, error) {
	if locked, retryAfter := h.lockouts.Status(ctx, req.Email); locked {
		return nil, accountLockedError(retryAfter)
	}
	if err := h.limiter.Allow(ctx, "login:ip", contextutil.ClientIP(ctx), ruleLoginIP); err != nil {
		return nil, err
	}
	if err := h.limiter.Allow(ctx, "login:email", req.Email, ruleLoginEmail); err != nil {
		return nil, err
	}

	u, err := h.users.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, h.registerLoginFailure(ctx, req.Email)
	}
	if !hash.ComparePasswordHash(req.Password, u.Password) {
		return nil, h.registerLoginFailure(ctx, req.Email)
	}

	if err := requireActive(u); err != nil {
		return nil, err
	}

	if err := h.lockouts.Reset(ctx, req.Email); err != nil {
		h.log.Error(err)
	}

	if !u.MFAEnabled() {
		s, err := h.openSession(ctx, u)
		if err != nil {
			return nil, err
		}

		return &dto.LoginResponse{MFARequired: false, Session: s}, nil
	}

	provider, err := h.mfaRegistry.Lookup(u.MFAMethod)
	if err != nil {
		return nil, err
	}

	if err := provider.IssueChallenge(ctx, challengeContextOf(u)); err != nil {
		return nil, err
	}

	token, err := h.challenges.Create(ctx, mfa.Challenge{UserID: u.ID, Method: u.MFAMethod})
	if err != nil {
		return nil, err
	}

	return &dto.LoginResponse{
		MFARequired:        true,
		ChallengeToken:     token,
		Method:             u.MFAMethod.String(),
		SupportsResend:     provider.SupportsResend(),
		ResendAfterSeconds: provider.ResendAfterSeconds(),
	}, nil
}

func (h *Auth) VerifyMFA(ctx context.Context, req *user.MFAVerifyRequest) (*dto.Session, error) {
	if err := h.limiter.Allow(ctx, "mfa:ip", contextutil.ClientIP(ctx), ruleMFAIP); err != nil {
		return nil, err
	}

	u, provider, err := h.openChallenge(ctx, req.ChallengeToken)
	if err != nil {
		return nil, err
	}

	if err := provider.VerifyCode(ctx, challengeContextOf(u), req.Code); err != nil {
		if errors.Is(err, otp.ErrorAttemptsExhausted) {
			_ = h.challenges.Delete(ctx, req.ChallengeToken)
		}

		return nil, err
	}

	_ = h.challenges.Delete(ctx, req.ChallengeToken)

	return h.openSession(ctx, u)
}

func (h *Auth) ResendMFA(ctx context.Context, req *user.MFAResendRequest) error {
	if err := h.limiter.Allow(ctx, "mfa:ip", contextutil.ClientIP(ctx), ruleMFAIP); err != nil {
		return err
	}

	u, provider, err := h.openChallenge(ctx, req.ChallengeToken)
	if err != nil {
		return err
	}
	if !provider.SupportsResend() {
		return mfa.ErrorResendNotSupported
	}

	return provider.ResendChallenge(ctx, challengeContextOf(u))
}

// Refresh rotates the session's refresh token. The User is re-read so a
// suspension takes effect at the next refresh at the latest.
func (h *Auth) Refresh(ctx context.Context, req *user.RefreshRequest) (*dto.Session, error) {
	pair, err := h.sessions.Refresh(ctx, req.RefreshToken, deviceOf(ctx), func(ctx context.Context, userID uuid.UUID) (*session.Principal, error) {
		u, err := h.users.FindByID(ctx, userID)
		if err != nil {
			return nil, session.ErrorInvalidRefreshToken
		}
		if err := requireActive(u); err != nil {
			return nil, err
		}

		return principalOf(u), nil
	})
	if err != nil {
		return nil, err
	}

	return dto.ToSession(pair), nil
}

func (h *Auth) Logout(ctx context.Context) error {
	return h.sessions.Revoke(ctx, contextutil.CurrentUserID(ctx), contextutil.CurrentSessionID(ctx))
}

func (h *Auth) LogoutAll(ctx context.Context) error {
	return h.sessions.RevokeAll(ctx, contextutil.CurrentUserID(ctx))
}

// ForgotPassword answers 200 whatever it finds.
func (h *Auth) ForgotPassword(ctx context.Context, req *user.ForgotPasswordRequest) error {
	if err := h.limiter.Allow(ctx, "forgot:ip", contextutil.ClientIP(ctx), ruleForgotIP); err != nil {
		return err
	}
	if err := h.limiter.Allow(ctx, "forgot:email", req.Email, ruleForgotEmail); err != nil {
		return err
	}

	u, err := h.users.FindByEmail(ctx, req.Email)
	if err != nil || !u.IsActive() {
		return nil
	}

	token, err := h.resets.Create(ctx, u.ID)
	if err != nil {
		return err
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", h.config.FrontURL, token)

	return h.resetMailer.Publish(ctx, recipientOf(u), resetURL)
}

// ResetPassword consumes the single-use token, sets the password and signs
// the User out everywhere: whoever caused the reset is signed out too.
func (h *Auth) ResetPassword(ctx context.Context, req *user.ResetPasswordRequest) error {
	if err := h.limiter.Allow(ctx, "reset:ip", contextutil.ClientIP(ctx), ruleResetIP); err != nil {
		return err
	}

	passwordHash, err := hash.GeneratePasswordHash(req.Password)
	if err != nil {
		return user.ErrorCannotUpdate.Copy().SetError(err)
	}

	userID, ok := h.resets.Consume(ctx, req.Token)
	if !ok {
		return user.ErrorInvalidOrExpiredToken
	}

	if err := h.users.UpdatePassword(ctx, userID, passwordHash); err != nil {
		return err
	}

	return h.sessions.RevokeAll(ctx, userID)
}

func (h *Auth) sendVerificationCode(ctx context.Context, u *user.User, fresh bool) error {
	var (
		code string
		err  error
	)
	if fresh {
		code, err = h.verificationCodes.Renew(ctx, u.ID.String())
	} else {
		code, err = h.verificationCodes.Issue(ctx, u.ID.String())
	}
	if err != nil {
		return err
	}

	return h.verificationMailer.Publish(ctx, recipientOf(u), code)
}

func (h *Auth) openChallenge(ctx context.Context, token string) (*user.User, mfa.Provider, error) {
	challenge, err := h.challenges.Get(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	u, err := h.users.FindByID(ctx, challenge.UserID)
	if err != nil {
		return nil, nil, mfa.ErrorInvalidChallenge
	}
	if err := requireActive(u); err != nil {
		return nil, nil, err
	}

	provider, err := h.mfaRegistry.Lookup(challenge.Method)
	if err != nil {
		return nil, nil, err
	}

	return u, provider, nil
}

func (h *Auth) openSession(ctx context.Context, u *user.User) (*dto.Session, error) {
	pair, err := h.sessions.Issue(ctx, *principalOf(u), deviceOf(ctx))
	if err != nil {
		return nil, err
	}

	return dto.ToSession(pair), nil
}

// registerLoginFailure records the miss and answers the enumeration-safe
// not-found, or the lock error on the attempt that tripped it. A store error
// degrades to the plain not-found so a Redis blip never blocks a valid user.
func (h *Auth) registerLoginFailure(ctx context.Context, email string) error {
	justLocked, retryAfter, err := h.lockouts.RegisterFailure(ctx, email)
	if err != nil {
		h.log.Error(err)

		return user.ErrorNotFound
	}
	if justLocked {
		return accountLockedError(retryAfter)
	}

	return user.ErrorNotFound
}

func accountLockedError(retryAfter time.Duration) rerror.Error {
	seconds := max(int(retryAfter.Seconds()), 1)

	return user.ErrorAccountLocked.Copy().Add("retry_after_seconds", seconds)
}

func requireActive(u *user.User) error {
	switch u.Status {
	case user.StatusActive:
		return nil
	case user.StatusPendingVerification:
		return user.ErrorEmailNotVerified
	default:
		return user.ErrorSuspended
	}
}

func recipientOf(u *user.User) events.Recipient {
	return events.Recipient{UserID: u.ID, Email: u.Email, FullName: u.FullName, Locale: string(u.Locale)}
}

func challengeContextOf(u *user.User) mfa.ChallengeContext {
	return mfa.ChallengeContext{UserID: u.ID, Email: u.Email, FullName: u.FullName, Locale: string(u.Locale)}
}

func principalOf(u *user.User) *session.Principal {
	return &session.Principal{UserID: u.ID, Email: u.Email, Status: string(u.Status)}
}

func deviceOf(ctx context.Context) session.Device {
	return session.Device{IP: contextutil.ClientIP(ctx), UserAgent: contextutil.ClientUserAgent(ctx)}
}
