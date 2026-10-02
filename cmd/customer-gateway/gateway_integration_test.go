package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/suite"

	"github.com/piggystats/nodera/cmd/customer-gateway/config"
	apiMiddleware "github.com/piggystats/nodera/contexts/gateway/api/middleware"
	gatewayConfig "github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
)

const (
	jwtSecret        = "gateway-integration-test-secret"
	sessionKeyFormat = "session:%s"
	liveSessionID    = "019a0000-0000-7000-8000-00000000aaaa"
	deadSessionID    = "019a0000-0000-7000-8000-00000000bbbb"
	userID           = "019a0000-0000-7000-8000-000000000001"
	freshUserID      = "019a0000-0000-7000-8000-000000000002"
	ownerTeamID      = "019a0000-0000-7000-8000-00000000f001"
	financeTeamID    = "019a0000-0000-7000-8000-00000000f002"
	otherTeamID      = "019a0000-0000-7000-8000-00000000f003"
	// permVersion is what Redis holds for userID; a token stamped with
	// anything else is stale.
	permVersion = 3

	noTarget       = ""
	targetCustomer = "customer"
	targetDroplet  = "droplet"
	targetBilling  = "billing"
)

// stubUpstream records what reached it; that is how the suite reads what
// survived the hop.
type stubUpstream struct {
	name    string
	server  *httptest.Server
	mutex   sync.Mutex
	headers http.Header
	path    string
}

func newStubUpstream(name string) *stubUpstream {
	u := &stubUpstream{name: name}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mutex.Lock()
		u.headers = r.Header.Clone()
		u.path = r.URL.Path
		u.mutex.Unlock()
		w.Header().Set("X-Upstream", name)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	return u
}

func (u *stubUpstream) seen() (string, http.Header) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	return u.path, u.headers.Clone()
}

type gatewaySuite struct {
	suite.Suite
	pool          *dockertest.Pool
	redisResource *dockertest.Resource
	redis         redis.IService

	customer *stubUpstream
	droplet  *stubUpstream
	billing  *stubUpstream

	server *echo.Echo
}

func TestGatewaySuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(gatewaySuite))
}

func (s *gatewaySuite) SetupSuite() {
	var err error
	s.pool, err = dockertest.NewPool("")
	s.Require().NoError(err)

	s.redisResource, err = s.pool.RunWithOptions(
		&dockertest.RunOptions{Repository: "redis", Tag: "8.2"},
		func(hostConfig *docker.HostConfig) {
			hostConfig.AutoRemove = true
			hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
		},
	)
	s.Require().NoError(err)
	redisPort, _ := strconv.Atoi(s.redisResource.GetPort("6379/tcp"))

	var client redis.IClient
	s.Require().NoError(s.pool.Retry(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("redis not ready: %v", r)
			}
		}()
		client = redis.NewClient(&redis.Config{Host: "127.0.0.1", Port: redisPort}, logger.Noop())

		return nil
	}))
	s.redis = redis.NewService(client)
	s.Require().NoError(s.redis.Set(context.Background(), fmt.Sprintf(sessionKeyFormat, liveSessionID), "{}", time.Hour))
	s.Require().NoError(s.redis.Set(context.Background(), fmt.Sprintf(apiMiddleware.PermVersionKeyFormat, userID), strconv.Itoa(permVersion), time.Hour))

	s.customer = newStubUpstream("customer")
	s.droplet = newStubUpstream("droplet")
	s.billing = newStubUpstream("billing")

	routeTable := &gatewayConfig.GatewayConfig{Gateway: gatewayConfig.Gateway{Services: map[string]gatewayConfig.Service{
		"customer": {Host: s.customer.server.URL, Endpoints: gatewayConfig.Endpoints{
			{Path: "/v1/auth/login", Method: "POST", Public: true},
			{Path: "/v1/users/me", Method: "GET"},
			{Path: "/v1/teams/:teamID/projects", Method: "POST"},
			{Path: "/v1/teams/:teamID/invitations", Method: "POST", Permissions: []string{"team.manage"}},
			{Path: "/v1/teams/:teamID/members", Method: "GET", Permissions: []string{"team.read"}},
		}},
		"droplet": {Host: s.droplet.server.URL, Endpoints: gatewayConfig.Endpoints{
			{Path: "/v1/catalog/regions", Method: "GET"},
			{Path: "/v1/projects/:projectID/droplets", Method: "POST"},
			{Path: "/v1/droplets/:dropletID", Method: "DELETE"},
		}},
		"billing": {Host: s.billing.server.URL, Endpoints: gatewayConfig.Endpoints{
			{Path: "/v1/teams/:teamID/billing/profile", Method: "GET", Permissions: []string{"billing.read"}},
			{Path: "/v1/teams/:teamID/billing/status", Method: "GET", Permissions: []string{"team.read"}},
			{Path: "/v1/billing/webhooks/craftgate", Method: "POST", Public: true, Headerless: true},
			{Path: "/v1/billing/webhooks/stripe", Method: "POST", Public: true, Headerless: true},
			{Path: "/v1/billing/payments/:paymentID/3ds/callback", Method: "POST", Public: true, Headerless: true},
			{Path: "/v1/billing/payments/:paymentID/3ds/start", Method: "POST", Public: true},
		}},
	}}}
	routeTable.InitEndpoints()

	conf := &config.Config{
		Environment: "test",
		Server:      config.Server{Name: "gateway-test", Port: ":0"},
		Secret:      config.Secret{JWT: jwtSecret, SessionKeyFormat: sessionKeyFormat},
		CORS:        config.CORS{AllowOrigins: []string{"http://localhost:3002"}},
	}

	s.server, err = BuildServer(logger.Noop(), conf, routeTable, s.redis, func(target *url.URL) apiMiddleware.ReverseProxy {
		return apiMiddleware.DefaultReverseProxy(target)
	})
	s.Require().NoError(err)
}

