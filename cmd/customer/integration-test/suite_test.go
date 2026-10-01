package integrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/piggystats/outbox"
	"github.com/stretchr/testify/suite"
	"go.mongodb.org/mongo-driver/bson"
	mongoDriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/piggystats/nodera/cmd/customer/app"
	"github.com/piggystats/nodera/cmd/customer/config"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
)

const (
	testJWTSecret = "customer-integration-test-jwt-secret"
	devCode       = "123456"
	seedEmail     = "dev@nodera.local"
	seedPassword  = "Nodera1234!"
	frontURL      = "http://localhost:3002"
)

// noopPublisher lets the outbox accept writes without a broker: the suite
// observes emails as outbox documents, never by relaying them.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string, string, any) error { return nil }

type customerSuite struct {
	suite.Suite
	pool             *dockertest.Pool
	postgresResource *dockertest.Resource
	redisResource    *dockertest.Resource
	mongoResource    *dockertest.Resource

	server  *echo.Echo
	pgPool  *pgxpool.Pool
	mongoDB *mongoDriver.Database
	redis   redis.IService
	outbox  *outbox.Outbox
}

func TestCustomerSuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(customerSuite))
}

func (s *customerSuite) SetupSuite() {
	var err error
	s.pool, err = dockertest.NewPool("")
	s.Require().NoError(err)
	s.Require().NoError(s.pool.Client.Ping())
	// Image pulls on a cold machine take longer than dockertest's default minute.
	s.pool.MaxWait = 3 * time.Minute

	s.postgresResource = s.run("postgres", "18", []string{"POSTGRES_USER=testuser", "POSTGRES_PASSWORD=testpassword", "POSTGRES_DB=testdb"})
	s.redisResource = s.run("redis", "8.2", nil)
	s.mongoResource = s.run("mongo", "8.2", nil)

	pgPort, _ := strconv.Atoi(s.postgresResource.GetPort("5432/tcp"))
	redisPort, _ := strconv.Atoi(s.redisResource.GetPort("6379/tcp"))
	mongoPort, _ := strconv.Atoi(s.mongoResource.GetPort("27017/tcp"))

	postgresURI := fmt.Sprintf("postgres://testuser:testpassword@127.0.0.1:%d/testdb?sslmode=disable", pgPort)

	s.Require().NoError(s.pool.Retry(func() error {
		pool, err := pgxpool.New(context.Background(), postgresURI)
		if err != nil {
			return err
		}
		if err := pool.Ping(context.Background()); err != nil {
			return err
		}
		s.pgPool = pool

		return nil
	}))

	s.Require().NoError(s.pool.Retry(func() error {
		client, err := mongoDriver.Connect(context.Background(), options.Client().ApplyURI(fmt.Sprintf("mongodb://127.0.0.1:%d/?directConnection=true", mongoPort)))
		if err != nil {
			return err
		}
		if err := client.Ping(context.Background(), readpref.Primary()); err != nil {
			return err
		}
		s.mongoDB = client.Database("testdb")

		return nil
	}))

	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/customer/db/migrations", "schema_migrations"))
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/customer/db/seed", "schema_seeds"))

	log := logger.Noop()
	var redisClient redis.IClient
	s.Require().NoError(s.pool.Retry(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("redis not ready: %v", r)
			}
		}()
		redisClient = redis.NewClient(&redis.Config{Host: "127.0.0.1", Port: redisPort}, log)

		return nil
	}))
	rawRedis = redisClient.GetClient()
	s.redis = redis.NewService(redisClient)

	outboxInstance, err := outbox.New(s.mongoDB.Client(), s.mongoDB, noopPublisher{}, log, "correlationID")
	s.Require().NoError(err)
	s.outbox = outboxInstance

	conf := &config.Config{
		Environment: "dev",
		Server:      config.Server{Name: "nodera-customer-test", Port: ":0"},
		Secret:      config.Secret{JWT: testJWTSecret},
		Auth:        config.Auth{AccessTTLMinutes: 15, RefreshTTLDays: 30, DevCode: devCode},
		Domain:      config.Domain{Front: frontURL},
	}

	const unlimited = 1_000_000
	s.server = app.BuildServer(log, conf, s.pgPool, s.redis, outboxInstance, unlimited)
}

