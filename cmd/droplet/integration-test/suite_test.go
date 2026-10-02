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

	"github.com/piggystats/nodera/cmd/droplet/app"
	"github.com/piggystats/nodera/cmd/droplet/config"
	"github.com/piggystats/nodera/contexts/droplet/application/jobs"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/consumers"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/contexts/droplet/providers/fake"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
)

const (
	testJWTSecret = "droplet-integration-test-jwt-secret"
	sessionKey    = "session:%s"

	// The fake stands in for the seeded Provider, so the seed's mappings apply.
	providerCode = "cherry"
	regionLT     = "LT-Siauliai"
	regionNL     = "NL-Amsterdam"
	regionDE     = "DE-Frankfurt"
	regionSE     = "SE-Stockholm"
	regionSG     = "SG-Singapore"
	regionJP     = "JP-Tokyo"

	// A throwaway ed25519 public key; no private half exists.
	testKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGpLYgdo0jRVvaW3cq4ZYq2C1a6D3kXAd0Y4r8p3gf1S test@nodera"
	testKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIC1XxvJ0m2aH4b7vK9fXfM0wq3Ycp8rZ4QbQyJ8oV2mN second@nodera"
)

// noopPublisher lets the outbox accept writes without a broker: the suite
// observes the meter's events as outbox documents, never by relaying them.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string, string, any) error { return nil }

type dropletSuite struct {
	suite.Suite
	pool             *dockertest.Pool
	postgresResource *dockertest.Resource
	redisResource    *dockertest.Resource
	mongoResource    *dockertest.Resource

	server   *echo.Echo
	pgPool   *pgxpool.Pool
	mongoDB  *mongoDriver.Database
	redis    redis.IService
	rawRedis *goredis.Client
	fake     *fake.Fake
	registry *providers.Registry
	outbox   *outbox.Outbox
	jobs     *jobs.Jobs
	customer *consumers.CustomerEvents
	billing  *consumers.BillingEvents
}

func TestDropletSuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(dropletSuite))
}

func (s *dropletSuite) SetupSuite() {
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

	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/droplet/db/migrations", "schema_migrations"))
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/droplet/db/seed", "schema_seeds"))

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
	s.redis = redis.NewService(redisClient)

	s.fake = fake.New(providerCode)
	s.registry = providers.NewRegistry(s.fake)

	outboxInstance, err := outbox.New(s.mongoDB.Client(), s.mongoDB, noopPublisher{}, log, "correlationID")
	s.Require().NoError(err)
	s.outbox = outboxInstance

	conf := &config.Config{
		Environment: "dev",
		Server:      config.Server{Name: "nodera-droplet-test", Port: ":0"},
		Secret:      config.Secret{JWT: testJWTSecret},
	}

	const unlimited = 1_000_000
	s.server = app.BuildServer(log, conf, s.pgPool, s.redis, s.registry, outboxInstance, unlimited)
	s.jobs = app.BuildJobs(log, s.pgPool, s.registry, outboxInstance)
	s.customer = app.BuildCustomerConsumer(s.pgPool)
	s.billing = app.BuildBillingConsumer(log, s.pgPool, s.registry, outboxInstance)
}

