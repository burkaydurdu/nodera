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

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
	"github.com/piggystats/nodera/contexts/billing/application/jobs"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/consumers"
	"github.com/piggystats/nodera/contexts/billing/pdf"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/providers/fake"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
)

const (
	testJWTSecret   = "billing-integration-test-jwt-secret"
	testLinksSecret = "billing-integration-test-links-secret"
	sessionKey      = "session:%s"
	frontURL        = "http://localhost:3002"
	gatewayURL      = "http://localhost:3460"

	// Valid identifiers: 10000000146 passes both TCKN check digits.
	validTCKN = "10000000146"
	validVKN  = "1234567890"
)

// noopPublisher lets the outbox accept writes without a broker: the suite
// observes events as outbox documents, never by relaying them.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string, string, any) error { return nil }

// clock is the controllable time every rule reads.
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

// rateFetcher stands in for the central bank: it answers the scripted
// bulletin and counts the calls.
type rateFetcher struct {
	mu    sync.Mutex
	next  *exchangerate.Rate
	err   error
	calls int
}

func (f *rateFetcher) Fetch(context.Context) (*exchangerate.Rate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := *f.next

	return &out, nil
}

func (f *rateFetcher) script(day time.Time, rate string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next = &exchangerate.Rate{RateDate: exchangerate.Day(day), USDBuying: mustDecimal(rate), Source: exchangerate.SourceTCMB}
	f.err = nil
}

func (f *rateFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

type billingSuite struct {
	suite.Suite
	pool              *dockertest.Pool
	postgresResource  *dockertest.Resource
	redisResource     *dockertest.Resource
	mongoResource     *dockertest.Resource
	gotenbergResource *dockertest.Resource

	// GotenbergURL is the PDF renderer, up and healthy for the Invoice
	// scenarios.
	GotenbergURL string

	server   *echo.Echo
	pgPool   *pgxpool.Pool
	mongoDB  *mongoDriver.Database
	redis    redis.IService
	rawRedis *goredis.Client
	fake     *fake.Fake
	renderer *pdf.Fake
	rates    *rateFetcher
	clock    *clock
	deps     app.Dependencies
	services *app.Services
	jobs     *jobs.Jobs
	consumer *consumers.CustomerEvents
	usage    *consumers.UsageEvents
	mail     *consumers.MailPackageEvents
	storage  *consumers.StoragePackageEvents
}

func TestBillingSuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(billingSuite))
}

func (s *billingSuite) SetupSuite() {
	var err error
	s.pool, err = dockertest.NewPool("")
	s.Require().NoError(err)
	s.Require().NoError(s.pool.Client.Ping())
	// Image pulls on a cold machine take longer than dockertest's default minute.
	s.pool.MaxWait = 3 * time.Minute

	s.postgresResource = s.run("postgres", "18", []string{"POSTGRES_USER=testuser", "POSTGRES_PASSWORD=testpassword", "POSTGRES_DB=testdb"})
	s.redisResource = s.run("redis", "8.2", nil)
	s.mongoResource = s.run("mongo", "8.2", nil)
	s.gotenbergResource = s.run("gotenberg/gotenberg", "8", nil)

	pgPort, _ := strconv.Atoi(s.postgresResource.GetPort("5432/tcp"))
	redisPort, _ := strconv.Atoi(s.redisResource.GetPort("6379/tcp"))
	mongoPort, _ := strconv.Atoi(s.mongoResource.GetPort("27017/tcp"))
	s.GotenbergURL = "http://127.0.0.1:" + s.gotenbergResource.GetPort("3000/tcp")
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

	s.Require().NoError(s.pool.Retry(func() error {
		res, err := http.Get(s.GotenbergURL + "/health")
		if err != nil {
			return err
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("gotenberg answered %d", res.StatusCode)
		}

		return nil
	}))

	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/billing/db/migrations", "schema_migrations"))
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/billing/db/seed", "schema_seeds"))

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

	outboxInstance, err := outbox.New(s.mongoDB.Client(), s.mongoDB, noopPublisher{}, log, "correlationID")
	s.Require().NoError(err)

	// The fake stands in for the seeded routing code, so the seed applies.
	s.fake = fake.New(fake.DefaultCode)
	// The fake renderer stands in for Gotenberg except in the PDF scenario.
	s.renderer = pdf.NewFake()
	s.rates = &rateFetcher{}
	s.clock = &clock{now: time.Now().UTC()}

	s.deps = app.Dependencies{
		Log: log,
		Conf: &config.Config{
			Environment: "dev",
			Server:      config.Server{Name: "nodera-billing-test", Port: ":0"},
			Secret:      config.Secret{JWT: testJWTSecret, Links: testLinksSecret},
			Domain:      config.Domain{Front: frontURL + "/", Gateway: gatewayURL + "/"},
			// Mid-September, so a scenario can bill a Period the cutover cuts.
			Billing: config.Billing{StartAt: "2026-09-15T00:00:00Z"},
			PDF:     config.PDF{GotenbergURL: s.GotenbergURL},
			Storage: config.Storage{LocalDir: s.T().TempDir()},
		},
		Pool:        s.pgPool,
		Redis:       s.redis,
		Outbox:      outboxInstance,
		Registry:    providers.NewRegistry(s.fake),
		RateFetcher: s.rates,
		Clock:       s.clock.Now,
		Renderer:    s.renderer,
	}

	const unlimited = 1_000_000
	s.server = app.BuildServer(s.deps, unlimited)
	s.services = app.NewServices(s.pgPool)
	s.jobs = app.BuildJobs(s.deps)
	s.consumer = app.BuildCustomerConsumer(s.deps)
	s.usage = app.BuildUsageConsumer(s.deps)
	s.mail = app.BuildMailPackageConsumer(s.deps)
	s.storage = app.BuildStoragePackageConsumer(s.deps)
}

