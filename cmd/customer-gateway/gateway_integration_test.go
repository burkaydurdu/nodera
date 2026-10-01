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

	noTarget       = ""
	targetCustomer = "customer"
	targetDroplet  = "droplet"
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

	s.customer = newStubUpstream("customer")
	s.droplet = newStubUpstream("droplet")

	routeTable := &gatewayConfig.GatewayConfig{Gateway: gatewayConfig.Gateway{Services: map[string]gatewayConfig.Service{
		"customer": {Host: s.customer.server.URL, Endpoints: gatewayConfig.Endpoints{
			{Path: "/v1/auth/login", Method: "POST", Public: true},
			{Path: "/v1/users/me", Method: "GET"},
			{Path: "/v1/teams/:teamID/projects", Method: "POST"},
		}},
		"droplet": {Host: s.droplet.server.URL, Endpoints: gatewayConfig.Endpoints{
			{Path: "/v1/catalog/regions", Method: "GET"},
			{Path: "/v1/projects/:projectID/droplets", Method: "POST"},
			{Path: "/v1/droplets/:dropletID", Method: "DELETE"},
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
	for _, u := range []*stubUpstream{s.customer, s.droplet} {
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

func (s *gatewaySuite) token(sessionID, typ string, expiresIn time.Duration) string {
	claims := jwt.MapClaims{
		"sub": userID,
		"sid": sessionID,
		"typ": typ,
		"exp": time.Now().Add(expiresIn).Unix(),
		"iat": time.Now().Unix(),
	}
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
	rec := s.do(http.MethodGet, "/v1/users/me", s.token(liveSessionID, "access", time.Minute), "billing")

	s.Equal(http.StatusNotFound, rec.Code, rec.Body.String())
}

func (s *gatewaySuite) TestMissingTargetHeader_OnDropletPath_IsUnauthorized() {
	rec := s.do(http.MethodGet, "/v1/catalog/regions", s.token(liveSessionID, "access", time.Minute), noTarget)

	s.Equal(http.StatusUnauthorized, rec.Code, rec.Body.String())
}