func (s *dropletSuite) TearDownSuite() {
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource, s.mongoResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

// SetupTest scripts the fake to match the seeded catalog and syncs once, so
// every scenario starts from the same priced, in-stock catalog.
func (s *dropletSuite) SetupTest() {
	s.fake.Reset()
	s.scriptCatalog()
	s.Require().NoError(s.jobs.SyncCatalog(context.Background()))
}

func (s *dropletSuite) TearDownTest() {
	ctx := context.Background()
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM droplet_status_history`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM droplets`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM project_access`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM team_access`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM team_billing`)
	_, _ = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	s.deleteByPattern("session:*")
	s.deleteByPattern("ratelimit:*")
}

// scriptCatalog gives the fake every seeded Offer with stock, at prices the
// scenarios assert on: the spec's 0.41 example backs c4-32.
func (s *dropletSuite) scriptCatalog() {
	for _, r := range [][3]string{
		{regionLT, "Lithuania, Šiauliai", "LT"}, {regionNL, "Netherlands, Amsterdam", "NL"}, {regionDE, "Germany, Frankfurt", "DE"},
		{regionSE, "Sweden, Stockholm", "SE"}, {regionSG, "Singapore", "SG"}, {regionJP, "Japan, Tokyo", "JP"},
	} {
		s.fake.AddRegion(r[0], r[1], r[2])
	}
	for _, i := range [][2]string{{"ubuntu_24_04_64bit", "Ubuntu 24.04"}, {"ubuntu_22_04", "Ubuntu 22.04"}, {"debian_12_64bit", "Debian 12"}} {
		s.fake.AddImage(i[0], i[1])
	}

	s.fake.SetOffer("e3-1240v3", regionLT, "0.098", 1)
	s.fake.SetOffer("e5-1620v4", regionLT, "0.41", 5)
	s.fake.SetOffer("e5-1620v4", regionNL, "0.41", 5)
	s.fake.SetOffer("amd-ryzen-7700x", regionLT, "0.318", 3)
	s.fake.SetOffer("amd-ryzen-7700x", regionNL, "0.318", 3)
	s.fake.SetOffer("amd-ryzen-7700x", regionSE, "0.318", 3)
	s.fake.SetOffer("amd-ryzen-7700x", regionDE, "0.318", 2)
	s.fake.SetOffer("amd-epyc-9124p", regionNL, "0.614", 1)
	s.fake.SetOffer("amd-epyc-9124p", regionSG, "0.614", 1)
}

func (s *dropletSuite) run(repository, tag string, env []string) *dockertest.Resource {
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

func (s *dropletSuite) deleteByPattern(pattern string) {
	ctx := context.Background()
	iter := s.rawRedis.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if len(keys) > 0 {
		_ = s.rawRedis.Del(ctx, keys...).Err()
	}
}

// ---------------------------------------------------------------------------
// Sessions and access
// ---------------------------------------------------------------------------

// user is a signed-in customer as the droplet service sees one: a user id and
// an access token whose session is alive in Redis.
type user struct {
	ID    uuid.UUID
	Token string
}

// signIn mints an access token exactly as the customer service would and
// stores the session record it points at.
func (s *dropletSuite) signIn() user {
	userID := uuid.New()
	sessionID := uuid.New()
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

func (s *dropletSuite) consume(routingKey, payload string) {
	s.Require().NoError(s.consumeErr(routingKey, payload))
}

// consumeErr hands the message to the consumer whose queue it would land on.
func (s *dropletSuite) consumeErr(routingKey, payload string) error {
	message := &rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload), CorrelationID: uuid.NewString()}
	switch routingKey {
	case constants.TeamBillingStatusChanged, constants.TeamDropletsDeletionRequested:
		return s.billing.Consume(message)
	default:
		return s.customer.Consume(message)
	}
}

// grantProject replays the events that make a Project usable: the owner's
// Team, their membership and the Project from the customer context, and the
// Team's payment method from the billing context.
func (s *dropletSuite) grantProject(owner user) (teamID, projectID uuid.UUID) {
	teamID, projectID = s.grantProjectWithoutBilling(owner)
	s.setBilling(teamID, "active", time.Now())

	return teamID, projectID
}

// grantProjectWithoutBilling is a Project whose Team the billing context has
// said nothing about yet.
func (s *dropletSuite) grantProjectWithoutBilling(owner user) (teamID, projectID uuid.UUID) {
	teamID, projectID = uuid.New(), uuid.New()
	now := time.Now().UTC().Format(time.RFC3339)
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"created_at":%q}}`, teamID, owner.ID, now))
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":"owner","added_at":%q}}`, teamID, owner.ID, now))
	s.consume(constants.ProjectCreated, fmt.Sprintf(`{"payload":{"project_id":%q,"team_id":%q,"created_at":%q}}`, projectID, teamID, now))

	return teamID, projectID
}

