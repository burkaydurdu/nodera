package integrationtest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gosmtp "github.com/emersion/go-smtp"
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

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/application/jobs"
	"github.com/piggystats/nodera/contexts/mail/async/constants"
	"github.com/piggystats/nodera/contexts/mail/async/consumers"
	"github.com/piggystats/nodera/contexts/mail/mta/fake"
	"github.com/piggystats/nodera/contexts/mail/smtp"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/secrets"
	"github.com/piggystats/nodera/utils/str"
)

const (
	testJWTSecret     = "mail-integration-test-jwt-secret"
	testInternalToken = "mail-integration-test-internal-token"
	testZone          = "mta.nodera.test"
	sessionKey        = "session:%s"
	base62            = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
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

// resolver is the DNS the Sending domains are checked against.
type resolver struct {
	mu     sync.Mutex
	cnames map[string]string
	txts   map[string][]string
	down   bool
}

func newResolver() *resolver {
	return &resolver{cnames: map[string]string{}, txts: map[string][]string{}}
}

func (r *resolver) LookupCNAME(_ context.Context, host string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.down {
		return "", &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
	}
	if target, ok := r.cnames[strings.ToLower(strings.TrimSuffix(host, "."))]; ok {
		return target + ".", nil
	}

	return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (r *resolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.down {
		return nil, &net.DNSError{Err: "i/o timeout", Name: name, IsTimeout: true}
	}
	if records, ok := r.txts[strings.ToLower(strings.TrimSuffix(name, "."))]; ok {
		return records, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// fail makes every lookup go unanswered, as a resolver timing out or a
// nameserver answering SERVFAIL does.
func (r *resolver) fail(down bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = down
}

func (r *resolver) setCNAME(host, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cnames[strings.ToLower(host)] = strings.ToLower(target)
}

func (r *resolver) removeCNAME(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cnames, strings.ToLower(host))
}

func (r *resolver) setTXT(name string, records ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.txts[strings.ToLower(name)] = records
}

func (r *resolver) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cnames = map[string]string{}
	r.txts = map[string][]string{}
}

// receiver is a Team's Webhook endpoint.
type receiver struct {
	mu       sync.Mutex
	server   *httptest.Server
	status   int
	requests []received
}

type received struct {
	Header http.Header
	Body   []byte
}

func newReceiver() *receiver {
	r := &receiver{status: http.StatusOK}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, received{Header: req.Header.Clone(), Body: body})
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
	}))

	return r
}

func (r *receiver) answer(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

func (r *receiver) received() []received {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]received(nil), r.requests...)
}

func (r *receiver) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = http.StatusOK
	r.requests = nil
}

type mailSuite struct {
	suite.Suite
	pool             *dockertest.Pool
	postgresResource *dockertest.Resource
	redisResource    *dockertest.Resource
	mongoResource    *dockertest.Resource

	pgPool   *pgxpool.Pool
	mongoDB  *mongoDriver.Database
	rawRedis *goredis.Client

	clock    *clock
	mta      *fake.Fake
	resolver *resolver
	hook     *receiver
	sealer   *secrets.Sealer

	deps       app.Deps
	handlers   *app.Handlers
	management *echo.Echo
	sending    *echo.Echo
	internal   *echo.Echo
	jobs       *jobs.Jobs
	customer   *consumers.CustomerEvents
	billing    *consumers.BillingEvents
	smtpAddr   string
	smtpServer *gosmtp.Server
}

func TestMailSuite(t *testing.T) {
	if str.IsEmpty(os.Getenv("INTEGRATION")) {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(mailSuite))
}

func (s *mailSuite) SetupSuite() {
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

	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/mail/db/migrations", "schema_migrations"))
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/mail/db/seed", "schema_seeds"))

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
	s.sealer, err = secrets.NewSealer(key)
	s.Require().NoError(err)

	s.clock = &clock{now: start}
	s.mta = fake.New()
	s.resolver = newResolver()
	s.hook = newReceiver()

	conf := &config.Config{
		Environment: "test",
		Server:      config.Server{Name: "nodera-mail-test"},
		DNS:         config.DNS{Zone: testZone},
		Internal:    config.Internal{Token: testInternalToken},
		Secret:      config.Secret{JWT: testJWTSecret},
		Webhooks:    config.Webhooks{AllowLocal: true},
	}
	returnPathKey := make([]byte, 32)
	_, _ = rand.Read(returnPathKey)
	deps := app.Deps{
		Log: log, Conf: conf, Pool: s.pgPool, Redis: redis.NewService(redisClient), Outbox: outboxInstance,
		MTA: s.mta, Resolver: s.resolver, Sealer: s.sealer, ReturnPaths: message.NewReturnPaths(returnPathKey), Clock: s.clock.Now,
	}

	const unlimited = 1_000_000
	s.deps = deps
	s.handlers = app.NewHandlers(deps)
	s.management = app.BuildManagement(deps, s.handlers, unlimited)
	s.sending = app.BuildSending(deps, s.handlers)
	s.internal = app.BuildInternal(deps, s.handlers)
	s.jobs = app.BuildJobs(deps, s.handlers)
	s.customer = app.BuildCustomerConsumer(s.handlers)
	s.billing = app.BuildBillingConsumer(s.handlers)
	s.startSMTP(deps)
}

