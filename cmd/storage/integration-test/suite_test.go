package integrationtest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/piggystats/outbox"
	"github.com/piggystats/rabbitmq"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
	"go.mongodb.org/mongo-driver/bson"
	mongoDriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/piggystats/nodera/cmd/storage/app"
	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/contexts/storage/application/jobs"
	"github.com/piggystats/nodera/contexts/storage/async/constants"
	"github.com/piggystats/nodera/contexts/storage/async/consumers"
	"github.com/piggystats/nodera/contexts/storage/engine/fake"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/secrets"
	"github.com/piggystats/nodera/utils/str"
)

const (
	testJWTSecret = "storage-integration-test-jwt-secret"
	testEndpoint  = "https://vno1.storage.test"
	sessionKey    = "session:%s"
)

// start is mid-month, so the Period and the UTC day are well away from a
// boundary unless a scenario moves the clock there.
var start = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

// noopPublisher lets the outbox accept writes without a broker: the suite
// observes the events as outbox documents, never by relaying them.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string, string, any) error { return nil }

// clock is the time every handler and job acts at.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type storageSuite struct {
	suite.Suite
	pool             *dockertest.Pool
	postgresResource *dockertest.Resource
	redisResource    *dockertest.Resource
	mongoResource    *dockertest.Resource

	pgPool   *pgxpool.Pool
	mongoDB  *mongoDriver.Database
	rawRedis *goredis.Client

	clock  *clock
	engine *fake.Fake

	handlers   *app.Handlers
	management *echo.Echo
	jobs       *jobs.Jobs
	customer   *consumers.CustomerEvents
	billing    *consumers.BillingEvents
}

func TestStorageSuite(t *testing.T) {
	if str.IsEmpty(os.Getenv("INTEGRATION")) {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(storageSuite))
}

func (s *storageSuite) SetupSuite() {
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

	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/storage/db/migrations", "schema_migrations"))
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/storage/db/seed", "schema_seeds"))

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
	s.rawRedis = redisClient.GetClient()

	outboxInstance, err := outbox.New(s.mongoDB.Client(), s.mongoDB, noopPublisher{}, log, "correlationID")
	s.Require().NoError(err)

	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	s.Require().NoError(err)

	s.clock = &clock{now: start}
	s.engine = fake.New()

	conf := &config.Config{
		Environment: "test",
		Server:      config.Server{Name: "nodera-storage-test"},
		Engine:      config.Engine{Region: "vno1", PublicEndpoint: testEndpoint},
		Secret:      config.Secret{JWT: testJWTSecret},
	}
	deps := app.Deps{
		Log: log, Conf: conf, Pool: s.pgPool, Redis: redis.NewService(redisClient), Outbox: outboxInstance,
		Engine: s.engine, Sealer: sealer, Clock: s.clock.Now,
	}

	const unlimited = 1_000_000
	s.handlers = app.NewHandlers(deps)
	s.management = app.BuildManagement(deps, s.handlers, unlimited)
	s.jobs = app.BuildJobs(deps, s.handlers)
	s.customer = app.BuildCustomerConsumer(s.handlers)
	s.billing = app.BuildBillingConsumer(s.handlers)
}

func (s *storageSuite) TearDownSuite() {
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource, s.mongoResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

func (s *storageSuite) TearDownTest() {
	ctx := context.Background()
	for _, table := range []string{
		"storage_traffic", "storage_sizes", "access_keys", "storages", "project_accounts",
		"team_billing", "team_access", "project_access",
	} {
		_, err := s.pgPool.Exec(ctx, "DELETE FROM "+table)
		s.Require().NoError(err)
	}
	_, _ = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	_ = s.rawRedis.FlushDB(ctx).Err()
	s.engine.Reset()
	s.clock.Set(start)
}

func (s *storageSuite) run(repository, tag string, env []string) *dockertest.Resource {
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

// ---------------------------------------------------------------------------
// People, Teams and Projects
// ---------------------------------------------------------------------------

type user struct {
	ID    uuid.UUID
	Token string
}

// signIn mints an access token exactly as the customer service would and
// stores the session record it points at.
func (s *storageSuite) signIn() user {
	userID, sessionID := uuid.New(), uuid.New()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "nodera-customer", "sub": userID.String(), "iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
		"typ": "access", "sid": sessionID.String(), "email": userID.String() + "@example.com", "status": "active",
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	s.Require().NoError(err)
	s.Require().NoError(s.rawRedis.Set(context.Background(), fmt.Sprintf(sessionKey, sessionID), "1", time.Hour).Err())

	return user{ID: userID, Token: token}
}

func (s *storageSuite) consume(routingKey, payload string) {
	s.Require().NoError(s.consumeErr(routingKey, payload))
}

func (s *storageSuite) consumeErr(routingKey, payload string) error {
	message := &rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload), CorrelationID: uuid.NewString()}
	switch routingKey {
	case constants.TeamBillingStatusChanged, constants.TeamDropletsDeletionRequested:
		return s.billing.Consume(message)
	default:
		return s.customer.Consume(message)
	}
}

type project struct {
	TeamID    uuid.UUID
	ProjectID uuid.UUID
	Owner     user
}

// grantProject replays what makes a Project usable for Storage: the owner's
// Team and membership and the named Project from the customer context, and a
// payment method from the billing context.
func (s *storageSuite) grantProject() project {
	p := s.grantProjectWithoutBilling()
	s.setBilling(p.TeamID, "active", time.Now())

	return p
}