func (s *gatewaySuite) TearDownSuite() {
	for _, u := range []*stubUpstream{s.customer, s.droplet, s.billing} {
		if u != nil {
			u.server.Close()
		}
	}
	if s.redisResource != nil {
		_ = s.pool.Purge(s.redisResource)
	}
}

// do sends a request through the gateway; an empty target leaves the
// target-service header off entirely.
func (s *gatewaySuite) do(method, path, token, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if target != "" {
		req.Header.Set(constants.TargetServerHeader, target)
	}
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, req)

	return rec
}

// defaultTeams is what the customer service would stamp for userID: owner of
// one Team, finance in another, no membership in otherTeamID.
func defaultTeams() map[string]string {
	return map[string]string{ownerTeamID: "owner", financeTeamID: "finance"}
}

// token mints a current access token the way the customer service does.
func (s *gatewaySuite) token(sessionID, typ string, expiresIn time.Duration) string {
	return s.tokenWith(sessionID, typ, expiresIn, func(claims jwt.MapClaims) {
		claims["teams"] = defaultTeams()
		claims["pv"] = permVersion
	})
}

// tokenWith lets a scenario shape the claims after the baseline is laid down.
func (s *gatewaySuite) tokenWith(sessionID, typ string, expiresIn time.Duration, shape func(claims jwt.MapClaims)) string {
	claims := jwt.MapClaims{
		"sub": userID,
		"sid": sessionID,
		"typ": typ,
		"exp": time.Now().Add(expiresIn).Unix(),
		"iat": time.Now().Unix(),
	}
	shape(claims)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
	s.Require().NoError(err)

	return signed
}

func (s *gatewaySuite) TestHealth_NeedsNoHeaderAndNoToken() {
	rec := s.do(http.MethodGet, "/health", "", noTarget)

	s.Equal(http.StatusOK, rec.Code)
}