func (s *billingSuite) TearDownSuite() {
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource, s.mongoResource, s.gotenbergResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

func (s *billingSuite) SetupTest() {
	s.fake.Reset()
	s.renderer.Reset()
	s.clock.Set(time.Now().UTC())
}

// TearDownTest keeps the seeded routes and tax rules and clears the rest.
func (s *billingSuite) TearDownTest() {
	ctx := context.Background()
	for _, table := range []string{
		"payments", "credit_ledger", "invoice_tax_lines", "invoice_lines", "invoices", "invoice_numbers", "usage", "mail_package_intervals",
		"storage_package_intervals",
		"card_verifications", "payment_methods", "billing_profiles", "team_billing_status",
		"team_members", "teams", "exchange_rates", "webhook_events",
	} {
		_, _ = s.pgPool.Exec(ctx, `DELETE FROM `+table)
	}
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM tax_rules WHERE label_key NOT IN ('tax.kdv_20', 'tax.export_service_exemption')`)
	_, _ = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	s.deleteByPattern("session:*")
	s.deleteByPattern("ratelimit:*")
}

func (s *billingSuite) run(repository, tag string, env []string) *dockertest.Resource {
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

func (s *billingSuite) deleteByPattern(pattern string) {
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
// Sessions and the access read model
// ---------------------------------------------------------------------------

// user is a signed-in customer as the billing service sees one.
type user struct {
	ID    uuid.UUID
	Email string
	Token string
}

// signIn mints an access token exactly as the customer service would, with
// the teams and pv claims, and stores the session record it points at.
func (s *billingSuite) signIn() user {
	userID := uuid.New()
	sessionID := uuid.New()
	email := userID.String() + "@example.com"
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "nodera-customer", "sub": userID.String(), "iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
		"typ": "access", "sid": sessionID.String(), "email": email, "status": "active",
		"teams": map[string]string{}, "pv": 0,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	s.Require().NoError(err)
	s.Require().NoError(s.rawRedis.Set(context.Background(), fmt.Sprintf(sessionKey, sessionID), "1", time.Hour).Err())

	return user{ID: userID, Email: email, Token: token}
}

func (s *billingSuite) consume(routingKey, payload string) {
	s.Require().NoError(s.consumer.Consume(&rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload)}))
}

func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

// team replays the customer events that make a Team known: TeamCreated with
// the owner's details and the owner's own TeamMemberAdded.
func (s *billingSuite) team(owner user) uuid.UUID {
	teamID := uuid.New()
	now := stamp(time.Now())
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"name":"Acme","owner_email":%q,"owner_full_name":"Owner Person","owner_locale":"tr","created_at":%q}}`,
		teamID, owner.ID, owner.Email, now))
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":"owner","email":%q,"full_name":"Owner Person","locale":"tr","added_at":%q}}`,
		teamID, owner.ID, owner.Email, now))

	return teamID
}