// setBilling is the billing context announcing a Team's status as of at.
func (s *dropletSuite) setBilling(teamID uuid.UUID, status string, at time.Time) {
	s.consume(constants.TeamBillingStatusChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"status":%q,"changed_at":%q}}`, teamID, status, at.UTC().Format(time.RFC3339Nano)))
}

func (s *dropletSuite) requestTeamDeletion(teamID uuid.UUID) {
	s.consume(constants.TeamDropletsDeletionRequested, fmt.Sprintf(`{"payload":{"team_id":%q,"requested_at":%q}}`, teamID, time.Now().UTC().Format(time.RFC3339Nano)))
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

func (s *dropletSuite) request(method, path, body, token string) *response {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
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

func (s *dropletSuite) get(path, token string) *response {
	return s.request(http.MethodGet, path, "", token)
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

// item finds the list entry whose key equals value.
func (r *response) item(key, value string) map[string]any {
	for _, entry := range r.List {
		if entry[key] == value {
			return entry
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Flow helpers
// ---------------------------------------------------------------------------

func createBody(hostname string, keys ...string) string {
	if len(keys) == 0 {
		keys = []string{testKey}
	}
	parts := make([]string, 0, len(keys))
	for i, k := range keys {
		parts = append(parts, fmt.Sprintf(`{"name":"key-%d","public_key":%q}`, i+1, k))
	}

	return fmt.Sprintf(`{"plan":"c4-32","region":"vno1","image":"ubuntu-24-04","hostname":%q,"ssh_keys":[%s]}`, hostname, strings.Join(parts, ","))
}

func (s *dropletSuite) createDroplet(u user, projectID uuid.UUID, hostname string) *response {
	return s.request(http.MethodPost, "/v1/projects/"+projectID.String()+"/droplets", createBody(hostname), u.Token)
}

// activeDroplet creates a Droplet and walks it to active through the fake
// and one poll, answering its id.
func (s *dropletSuite) activeDroplet(u user, projectID uuid.UUID, hostname string) string {
	created := s.createDroplet(u, projectID, hostname)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.fake.Advance()
	s.Require().NoError(s.jobs.PollDroplets(context.Background()))

	detail := s.get("/v1/droplets/"+created.str("id"), u.Token)
	s.Require().Equal("active", detail.str("status"), detail.Raw)

	return created.str("id")
}

// providerRef reads the supplier-side reference straight from the row: it is
// never part of a customer-facing answer.
func (s *dropletSuite) providerRef(dropletID string) string {
	var ref *string
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT provider_ref FROM droplets WHERE id = $1`, dropletID).Scan(&ref))
	s.Require().NotNil(ref, "the Provider was never asked")

	return *ref
}

func (s *dropletSuite) poll() {
	s.Require().NoError(s.jobs.PollDroplets(context.Background()))
}

func (s *dropletSuite) sync() {
	s.Require().NoError(s.jobs.SyncCatalog(context.Background()))
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

func (s *dropletSuite) outboxCount(routingKey string) int64 {
	n, err := s.mongoDB.Collection("outbox").CountDocuments(context.Background(), bson.M{"routing_key": routingKey})
	s.Require().NoError(err)

	return n
}

// outboxPayloads answers the payload of every document under routingKey, in
// the order they were written.
func (s *dropletSuite) outboxPayloads(routingKey string) []map[string]any {
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

// outboxPayload answers the one payload under routingKey for the Droplet.
func (s *dropletSuite) outboxPayload(routingKey, dropletID string) map[string]any {
	var found map[string]any
	for _, payload := range s.outboxPayloads(routingKey) {
		if payload["droplet_id"] == dropletID {
			s.Require().Nil(found, "%s was published more than once for %s", routingKey, dropletID)
			found = payload
		}
	}
	s.Require().NotNil(found, "%s was not published for %s", routingKey, dropletID)

	return found
}

func (s *dropletSuite) clearOutbox() {
	_, err := s.mongoDB.Collection("outbox").DeleteMany(context.Background(), bson.M{})
	s.Require().NoError(err)
}

// sameInstant compares two RFC 3339 timestamps as instants; the API answers
// in the database's zone, the events in UTC.
func (s *dropletSuite) sameInstant(expected, actual any) {
	e, err := time.Parse(time.RFC3339Nano, fmt.Sprint(expected))
	s.Require().NoError(err, expected)
	a, err := time.Parse(time.RFC3339Nano, fmt.Sprint(actual))
	s.Require().NoError(err, actual)
	s.True(e.Equal(a), "expected %s, got %s", e, a)
}

// isRFC3339UTC holds the contract's timestamp shape: RFC 3339 in UTC.
func (s *dropletSuite) isRFC3339UTC(value any) {
	raw, _ := value.(string)
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	s.Require().NoError(err, raw)
	s.True(strings.HasSuffix(raw, "Z"), "timestamp is not UTC: %s", raw)
	s.False(parsed.IsZero())
}