func (s *storageSuite) grantProjectWithoutBilling() project {
	p := project{TeamID: uuid.New(), ProjectID: uuid.New(), Owner: s.signIn()}
	s.addMember(p.TeamID, p.Owner, "owner")
	s.consume(constants.ProjectCreated, fmt.Sprintf(`{"payload":{"project_id":%q,"team_id":%q,"name":"Shop","created_at":%q}}`,
		p.ProjectID, p.TeamID, time.Now().UTC().Format(time.RFC3339)))

	return p
}

func (s *storageSuite) addMember(teamID uuid.UUID, u user, role string) {
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"added_at":%q}}`,
		teamID, u.ID, role, time.Now().UTC().Format(time.RFC3339Nano)))
}

// member is a new User in the Team with the Role.
func (s *storageSuite) member(teamID uuid.UUID, role string) user {
	u := s.signIn()
	s.addMember(teamID, u, role)

	return u
}

func (s *storageSuite) setBilling(teamID uuid.UUID, status string, at time.Time) {
	s.consume(constants.TeamBillingStatusChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"status":%q,"changed_at":%q}}`,
		teamID, status, at.UTC().Format(time.RFC3339Nano)))
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

type response struct {
	Code int
	Body map[string]any
	List []map[string]any
	Raw  string
}

func serve(server *echo.Echo, method, path, body string, headers map[string]string) *response {
	var reader io.Reader
	if str.IsNotEmpty(body) {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	out := &response{Code: rec.Code, Raw: rec.Body.String()}
	trimmed := strings.TrimSpace(out.Raw)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		_ = json.Unmarshal(rec.Body.Bytes(), &out.Body)
	case strings.HasPrefix(trimmed, "["):
		_ = json.Unmarshal(rec.Body.Bytes(), &out.List)
	}

	return out
}

// manage is the web app calling the management API.
func (s *storageSuite) manage(method, path, body string, u user) *response {
	return serve(s.management, method, path, body, map[string]string{echo.HeaderAuthorization: "Bearer " + u.Token})
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

func (r *response) num(key string) float64 {
	v, _ := r.Body[key].(float64)

	return v
}

func (r *response) obj(key string) map[string]any {
	v, _ := r.Body[key].(map[string]any)

	return v
}

// expectError asserts the status and the rerror of a refusal.
func (s *storageSuite) expectError(r *response, status, domain, code int) {
	s.T().Helper()
	s.Require().Equal(status, r.Code, r.Raw)
	d, c := r.errorCode()
	s.Equal(domain, d, r.Raw)
	s.Equal(code, c, r.Raw)
}

// ---------------------------------------------------------------------------
// Flows
// ---------------------------------------------------------------------------

func projectPath(p project, rest string) string {
	return "/v1/projects/" + p.ProjectID.String() + "/storages" + rest
}

func storagePath(p project, id, rest string) string {
	return projectPath(p, "/"+id+rest)
}

func (s *storageSuite) open(p project, bucket, pkg string) *response {
	return s.manage(http.MethodPost, projectPath(p, ""), fmt.Sprintf(`{"bucket_name":%q,"region":"vno1","package":%q}`, bucket, pkg), p.Owner)
}

// opened is a Storage of the Project on the Package, open and usable.
func (s *storageSuite) opened(p project, bucket, pkg string) *response {
	r := s.open(p, bucket, pkg)
	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)

	return r
}

func (s *storageSuite) get(p project, id string) *response {
	r := s.manage(http.MethodGet, storagePath(p, id, ""), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)

	return r
}

func (s *storageSuite) choose(p project, id, code string) *response {
	return s.manage(http.MethodPut, storagePath(p, id, "/package"), fmt.Sprintf(`{"code":%q}`, code), p.Owner)
}

func (s *storageSuite) deleteStorage(p project, id, bucket string) *response {
	return s.manage(http.MethodDelete, storagePath(p, id, "?bucket_name="+bucket), "", p.Owner)
}

func (s *storageSuite) exec(sql string, args ...any) {
	_, err := s.pgPool.Exec(context.Background(), sql, args...)
	s.Require().NoError(err)
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

func (s *storageSuite) outboxPayloads(routingKey string) []map[string]any {
	opts := options.Find().SetSort(bson.M{"created_at": 1})
	cursor, err := s.mongoDB.Collection("outbox").Find(context.Background(), bson.M{"routing_key": routingKey}, opts)
	s.Require().NoError(err)
	defer func() { _ = cursor.Close(context.Background()) }()

	payloads := make([]map[string]any, 0)
	for cursor.Next(context.Background()) {
		var doc struct {
			Payload string `bson:"payload"`
		}
		s.Require().NoError(cursor.Decode(&doc))
		var event map[string]any
		s.Require().NoError(json.Unmarshal([]byte(doc.Payload), &event))
		payload, _ := event["payload"].(map[string]any)
		payloads = append(payloads, payload)
	}

	return payloads
}

// realtimeTypes are the types of the live messages published, in order.
func (s *storageSuite) realtimeTypes() []string {
	var out []string
	for _, payload := range s.outboxPayloads("RealtimeMessage") {
		out = append(out, payload["type"].(string))
	}

	return out
}

// sameInstant compares two RFC 3339 timestamps as instants.
func (s *storageSuite) sameInstant(expected, actual any) {
	s.T().Helper()
	e, err := time.Parse(time.RFC3339Nano, fmt.Sprint(expected))
	s.Require().NoError(err, expected)
	a, err := time.Parse(time.RFC3339Nano, fmt.Sprint(actual))
	s.Require().NoError(err, actual)
	s.True(e.Equal(a), "expected %s, got %s", e, a)
}