// startSMTP serves the relay on a real socket with a throwaway certificate,
// so the scenarios speak SMTP the way a client does.
func (s *mailSuite) startSMTP(deps app.Deps) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)

	s.smtpServer = gosmtp.NewServer(app.BuildSMTPBackend(deps, s.handlers))
	smtp.Configure(s.smtpServer, "localhost", 10<<20)
	s.smtpServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{selfSigned(s.T())}, MinVersion: tls.VersionTLS12}
	s.smtpAddr = listener.Addr().String()

	go func() { _ = s.smtpServer.Serve(listener) }()
}

func (s *mailSuite) TearDownSuite() {
	if s.smtpServer != nil {
		_ = s.smtpServer.Close()
	}
	if s.hook != nil {
		s.hook.server.Close()
	}
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource, s.mongoResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

func (s *mailSuite) TearDownTest() {
	ctx := context.Background()
	for _, table := range []string{
		"webhook_events", "webhook_deliveries", "webhooks", "suppressions", "message_spool", "message_events",
		"message_recipients", "messages", "mail_days", "project_limits", "mail_periods", "project_packages",
		"dkim_keys", "sending_domains", "team_billing", "team_access", "project_access",
	} {
		_, err := s.pgPool.Exec(ctx, "DELETE FROM "+table)
		s.Require().NoError(err)
	}
	_, _ = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	_ = s.rawRedis.FlushDB(ctx).Err()
	s.mta.Reset()
	s.resolver.reset()
	s.hook.reset()
	s.clock.Set(start)
}

func (s *mailSuite) run(repository, tag string, env []string) *dockertest.Resource {
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

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
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
func (s *mailSuite) signIn() user {
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

func (s *mailSuite) consume(routingKey, payload string) {
	s.Require().NoError(s.consumeErr(routingKey, payload))
}

func (s *mailSuite) consumeErr(routingKey, payload string) error {
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

// grantProject replays what makes a Project usable for Mail: the owner's
// Team and membership and the named Project from the customer context, and
// a payment method from the billing context.
func (s *mailSuite) grantProject() project {
	p := s.grantProjectWithoutBilling()
	s.setBilling(p.TeamID, "active", time.Now())

	return p
}

func (s *mailSuite) grantProjectWithoutBilling() project {
	p := project{TeamID: uuid.New(), ProjectID: uuid.New(), Owner: s.signIn()}
	s.addMember(p.TeamID, p.Owner, "owner")
	s.consume(constants.ProjectCreated, fmt.Sprintf(`{"payload":{"project_id":%q,"team_id":%q,"name":"Shop","created_at":%q}}`,
		p.ProjectID, p.TeamID, time.Now().UTC().Format(time.RFC3339)))

	return p
}

func (s *mailSuite) addMember(teamID uuid.UUID, u user, role string) {
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"added_at":%q}}`,
		teamID, u.ID, role, time.Now().UTC().Format(time.RFC3339Nano)))
}

// member is a new User in the Team with the Role.
func (s *mailSuite) member(teamID uuid.UUID, role string) user {
	u := s.signIn()
	s.addMember(teamID, u, role)

	return u
}

func (s *mailSuite) setBilling(teamID uuid.UUID, status string, at time.Time) {
	s.consume(constants.TeamBillingStatusChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"status":%q,"changed_at":%q}}`,
		teamID, status, at.UTC().Format(time.RFC3339Nano)))
}

// apiKey is a key the customer context created: its Redis entry, and the
// whole key only the Team saw.
func (s *mailSuite) apiKey(p project, permissions ...string) string {
	if len(permissions) == 0 {
		permissions = []string{"mail.send"}
	}

	id, secret := randomBase62(12), randomBase62(43)
	sum := sha256.Sum256([]byte(secret))
	entry, err := json.Marshal(map[string]any{"hash": hex.EncodeToString(sum[:]), "project_id": p.ProjectID, "team_id": p.TeamID, "permissions": permissions})
	s.Require().NoError(err)
	s.Require().NoError(s.rawRedis.Set(context.Background(), "api_key:"+id, string(entry), 0).Err())

	return "nd_" + id + "." + secret
}

func (s *mailSuite) revokeKey(key string) {
	id := strings.TrimPrefix(strings.SplitN(key, ".", 2)[0], "nd_")
	s.Require().NoError(s.rawRedis.Del(context.Background(), "api_key:"+id).Err())
}