func (s *gatewaySuite) TestMissingTargetHeader_IsUnauthorized() {
	rec := s.do(http.MethodPost, "/v1/auth/login", "", noTarget)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPublicRoute_IsProxiedWithoutToken() {
	rec := s.do(http.MethodPost, "/v1/auth/login", "", targetCustomer)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("customer", rec.Header().Get("X-Upstream"))
	s.NotEmpty(rec.Header().Get(constants.CorrelationIDHeader), "the caller gets the correlation id back")
	path, headers := s.customer.seen()
	s.Equal("/v1/auth/login", path)
	s.NotEmpty(headers.Get(constants.CorrelationIDHeader), "the upstream gets the same id")
}

func (s *gatewaySuite) TestUnlistedRoute_IsNotFound_EvenWithAValidToken() {
	rec := s.do(http.MethodGet, "/v1/users/me/not-published", s.token(liveSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
	s.Contains(rec.Body.String(), `"domain":100`)
}

func (s *gatewaySuite) TestListedRoute_WrongMethod_IsNotFound() {
	rec := s.do(http.MethodDelete, "/v1/users/me", s.token(liveSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_WithoutToken_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/users/me", "", targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_LiveSession_IsProxiedWithHeadersIntact() {
	token := s.token(liveSessionID, "access", time.Minute)

	rec := s.do(http.MethodPost, "/v1/teams/abc/projects", token, targetCustomer)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("customer", rec.Header().Get("X-Upstream"))
	path, headers := s.customer.seen()
	s.Equal("/v1/teams/abc/projects", path)
	s.Equal("Bearer "+token, headers.Get(echo.HeaderAuthorization), "the service re-validates the same token")
}

func (s *gatewaySuite) TestPrivateRoute_DeadSession_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/users/me", s.token(deadSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_ExpiredToken_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/users/me", s.token(liveSessionID, "access", -time.Minute), targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_RefreshTokenAsBearer_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/users/me", s.token(liveSessionID, "refresh", time.Minute), targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_WrongSecret_IsUnauthorized() {
	claims := jwt.MapClaims{"sub": userID, "sid": liveSessionID, "typ": "access", "exp": time.Now().Add(time.Minute).Unix()}
	forged, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("other-secret"))

	rec := s.do(http.MethodGet, "/v1/users/me", forged, targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestDropletRoute_LiveSession_IsProxiedToTheDropletUpstream() {
	token := s.token(liveSessionID, "access", time.Minute)

	rec := s.do(http.MethodGet, "/v1/catalog/regions", token, targetDroplet)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("droplet", rec.Header().Get("X-Upstream"))
	path, headers := s.droplet.seen()
	s.Equal("/v1/catalog/regions", path)
	s.Equal("Bearer "+token, headers.Get(echo.HeaderAuthorization), "the droplet service reads the user off the same token")
	s.Equal(targetDroplet, headers.Get(constants.TargetServerHeader))
	s.NotEmpty(headers.Get(constants.CorrelationIDHeader))
	s.Equal(rec.Header().Get(constants.CorrelationIDHeader), headers.Get(constants.CorrelationIDHeader))
}

func (s *gatewaySuite) TestDropletRoute_PathParams_IsProxiedToTheDropletUpstream() {
	rec := s.do(http.MethodDelete, "/v1/droplets/019a0000-0000-7000-8000-00000000d0d0", s.token(liveSessionID, "access", time.Minute), targetDroplet)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("droplet", rec.Header().Get("X-Upstream"))
}

func (s *gatewaySuite) TestDropletRoute_WithoutToken_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/catalog/regions", "", targetDroplet)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestDropletRoute_DeadSession_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/catalog/regions", s.token(deadSessionID, "access", time.Minute), targetDroplet)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestUnlistedDropletPath_IsNotFound_EvenWithAValidToken() {
	rec := s.do(http.MethodGet, "/v1/catalog/gpus", s.token(liveSessionID, "access", time.Minute), targetDroplet)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestCustomerPath_WithDropletTarget_IsNotFound() {
	rec := s.do(http.MethodGet, "/v1/users/me", s.token(liveSessionID, "access", time.Minute), targetDroplet)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestCustomerPublicPath_WithDropletTarget_IsNotPublic() {
	rec := s.do(http.MethodPost, "/v1/auth/login", "", targetDroplet)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestDropletPath_WithCustomerTarget_IsNotFound() {
	rec := s.do(http.MethodGet, "/v1/catalog/regions", s.token(liveSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestUnknownTarget_IsNotFound() {
	rec := s.do(http.MethodGet, "/v1/users/me", s.token(liveSessionID, "access", time.Minute), "admin")

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestMissingTargetHeader_OnDropletPath_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/catalog/regions", s.token(liveSessionID, "access", time.Minute), noTarget)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestGuardedRoute_Owner_IsProxied() {
	token := s.token(liveSessionID, "access", time.Minute)

	rec := s.do(http.MethodPost, "/v1/teams/"+ownerTeamID+"/invitations", token, targetCustomer)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("customer", rec.Header().Get("X-Upstream"))
	path, headers := s.customer.seen()
	s.Equal("/v1/teams/"+ownerTeamID+"/invitations", path)
	s.Equal("Bearer "+token, headers.Get(echo.HeaderAuthorization), "the service repeats the same check off the same token")
}

func (s *gatewaySuite) TestGuardedRoute_FinanceLacksThePermission_IsForbidden() {
	rec := s.do(http.MethodPost, "/v1/teams/"+financeTeamID+"/invitations", s.token(liveSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusForbidden, rec.Code, rec.Body.String())
	s.Contains(rec.Body.String(), `"domain":100`)
}

func (s *gatewaySuite) TestGuardedRoute_FinanceHoldsThePermission_IsProxied() {
	rec := s.do(http.MethodGet, "/v1/teams/"+financeTeamID+"/members", s.token(liveSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("customer", rec.Header().Get("X-Upstream"))
}

func (s *gatewaySuite) TestGuardedRoute_NotAMember_IsNotFound() {
	rec := s.do(http.MethodPost, "/v1/teams/"+otherTeamID+"/invitations", s.token(liveSessionID, "access", time.Minute), targetCustomer)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestGuardedRoute_NoTeamsAtAll_IsNotFound() {
	token := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {
		claims["teams"] = map[string]string{}
		claims["pv"] = permVersion
	})

	rec := s.do(http.MethodGet, "/v1/teams/"+ownerTeamID+"/members", token, targetCustomer)

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_StalePermissionVersion_IsUnauthorized() {
	token := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {
		claims["teams"] = defaultTeams()
		claims["pv"] = permVersion - 1
	})

	rec := s.do(http.MethodGet, "/v1/users/me", token, targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_TokenWithoutTeamsAndVersion_IsUnauthorized() {
	token := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {})

	rec := s.do(http.MethodGet, "/v1/users/me", token, targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestPrivateRoute_TokenWithoutVersionOnly_IsUnauthorized() {
	token := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {
		claims["teams"] = defaultTeams()
	})

	rec := s.do(http.MethodGet, "/v1/users/me", token, targetCustomer)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// A User whose memberships never changed has no version key yet; the customer
// service stamps 0 and the gateway must agree.
func (s *gatewaySuite) TestPrivateRoute_NoVersionKeyInRedis_TokenAtZero_IsProxied() {
	token := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {
		claims["sub"] = freshUserID
		claims["teams"] = map[string]string{}
		claims["pv"] = 0
	})

	rec := s.do(http.MethodGet, "/v1/users/me", token, targetCustomer)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestDropletRoute_NoPermissions_IsProxiedWithTheNewClaims() {
	token := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {
		claims["teams"] = map[string]string{financeTeamID: "finance"}
		claims["pv"] = permVersion
	})

	rec := s.do(http.MethodPost, "/v1/projects/p1/droplets", token, targetDroplet)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("droplet", rec.Header().Get("X-Upstream"))
}

func (s *gatewaySuite) TestBillingRoute_FinanceIsProxiedToTheBillingUpstream_DeveloperIsForbidden() {
	rec := s.do(http.MethodGet, "/v1/teams/"+financeTeamID+"/billing/profile", s.token(liveSessionID, "access", time.Minute), targetBilling)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("billing", rec.Header().Get("X-Upstream"))
	path, headers := s.billing.seen()
	s.Equal("/v1/teams/"+financeTeamID+"/billing/profile", path)
	s.Equal(targetBilling, headers.Get(constants.TargetServerHeader))

	developer := s.tokenWith(liveSessionID, "access", time.Minute, func(claims jwt.MapClaims) {
		claims["teams"] = map[string]string{otherTeamID: "developer"}
		claims["pv"] = permVersion
	})
	forbidden := s.do(http.MethodGet, "/v1/teams/"+otherTeamID+"/billing/profile", developer, targetBilling)
	s.Equal(http.StatusForbidden, forbidden.Code, forbidden.Body.String())
	s.Contains(forbidden.Body.String(), `"domain":100`)

	status := s.do(http.MethodGet, "/v1/teams/"+otherTeamID+"/billing/status", developer, targetBilling)
	s.Equal(http.StatusOK, status.Code, "every member reads the billing status: %s", status.Body.String())
	s.Equal("billing", status.Header().Get("X-Upstream"))
}

func (s *gatewaySuite) TestBillingWebhook_IsPublic() {
	rec := s.do(http.MethodPost, "/v1/billing/webhooks/craftgate", "", targetBilling)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("billing", rec.Header().Get("X-Upstream"))
	path, _ := s.billing.seen()
	s.Equal("/v1/billing/webhooks/craftgate", path)

	s.Equal(http.StatusUnauthorized, s.do(http.MethodPost, "/v1/billing/webhooks/craftgate", "", targetCustomer).Code, "public only under its own target")
}

// Payment providers and the card issuer's 3-D Secure page cannot add the
// target header; their routes resolve their service from the path alone.
func (s *gatewaySuite) TestHeaderlessRoutes_WithoutTargetHeader_ReachTheBillingUpstream() {
	for _, path := range []string{
		"/v1/billing/webhooks/craftgate",
		"/v1/billing/webhooks/stripe",
		"/v1/billing/payments/019a0000-0000-7000-8000-0000000000aa/3ds/callback",
	} {
		rec := s.do(http.MethodPost, path, "", noTarget)

		s.Equal(http.StatusOK, rec.Code, "%s: %s", path, rec.Body.String())
		s.Equal("billing", rec.Header().Get("X-Upstream"), path)
		seen, _ := s.billing.seen()
		s.Equal(path, seen)
	}
}

func (s *gatewaySuite) TestHeaderlessRoute_WrongMethodWithoutTargetHeader_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/billing/webhooks/stripe", "", noTarget)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// The web app starts the 3-D Secure flow and sends the header like any other
// call; being public does not make a route headerless.
func (s *gatewaySuite) TestPublicRoute_NotHeaderless_WithoutTargetHeader_IsUnauthorized() {
	rec := s.do(http.MethodPost, "/v1/billing/payments/019a0000-0000-7000-8000-0000000000aa/3ds/start", "", noTarget)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())

	withHeader := s.do(http.MethodPost, "/v1/billing/payments/019a0000-0000-7000-8000-0000000000aa/3ds/start", "", targetBilling)
	s.Equal(http.StatusOK, withHeader.Code, withHeader.Body.String())
	s.Equal("billing", withHeader.Header().Get("X-Upstream"))
}

// A header, once sent, is the whole answer: a headerless route named under
// the wrong target is judged by that target's table, as before.
func (s *gatewaySuite) TestHeaderlessRoute_WithAWrongTargetHeader_FollowsTheHeaderRules() {
	asCustomer := s.do(http.MethodPost, "/v1/billing/webhooks/stripe", "", targetCustomer)
	s.Equal(http.StatusUnauthorized, asCustomer.Code, "private under customer, and no token: %s", asCustomer.Body.String())

	asDroplet := s.do(http.MethodPost, "/v1/billing/webhooks/stripe", s.token(liveSessionID, "access", time.Minute), targetDroplet)
	s.Equal(http.StatusNotFound, asDroplet.Code, "unlisted under droplet: %s", asDroplet.Body.String())

	unknown := s.do(http.MethodPost, "/v1/billing/webhooks/stripe", "", "payments")
	s.Equal(http.StatusNotFound, unknown.Code, "an unknown target stays unknown: %s", unknown.Body.String())
}

// The route table is checked before the first request: permissions on a route
// with no Team in its path would never find a Role, so the gateway refuses to
// start rather than answer 404 to everyone.
func TestRouteTable_PermissionsOnARouteWithoutTeamID_RefusesToStart(t *testing.T) {
	routeTable := &gatewayConfig.GatewayConfig{Gateway: gatewayConfig.Gateway{Services: map[string]gatewayConfig.Service{
		"droplet": {Host: "http://droplet:9660", Endpoints: gatewayConfig.Endpoints{
			{Path: "/v1/droplets/:dropletID", Method: "DELETE", Permissions: []string{"droplet.write"}},
		}},
	}}}

	err := routeTable.Validate()

	if err == nil {
		t.Fatal("expected the route table to be refused")
	}
}
