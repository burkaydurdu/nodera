package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"

	apiMiddleware "github.com/piggystats/nodera/contexts/gateway/api/middleware"
	gatewayConfig "github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

const (
	sendPath  = "/v1/mail/messages"
	projectID = "019a0000-0000-7000-8000-0000000000b1"
	teamID    = "019a0000-0000-7000-8000-00000000f001"
	// bodyLimit is the gateway's "15MB" as Echo reads it.
	bodyLimit = 15_000_000
	payload   = `{"from":{"email":"hello@example.com","name":"Example"},"to":[{"email":"ada@example.org"}],"subject":"Welcome","text":"Hello, Ada."}`
	// unauthorized is the one answer to every key that does not authenticate.
	unauthorized = `{"code":401,"domain":100,"message":"Unauthorized"}`
)

// sent is one request as the mail service received it.
type sent struct {
	path    string
	headers http.Header
	body    []byte
}

// stubMail stands in for the mail service's sending API. It reads the whole
// body before it answers, as the real one does, and records only what fully
// arrived: that is how the suite reads what survived the hop.
type stubMail struct {
	server   *httptest.Server
	mutex    sync.Mutex
	received []sent
}

func newStubMail() *stubMail {
	u := &stubMail{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}

		u.mutex.Lock()
		u.received = append(u.received, sent{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		u.mutex.Unlock()

		w.Header().Set("X-Upstream", "mail")
		w.Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"019a0000-0000-7000-8000-00000000e001","accepted":1,"suppressed":[]}`))
	}))

	return u
}

func (u *stubMail) count() int {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	return len(u.received)
}

func (u *stubMail) last() sent {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	if len(u.received) == 0 {
		return sent{}
	}

	return u.received[len(u.received)-1]
}

// apiKey is a key as the customer context issues it.
type apiKey struct {
	id     string
	secret string
}

func (k apiKey) bearer() string {
	return "Bearer nd_" + k.id + "." + k.secret
}

type integrationGatewaySuite struct {
	suite.Suite
	pool          *dockertest.Pool
	redisResource *dockertest.Resource
	rawRedis      *goredis.Client
	redis         redis.IService

	mail   *stubMail
	server *echo.Echo
}

func TestIntegrationGatewaySuite(t *testing.T) {
	if str.IsEmpty(os.Getenv("INTEGRATION")) {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(integrationGatewaySuite))
}

func (s *integrationGatewaySuite) SetupSuite() {
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

	s.rawRedis = goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:" + s.redisResource.GetPort("6379/tcp")})
	s.Require().NoError(s.pool.Retry(func() error {
		return s.rawRedis.Ping(context.Background()).Err()
	}))
	s.redis = redis.NewService(redis.NewClientFromRedis(s.rawRedis))

	s.mail = newStubMail()

	routeTable := &gatewayConfig.IntegrationGatewayConfig{Gateway: gatewayConfig.Gateway{
		Name: "nodera-integration-gateway",
		Services: map[string]gatewayConfig.Service{
			"mail": {Host: s.mail.server.URL, Endpoints: gatewayConfig.Endpoints{
				{Path: sendPath, Method: http.MethodPost, Permissions: []string{"mail.send"}},
			}},
		},
	}}
	routeTable.InitEndpoints()

	s.server, err = BuildServer(logger.Noop(), routeTable, s.redis, apiMiddleware.NewIntegrationReverseProxy(logger.Noop()))
	s.Require().NoError(err)
}

func (s *integrationGatewaySuite) TearDownSuite() {
	if s.mail != nil {
		s.mail.server.Close()
	}
	if s.rawRedis != nil {
		_ = s.rawRedis.Close()
	}
	if s.redisResource != nil {
		_ = s.pool.Purge(s.redisResource)
	}
}

// issueKey writes a key into Redis the way the customer context does on
// creation: a random id, the hash of a random secret, the Project, the Team
// and the Permissions.
func (s *integrationGatewaySuite) issueKey(permissions ...string) apiKey {
	key := apiKey{id: randomID(), secret: randomSecret()}
	if permissions == nil {
		permissions = []string{}
	}

	record, err := json.Marshal(map[string]any{
		"hash":        hash.SHA256Hex(key.secret),
		"project_id":  projectID,
		"team_id":     teamID,
		"permissions": permissions,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.redis.Set(context.Background(), fmt.Sprintf(apiMiddleware.APIKeyKeyFormat, key.id), string(record), 0))

	return key
}

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func randomID() string {
	id := make([]byte, 12)
	for i := range id {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Alphabet))))
		if err != nil {
			panic(err)
		}
		id[i] = base62Alphabet[n.Int64()]
	}

	return string(id)
}

// randomSecret renders 32 random bytes as one base62 number padded to 43
// digits, as the customer context does.
func randomSecret() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	encoded := new(big.Int).SetBytes(raw).Text(62)

	return strings.Repeat("0", 43-len(encoded)) + encoded
}

// send posts through the gateway; an empty authorization leaves the header
// off entirely.
func (s *integrationGatewaySuite) send(method, path, authorization string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if str.IsNotEmpty(authorization) {
		req.Header.Set(echo.HeaderAuthorization, authorization)
	}
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, req)

	return rec
}

func (s *integrationGatewaySuite) sendMessage(authorization string) *httptest.ResponseRecorder {
	return s.send(http.MethodPost, sendPath, authorization, strings.NewReader(payload))
}

func (s *integrationGatewaySuite) assertUnauthorized(rec *httptest.ResponseRecorder, msgAndArgs ...any) {
	s.Equal(http.StatusUnauthorized, rec.Code, msgAndArgs...)
	s.JSONEq(unauthorized, rec.Body.String(), msgAndArgs...)
}

func (s *integrationGatewaySuite) TestHealth_AnswersWithoutAKey() {
	rec := s.send(http.MethodGet, "/health", "", nil)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
}

func (s *integrationGatewaySuite) TestUnlistedRoute_IsNotFound_EvenWithAValidKey() {
	key := s.issueKey("mail.send")
	before := s.mail.count()

	cases := []struct{ method, path string }{
		{http.MethodGet, sendPath},
		{http.MethodPut, sendPath},
		{http.MethodPost, sendPath + "/"},
		{http.MethodPost, sendPath + "/batch"},
		// The management API stays behind the customer gateway.
		{http.MethodGet, "/v1/projects/" + projectID + "/mail"},
		{http.MethodPost, "/v1/droplets"},
	}

	for _, c := range cases {
		rec := s.send(c.method, c.path, key.bearer(), strings.NewReader(payload))

		s.Equal(http.StatusNotFound, rec.Code, "%s %s: %s", c.method, c.path, rec.Body.String())
		s.Contains(rec.Body.String(), `"domain":100`)
	}
	s.Equal(before, s.mail.count(), "nothing unlisted reaches the mail service")
}

func (s *integrationGatewaySuite) TestMissingOrMalformedKey_IsUnauthorized() {
	key := s.issueKey("mail.send")
	accessToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl"

	cases := map[string]string{
		"no header":                           "",
		"another scheme":                      "Basic " + key.secret,
		"the scheme alone":                    "Bearer",
		"the secret alone":                    "Bearer " + key.secret,
		"the public prefix alone":             "Bearer nd_" + key.id,
		"the scheme in lower case":            "bearer nd_" + key.id + "." + key.secret,
		"a trailing segment":                  key.bearer() + ".x",
		"a User's access token is not a key":  "Bearer " + accessToken,
		"an id that could never be issued":    "Bearer nd_" + key.id + "0." + key.secret,
		"a secret that could never be issued": key.bearer() + "-",
	}
	before := s.mail.count()

	for name, authorization := range cases {
		s.assertUnauthorized(s.sendMessage(authorization), name)
	}
	s.Equal(before, s.mail.count())
}

func (s *integrationGatewaySuite) TestUnknownKey_IsUnauthorized() {
	never := apiKey{id: randomID(), secret: randomSecret()}

	s.assertUnauthorized(s.sendMessage(never.bearer()))
}

func (s *integrationGatewaySuite) TestWrongSecret_IsUnauthorized() {
	key := s.issueKey("mail.send")
	guessed := apiKey{id: key.id, secret: randomSecret()}

	s.assertUnauthorized(s.sendMessage(guessed.bearer()))
}

// The customer context deletes the record on revocation; nothing else has to
// happen for the key to stop working.
func (s *integrationGatewaySuite) TestRevokedKey_IsUnauthorizedOnTheNextRequest() {
	key := s.issueKey("mail.send")
	s.Require().Equal(http.StatusAccepted, s.sendMessage(key.bearer()).Code)

	s.Require().NoError(s.redis.Delete(context.Background(), fmt.Sprintf(apiMiddleware.APIKeyKeyFormat, key.id)))

	s.assertUnauthorized(s.sendMessage(key.bearer()))
}

func (s *integrationGatewaySuite) TestKeyWithoutMailSend_IsForbidden() {
	key := s.issueKey()
	before := s.mail.count()

	rec := s.sendMessage(key.bearer())

	s.Equal(http.StatusForbidden, rec.Code, rec.Body.String())
	s.Contains(rec.Body.String(), `"domain":100`)
	s.Equal(before, s.mail.count(), "the mail service never sees it")
}

func (s *integrationGatewaySuite) TestValidKey_ReachesTheMailServiceUnchanged() {
	key := s.issueKey("mail.send")
	req := httptest.NewRequest(http.MethodPost, sendPath+"?dry=1", strings.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderAuthorization, key.bearer())
	req.Header.Set("Idempotency-Key", "order-1234")
	req.Header.Set(echo.HeaderOrigin, "https://example.org")
	rec := httptest.NewRecorder()

	s.server.ServeHTTP(rec, req)

	s.Equal(http.StatusAccepted, rec.Code, rec.Body.String())
	s.Equal("mail", rec.Header().Get("X-Upstream"))
	s.JSONEq(`{"id":"019a0000-0000-7000-8000-00000000e001","accepted":1,"suppressed":[]}`, rec.Body.String(), "the mail service's answer comes back as it was")
	s.NotEmpty(rec.Header().Get(constants.CorrelationIDHeader), "the caller gets the correlation id back")
	s.Equal("nosniff", rec.Header().Get(echo.HeaderXContentTypeOptions))
	s.Empty(rec.Header().Get(echo.HeaderAccessControlAllowOrigin), "software needs no CORS")

	seen := s.mail.last()
	s.Equal(sendPath, seen.path)
	s.Equal(payload, string(seen.body))
	s.Equal(key.bearer(), seen.headers.Get(echo.HeaderAuthorization), "the mail service checks the same key again")
	s.Equal("order-1234", seen.headers.Get("Idempotency-Key"))
	s.Equal(echo.MIMEApplicationJSON, seen.headers.Get(echo.HeaderContentType))
	s.Equal(rec.Header().Get(constants.CorrelationIDHeader), seen.headers.Get(constants.CorrelationIDHeader), "the mail service logs under the same id")
}

func (s *integrationGatewaySuite) TestTheHundredAndFirstRequestWithinASecond_IsTooManyRequests() {
	key := s.issueKey("mail.send")
	before := s.mail.count()

	start := time.Now()
	for i := 1; i <= 100; i++ {
		rec := s.sendMessage(key.bearer())
		s.Require().Equal(http.StatusAccepted, rec.Code, "request %d: %s", i, rec.Body.String())
	}
	limited := s.sendMessage(key.bearer())
	elapsed := time.Since(start)

	s.Require().Less(elapsed, time.Second, "the 101 requests must fall in one window for the scenario to hold")
	s.Equal(http.StatusTooManyRequests, limited.Code, limited.Body.String())
	s.Equal("1", limited.Header().Get(echo.HeaderRetryAfter))
	s.Contains(limited.Body.String(), fmt.Sprintf(`"code":%d`, apperror.ErrorRateLimited.Code))
	s.Equal(before+100, s.mail.count(), "the refused request never reaches the mail service")

	other := s.issueKey("mail.send")
	s.Equal(http.StatusAccepted, s.sendMessage(other.bearer()).Code, "every key has its own budget")

	time.Sleep(time.Second + 100*time.Millisecond)
	s.Equal(http.StatusAccepted, s.sendMessage(key.bearer()).Code, "once Retry-After has passed the key sends again")
}

func (s *integrationGatewaySuite) TestBodyOverTheLimit_IsRefusedBeforeTheMailService() {
	key := s.issueKey("mail.send")
	before := s.mail.count()

	rec := s.send(http.MethodPost, sendPath, key.bearer(), bytes.NewReader(make([]byte, bodyLimit+1)))

	s.Equal(http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	s.Contains(rec.Body.String(), `"domain":100`)
	s.Equal(before, s.mail.count())
}

// A chunked body declares no length, so it is cut short while it streams to
// the mail service; the caller still gets the 413, not a 502.
func (s *integrationGatewaySuite) TestChunkedBodyOverTheLimit_IsRefused() {
	key := s.issueKey("mail.send")
	before := s.mail.count()
	unsized := io.MultiReader(bytes.NewReader(make([]byte, bodyLimit)), strings.NewReader("x"))

	rec := s.send(http.MethodPost, sendPath, key.bearer(), unsized)

	s.Equal(http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	s.Contains(rec.Body.String(), `"domain":100`)
	s.Equal(before, s.mail.count(), "the mail service never takes a cut-short body as a request")
}

func (s *integrationGatewaySuite) TestBodyAtTheLimit_ReachesTheMailService() {
	key := s.issueKey("mail.send")
	body := bytes.Repeat([]byte("a"), bodyLimit)

	rec := s.send(http.MethodPost, sendPath, key.bearer(), bytes.NewReader(body))

	s.Equal(http.StatusAccepted, rec.Code, rec.Body.String())
	s.Equal(bodyLimit, len(s.mail.last().body))
}

func (s *integrationGatewaySuite) TestLastUse_IsStampedInUTCWithoutExpiry() {
	key := s.issueKey("mail.send")
	lastUsedKey := fmt.Sprintf(apiMiddleware.APIKeyLastUsedKeyFormat, key.id)

	before := time.Now()
	s.Require().Equal(http.StatusAccepted, s.sendMessage(key.bearer()).Code)
	after := time.Now()

	stamp, err := s.redis.GetString(context.Background(), lastUsedKey)
	s.Require().NoError(err)
	s.True(strings.HasSuffix(stamp, "Z"), "UTC: %s", stamp)
	usedAt, err := time.Parse(time.RFC3339Nano, stamp)
	s.Require().NoError(err)
	s.False(usedAt.Before(before.Round(0)), "%s is before the request", stamp)
	s.False(usedAt.After(after.Round(0)), "%s is after the request", stamp)

	ttl, err := s.rawRedis.TTL(context.Background(), lastUsedKey).Result()
	s.Require().NoError(err)
	s.Equal(time.Duration(-1), ttl, "the stamp never expires")
}