func randomBase62(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = base62[int(b[i])%len(base62)]
	}

	return string(b)
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

type response struct {
	Code int
	Body map[string]any
	List []map[string]any
	Raw  string
	Hdr  http.Header
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

// manage is the web app calling the management API.
func (s *mailSuite) manage(method, path, body string, u user) *response {
	return serve(s.management, method, path, body, map[string]string{echo.HeaderAuthorization: "Bearer " + u.Token})
}

func (s *mailSuite) send(key, body string, headers ...string) *response {
	h := map[string]string{echo.HeaderAuthorization: "Bearer " + key}
	for i := 0; i+1 < len(headers); i += 2 {
		h[headers[i]] = headers[i+1]
	}

	return serve(s.sending, http.MethodPost, "/v1/mail/messages", body, h)
}

// sendTo is a plain Message from the Sending domain to the recipients.
func (s *mailSuite) sendTo(key, from string, to ...string) *response {
	recipients := make([]string, len(to))
	for i, r := range to {
		recipients[i] = fmt.Sprintf(`{"email":%q}`, r)
	}

	return s.send(key, fmt.Sprintf(`{"from":{"email":%q,"name":"Shop"},"to":[%s],"subject":"Your code","text":"123456","tags":["auth"]}`,
		from, strings.Join(recipients, ",")))
}

func (s *mailSuite) mtaHook(method, path, body, token string) *response {
	return serve(s.internal, method, path, body, map[string]string{echo.HeaderAuthorization: "Bearer " + token})
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

func (r *response) obj(key string) map[string]any {
	v, _ := r.Body[key].(map[string]any)

	return v
}

func (r *response) items() []map[string]any {
	raw, _ := r.Body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, m)
	}

	return out
}

// expectError asserts the status and the rerror of a refusal.
func (s *mailSuite) expectError(r *response, status, domain, code int) {
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
	return "/v1/projects/" + p.ProjectID.String() + "/mail" + rest
}

// addDomain adds a Sending domain and answers its body.
func (s *mailSuite) addDomain(p project, name string) *response {
	r := s.manage(http.MethodPost, projectPath(p, "/domains"), fmt.Sprintf(`{"name":%q}`, name), p.Owner)
	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)

	return r
}

// publish puts a domain's three records in DNS, as the Team would.
func (s *mailSuite) publish(domainBody map[string]any) {
	records, _ := domainBody["records"].([]any)
	s.Require().Len(records, 3)
	for _, raw := range records {
		record := raw.(map[string]any)
		s.resolver.setCNAME(record["host"].(string), record["value"].(string))
	}
}

// verifiedDomain is a Sending domain whose records are published and checked.
func (s *mailSuite) verifiedDomain(p project, name string) string {
	added := s.addDomain(p, name)
	s.publish(added.Body)
	verified := s.manage(http.MethodPost, projectPath(p, "/domains/"+added.str("id")+"/verify"), "", p.Owner)
	s.Require().Equal(http.StatusOK, verified.Code, verified.Raw)
	s.Require().Equal("verified", verified.str("status"), verified.Raw)

	return added.str("id")
}

func (s *mailSuite) choose(p project, code string) *response {
	return s.manage(http.MethodPut, projectPath(p, "/package"), fmt.Sprintf(`{"code":%q}`, code), p.Owner)
}

// ready is a Project that can send: a verified example.com, the 10k Package
// and a sending key.
func (s *mailSuite) ready() (project, string) {
	p := s.grantProject()
	s.verifiedDomain(p, "example.com")
	chosen := s.choose(p, "10k")
	s.Require().Equal(http.StatusOK, chosen.Code, chosen.Raw)

	return p, s.apiKey(p)
}

func (s *mailSuite) overview(p project) *response {
	r := s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)

	return r
}

func (s *mailSuite) exec(sql string, args ...any) {
	_, err := s.pgPool.Exec(context.Background(), sql, args...)
	s.Require().NoError(err)
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

func (s *mailSuite) outboxPayloads(routingKey string) []map[string]any {
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
func (s *mailSuite) realtimeTypes() []string {
	var out []string
	for _, payload := range s.outboxPayloads("RealtimeMessage") {
		out = append(out, payload["type"].(string))
	}

	return out
}

// sameInstant compares two RFC 3339 timestamps as instants.
func (s *mailSuite) sameInstant(expected, actual any) {
	e, err := time.Parse(time.RFC3339Nano, fmt.Sprint(expected))
	s.Require().NoError(err, expected)
	a, err := time.Parse(time.RFC3339Nano, fmt.Sprint(actual))
	s.Require().NoError(err, actual)
	s.True(e.Equal(a), "expected %s, got %s", e, a)
}