func (s *billingSuite) addMember(teamID uuid.UUID, member user, role string, at time.Time) {
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"email":%q,"full_name":"Member","locale":"en","added_at":%q}}`,
		teamID, member.ID, role, member.Email, stamp(at)))
}

func (s *billingSuite) removeMember(teamID uuid.UUID, member user, at time.Time) {
	s.consume(constants.TeamMemberRemoved, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"removed_at":%q}}`, teamID, member.ID, stamp(at)))
}

func (s *billingSuite) changeRole(teamID uuid.UUID, member user, role string, at time.Time) {
	s.consume(constants.TeamMemberRoleChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"changed_at":%q}}`, teamID, member.ID, role, stamp(at)))
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

func (s *billingSuite) request(method, path, body, token string) *response {
	return s.requestWith(method, path, body, token, nil)
}

func (s *billingSuite) requestWith(method, path, body, token string, headers http.Header) *response {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	for k, v := range headers {
		req.Header[k] = v
	}
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

func (s *billingSuite) get(path, token string) *response {
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

// assertError holds the exact status and the {domain, code} of the rerror.
func (s *billingSuite) assertError(res *response, status, domain, code int) {
	s.T().Helper()
	s.Equal(status, res.Code, res.Raw)
	d, c := res.errorCode()
	s.Equal(domain, d, res.Raw)
	s.Equal(code, c, res.Raw)
}

func billingPath(teamID uuid.UUID, rest string) string {
	return "/v1/teams/" + teamID.String() + "/billing" + rest
}

// ---------------------------------------------------------------------------
// Flow helpers
// ---------------------------------------------------------------------------

func trCompany() string {
	return `{"account_type":"company","legal_name":"Acme Bilişim A.Ş.","country":"TR","address_line1":"Moda Cad. 1","city":"İstanbul",
		"postal_code":"34710","tax_id":"` + validVKN + `","tax_office":"Kadıköy","invoice_email":"Muhasebe@Acme.example"}`
}

func deCompany() string {
	return `{"account_type":"company","legal_name":"Acme GmbH","country":"DE","address_line1":"Hauptstr. 1","city":"Berlin",
		"postal_code":"10115","vat_id":"de123456789","invoice_email":"books@acme.example"}`
}

func (s *billingSuite) putProfile(teamID uuid.UUID, token, body string) *response {
	return s.request(http.MethodPut, billingPath(teamID, "/profile"), body, token)
}

func (s *billingSuite) startVerification(teamID uuid.UUID, token string) *response {
	return s.request(http.MethodPost, billingPath(teamID, "/payment-methods/verify"), "", token)
}

func (s *billingSuite) completeVerification(teamID uuid.UUID, token, verificationID string) *response {
	return s.request(http.MethodPost, billingPath(teamID, "/payment-methods/verify/complete"),
		fmt.Sprintf(`{"verification_id":%q,"callback":{"status":"ok"}}`, verificationID), token)
}

// addCard runs the hosted verification end to end and answers the saved card.
func (s *billingSuite) addCard(teamID uuid.UUID, token string) *response {
	started := s.startVerification(teamID, token)
	s.Require().Equal(http.StatusCreated, started.Code, started.Raw)
	completed := s.completeVerification(teamID, token, started.str("verification_id"))
	s.Require().Equal(http.StatusCreated, completed.Code, completed.Raw)

	return completed
}

// payingTeam is a Team whose owner saved a Turkish company profile.
func (s *billingSuite) payingTeam() (user, uuid.UUID) {
	owner := s.signIn()
	teamID := s.team(owner)
	res := s.putProfile(teamID, owner.Token, trCompany())
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return owner, teamID
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

// outboxPayloads answers the payloads published under routingKey, oldest first.
func (s *billingSuite) outboxPayloads(routingKey string) []map[string]any {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
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

// statusEvents answers the statuses TeamBillingStatusChanged carried for a
// Team, in publication order.
func (s *billingSuite) statusEvents(teamID uuid.UUID) []string {
	out := make([]string, 0)
	for _, p := range s.outboxPayloads(constants.TeamBillingStatusChanged) {
		if p["team_id"] == teamID.String() {
			status, _ := p["status"].(string)
			out = append(out, status)
		}
	}

	return out
}