func (s *customerSuite) TearDownSuite() {
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource, s.mongoResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

// TearDownTest leaves only the seeded User and clears every short-lived key,
// so each scenario starts from the same state.
func (s *customerSuite) TearDownTest() {
	ctx := context.Background()
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM projects`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM ssh_keys`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM team_members WHERE user_id NOT IN (SELECT id FROM users WHERE email = $1)`, seedEmail)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM teams WHERE owner_user_id NOT IN (SELECT id FROM users WHERE email = $1)`, seedEmail)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM users WHERE email != $1`, seedEmail)
	_, _ = s.pgPool.Exec(ctx, `UPDATE users SET mfa_method = 'email', status = 'active' WHERE email = $1`, seedEmail)
	_, _ = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	s.flushRedis()
}

func (s *customerSuite) run(repository, tag string, env []string) *dockertest.Resource {
	resource, err := s.pool.RunWithOptions(
		&dockertest.RunOptions{Repository: repository, Tag: tag, Env: env},
		func(hostConfig *docker.HostConfig) {
			hostConfig.AutoRemove = true
			hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
		},
	)
	s.Require().NoError(err, "could not start %s", repository)

	return resource
}

func runMigrate(databaseURL, path, table string) error {
	cmd := exec.Command("migrate", "-path", path, "-database", databaseURL+"&x-migrations-table="+table, "up")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (s *customerSuite) flushRedis() {
	// The service's Redis wrapper has no FLUSHALL on purpose; the suite reaches
	// past it through the same client the wrapper was built on.
	for _, pattern := range []string{"session:*", "user_sessions:*", "verify:*", "mfa:*", "login_lockout:*", "password_reset:*", "ratelimit:*"} {
		s.deleteByPattern(pattern)
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

type response struct {
	Code int
	Body map[string]any
	List []map[string]any
	Raw  string
	Hdr  http.Header
}

func (s *customerSuite) request(method, path, body, token string) *response {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("User-Agent", "integration-suite")
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, req)

	out := &response{Code: rec.Code, Raw: rec.Body.String(), Hdr: rec.Header()}
	trimmed := strings.TrimSpace(out.Raw)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		_ = json.Unmarshal(rec.Body.Bytes(), &out.Body)
	case strings.HasPrefix(trimmed, "["):
		_ = json.Unmarshal(rec.Body.Bytes(), &out.List)
	}

	return out
}

func (s *customerSuite) post(path, body string) *response {
	return s.request(http.MethodPost, path, body, "")
}

func (s *customerSuite) authed(method, path, body, token string) *response {
	return s.request(method, path, body, token)
}

func (r *response) errorCode() (domain, code int) {
	d, _ := r.Body["domain"].(float64)
	c, _ := r.Body["code"].(float64)

	return int(d), int(c)
}

func (r *response) str(key string) string {
	v, _ := r.Body[key].(string)

	return v
}

// ---------------------------------------------------------------------------
// Flow helpers
// ---------------------------------------------------------------------------

type tokens struct {
	Access  string
	Refresh string
}

func tokensOf(body map[string]any) tokens {
	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)

	return tokens{Access: access, Refresh: refresh}
}

// register signs a new User up and verifies them, answering their first session.
func (s *customerSuite) registerAndVerify(email, password string) tokens {
	res := s.post("/v1/auth/register", fmt.Sprintf(
		`{"email":%q,"password":%q,"full_name":"Test Person","country":"tr","locale":"en"}`, email, password))
	s.Require().Equal(http.StatusCreated, res.Code, res.Raw)

	res = s.post("/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":%q}`, email, devCode))
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return tokensOf(res.Body)
}

// login runs both sign-in steps for a User with email MFA on.
func (s *customerSuite) login(email, password string) tokens {
	res := s.post("/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	s.Require().Equal(true, res.Body["mfa_required"])

	res = s.post("/v1/auth/mfa/verify", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, res.str("challenge_token"), devCode))
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return tokensOf(res.Body)
}

func (s *customerSuite) outboxCount(routingKey string) int64 {
	n, err := s.mongoDB.Collection("outbox").CountDocuments(context.Background(), bson.M{"routing_key": routingKey})
	s.Require().NoError(err)

	return n
}

// outboxPayload answers the newest payload published under routingKey.
func (s *customerSuite) outboxPayload(routingKey string) map[string]any {
	var doc struct {
		Payload string `bson:"payload"`
	}
	opts := options.FindOne().SetSort(bson.M{"created_at": -1})
	err := s.mongoDB.Collection("outbox").FindOne(context.Background(), bson.M{"routing_key": routingKey}, opts).Decode(&doc)
	s.Require().NoError(err)

	var event map[string]any
	s.Require().NoError(json.Unmarshal([]byte(doc.Payload), &event))
	payload, _ := event["payload"].(map[string]any)

	return payload
}

// outboxPayloads answers one payload field of every document under routingKey.
func (s *customerSuite) outboxPayloads(routingKey, field string) []string {
	cursor, err := s.mongoDB.Collection("outbox").Find(context.Background(), bson.M{"routing_key": routingKey})
	s.Require().NoError(err)
	defer func() { _ = cursor.Close(context.Background()) }()

	values := make([]string, 0)
	for cursor.Next(context.Background()) {
		var doc struct {
			Payload string `bson:"payload"`
		}
		s.Require().NoError(cursor.Decode(&doc))
		var event map[string]any
		s.Require().NoError(json.Unmarshal([]byte(doc.Payload), &event))
		payload, _ := event["payload"].(map[string]any)
		value, _ := payload[field].(string)
		values = append(values, value)
	}

	return values
}

func (s *customerSuite) clearOutbox() {
	_, err := s.mongoDB.Collection("outbox").DeleteMany(context.Background(), bson.M{})
	s.Require().NoError(err)
}

// isRFC3339UTC holds the contract's timestamp shape: RFC 3339 in UTC.
func (s *customerSuite) isRFC3339UTC(value any) {
	raw, _ := value.(string)
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	s.Require().NoError(err, raw)
	s.True(strings.HasSuffix(raw, "Z"), "timestamp is not UTC: %s", raw)
	s.False(parsed.IsZero())
}

func (s *customerSuite) teamID(token string) string {
	res := s.authed(http.MethodGet, "/v1/users/me", "", token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	team, _ := res.Body["team"].(map[string]any)
	id, _ := team["id"].(string)
	s.Require().NotEmpty(id)

	return id
}

func (s *customerSuite) expireKeyPattern(pattern string) {
	s.deleteByPattern(pattern)
}

func (s *customerSuite) uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

// restoreSeedPassword puts the seeded password back after a scenario changed it.
func (s *customerSuite) restoreSeedPassword() {
	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE users SET password = $2 WHERE email = $1`, seedEmail, seedPasswordHash)
	s.Require().NoError(err)
}

const seedPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$qa8sYQvS16KqHEDgxE9lVw$Fx9W4O9nejpcXm+M953RMqgLIrADpinv0m2h6tk7KR4"
