package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/customer/config"
	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey/entry"
	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team/permversion"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/lockout"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/otp"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user/reset"
	"github.com/piggystats/nodera/contexts/customer/api"
	v1 "github.com/piggystats/nodera/contexts/customer/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/handlers"
	"github.com/piggystats/nodera/contexts/customer/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/ratelimit"
)

// Policies for the two numeric codes. The MFA code is short because a sign-in
// is a live interaction; the verification code is longer because a person
// may leave the sign-up page and come back from their inbox.
const (
	verificationCodeTTL      = 10 * time.Minute
	verificationCodeAttempts = 5
	verificationCodeCooldown = 60 * time.Second
	mfaCodeTTL               = 3 * time.Minute
	mfaCodeAttempts          = 5
	mfaCodeCooldown          = 60 * time.Second
)

// BuildServer wires the customer service over already-built dependencies. The
// binary and the integration suite both go through it, so the suite exercises
// the same chain the binary serves.
func BuildServer(
	log logger.ILog,
	conf *config.Config,
	pool *pgxpool.Pool,
	redisService redis.IService,
	outboxClient producers.OutboxClient,
	rateLimit int,
) *echo.Echo {
	authConfig := conf.ToAuthConfig()
	queries := postgresql.New(pool)
	transactions := postgresql.NewTransactionManager(pool)

	userService := user.NewService(user.NewRepository(queries))
	teamService := team.NewService(team.NewRepository(queries))
	projectService := project.NewService(project.NewRepository(queries))
	sshKeyService := sshkey.NewService(sshkey.NewRepository(queries))
	invitationService := invitation.NewService(invitation.NewRepository(queries))
	apiKeyService := apikey.NewService(apikey.NewRepository(queries))

	teamCreated := producers.NewTeamCreated(outboxClient)
	memberAdded := producers.NewTeamMemberAdded(outboxClient)
	roleChanged := producers.NewTeamMemberRoleChanged(outboxClient)
	memberRemoved := producers.NewTeamMemberRemoved(outboxClient)
	projectCreated := producers.NewProjectCreated(outboxClient)
	permVersions := permversion.NewStore(redisService)
	limiter := ratelimit.NewLimiter(redisService)
	joiner := handlers.NewJoiner(log, teamService, invitationService, transactions, permVersions, memberAdded)

	verificationCodes := otp.NewStore(redisService, otp.Policy{
		Prefix: "verify:email", TTL: verificationCodeTTL, MaxAttempts: verificationCodeAttempts,
		Cooldown: verificationCodeCooldown, DevCode: authConfig.DevCode,
	})
	mfaCodes := otp.NewStore(redisService, otp.Policy{
		Prefix: "mfa:email_code", TTL: mfaCodeTTL, MaxAttempts: mfaCodeAttempts,
		Cooldown: mfaCodeCooldown, DevCode: authConfig.DevCode,
	})

	mfaRegistry := mfa.NewRegistry()
	mfaRegistry.Register(mfa.NewEmailProvider(mfaCodes, producers.NewMFAEmailCode(outboxClient)))

	sessions := session.NewManager(redisService, session.Config{
		Secret:     authConfig.JWTSecret,
		AccessTTL:  authConfig.AccessTTL,
		RefreshTTL: authConfig.RefreshTTL,
		Issuer:     conf.Server.Name,
	})

	authHandler := handlers.NewAuth(
		log,
		authConfig,
		userService,
		teamService,
		invitationService,
		joiner,
		transactions,
		verificationCodes,
		producers.NewEmailVerificationCode(outboxClient),
		producers.NewEmailAlreadyRegistered(outboxClient),
		mfaRegistry,
		mfa.NewChallengeStore(redisService),
		lockout.NewStore(redisService),
		sessions,
		reset.NewStore(redisService),
		producers.NewPasswordReset(outboxClient),
		limiter,
		permVersions,
		teamCreated,
		memberAdded,
	)
	userHandler := handlers.NewUser(log, userService, teamService, sessions, producers.NewUserProfileUpdated(outboxClient))
	teamHandler := handlers.NewTeam(log, userService, teamService, projectService, transactions, permVersions, teamCreated, memberAdded, roleChanged, memberRemoved, projectCreated)
	sshKeyHandler := handlers.NewSSHKey(teamService, sshKeyService)
	apiKeyHandler := handlers.NewAPIKey(log, teamService, projectService, apiKeyService, transactions, entry.NewStore(redisService))
	invitationHandler := handlers.NewInvitation(log, authConfig, userService, teamService, invitationService, limiter, joiner, producers.NewTeamInvitation(outboxClient))

	authenticate := apiMiddleware.Authenticate(authConfig.JWTSecret, sessions)

	server := http.CreateEcho(log, rateLimit)
	api.InitSwagger(server)

	apiV1 := api.NewV1Routes(server)
	apiV1.Use(echo_middleware.GetCorrelationID(log))
	v1.NewAuth(authHandler, authenticate).SetRoutes(apiV1)
	v1.NewUser(userHandler, authenticate).SetRoutes(apiV1)
	v1.NewTeam(teamHandler, authenticate).SetRoutes(apiV1)
	v1.NewSSHKey(sshKeyHandler, authenticate).SetRoutes(apiV1)
	v1.NewAPIKey(apiKeyHandler, authenticate).SetRoutes(apiV1)
	v1.NewInvitation(invitationHandler, authenticate).SetRoutes(apiV1)

	return server
}
