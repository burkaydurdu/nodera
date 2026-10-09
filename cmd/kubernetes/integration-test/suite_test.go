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

	"github.com/piggystats/nodera/cmd/kubernetes/app"
	"github.com/piggystats/nodera/cmd/kubernetes/config"
	"github.com/piggystats/nodera/contexts/kubernetes/application/jobs"
	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
	"github.com/piggystats/nodera/contexts/kubernetes/async/consumers"
	"github.com/piggystats/nodera/contexts/kubernetes/engine/fake"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/utils/str"
)

const (
	testJWTSecret = "kubernetes-integration-test-jwt-secret"
	sessionKey    = "session:%s"
	region        = "vno1"
	domain        = "k8s.nodera.test"
)

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

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Set(t time.Time)         { c.mu.Lock(); defer c.mu.Unlock(); c.now = t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type kubernetesSuite struct {
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
	droplet    *consumers.DropletEvents
}

func TestKubernetesSuite(t *testing.T) {
	if str.IsEmpty(os.Getenv("INTEGRATION")) {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(kubernetesSuite))
}

func (s *kubernetesSuite) SetupSuite() {
	var err error
	s.pool, err = dockertest.NewPool("")
	s.Require().NoError(err)
	s.Require().NoError(s.pool.Client.Ping())
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

	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/kubernetes/db/migrations", "schema_migrations"))
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/kubernetes/db/seed", "schema_seeds"))

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

	s.clock = &clock{now: start}
	s.engine = fake.New()

	conf := &config.Config{
		Environment: "test",
		Server:      config.Server{Name: "nodera-kubernetes-test"},
		// Room for three Clusters in the Region, two ports each.
		Engine: config.Engine{Kind: "fake", Region: region, EndpointDomain: domain, FirstEndpointPort: 20000, LastEndpointPort: 20005},
		Nodes:  config.Nodes{Image: "ubuntu-24-04"},
		Limits: config.Limits{ClustersPerProject: 2, PoolsPerCluster: 2, NodesPerPool: 5},
		Secret: config.Secret{JWT: testJWTSecret},
	}
	deps := app.Deps{
		Log: log, Conf: conf, Pool: s.pgPool, Redis: redis.NewService(redisClient), Outbox: outboxInstance,
		Engine: s.engine, Clock: s.clock.Now,
	}

	const unlimited = 1_000_000
	s.handlers = app.NewHandlers(deps)
	s.management = app.BuildManagement(deps, s.handlers, unlimited)
	s.jobs = app.BuildJobs(deps, s.handlers)
	s.customer = app.BuildCustomerConsumer(s.handlers)
	s.billing = app.BuildBillingConsumer(s.handlers)
	s.droplet = app.BuildDropletConsumer(s.handlers)
}

func (s *kubernetesSuite) TearDownSuite() {
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource, s.mongoResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

// SetupTest announces the catalog every scenario buys from, as the droplet
// context would.
func (s *kubernetesSuite) SetupTest() {
	s.announceRegion(region, "Šiauliai", "LT", true)
	s.announceRegion("ams1", "Amsterdam", "NL", true)
	s.announcePlan("c4-32", region, 4, 32, "0.2766", "185.88", false, true)
	s.announcePlan("c8-64", region, 8, 64, "0.5500", "369.60", false, true)
	s.announcePlan("c1-4", region, 1, 4, "0.0980", "65.86", false, true)
	s.announcePlan("c4-32", "ams1", 4, 32, "0.2766", "185.88", false, true)
	s.announcePlan("c2-16", region, 2, 16, "0.1800", "120.96", true, true)
}

func (s *kubernetesSuite) TearDownTest() {
	ctx := context.Background()
	for _, table := range []string{"nodes", "node_pools", "clusters", "catalog_plans", "catalog_regions", "team_billing", "team_access", "project_access"} {
		_, err := s.pgPool.Exec(ctx, "DELETE FROM "+table)
		s.Require().NoError(err)
	}
	_, _ = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	_ = s.rawRedis.FlushDB(ctx).Err()
	s.engine.Reset()
	s.clock.Set(start)
}

func (s *kubernetesSuite) run(repository, tag string, env []string) *dockertest.Resource {
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
// People, Teams, Projects and the catalog
// ---------------------------------------------------------------------------

type user struct {
	ID    uuid.UUID
	Token string
}

// signIn mints an access token exactly as the customer service would and
// stores the session record it points at.
func (s *kubernetesSuite) signIn() user {
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

func (s *kubernetesSuite) consume(routingKey, payload string) {
	s.Require().NoError(s.consumeErr(routingKey, payload))
}

// consumeErr hands the message to the consumer whose queue it would land on.
func (s *kubernetesSuite) consumeErr(routingKey, payload string) error {
	message := &rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload), CorrelationID: uuid.NewString()}
	switch routingKey {
	case constants.TeamBillingStatusChanged, constants.TeamDropletsDeletionRequested:
		return s.billing.Consume(message)
	case constants.DropletStatusChanged, constants.NodeRejected, constants.CatalogPlanChanged, constants.CatalogRegionChanged:
		return s.droplet.Consume(message)
	default:
		return s.customer.Consume(message)
	}
}

type project struct {
	TeamID    uuid.UUID
	ProjectID uuid.UUID
	Owner     user
}

// grantProject replays what makes a Project usable for Kubernetes: the
// owner's Team and membership and the named Project from the customer
// context, and a payment method from the billing context.
func (s *kubernetesSuite) grantProject() project {
	p := s.grantProjectWithoutBilling()
	s.setBilling(p.TeamID, "active", time.Now())

	return p
}

func (s *kubernetesSuite) grantProjectWithoutBilling() project {
	p := project{TeamID: uuid.New(), ProjectID: uuid.New(), Owner: s.signIn()}
	s.addMember(p.TeamID, p.Owner, "owner")
	s.consume(constants.ProjectCreated, fmt.Sprintf(`{"payload":{"project_id":%q,"team_id":%q,"name":"Shop","created_at":%q}}`,
		p.ProjectID, p.TeamID, time.Now().UTC().Format(time.RFC3339)))

	return p
}

func (s *kubernetesSuite) addMember(teamID uuid.UUID, u user, role string) {
	s.consume(constants.TeamMemberAdded, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"role":%q,"added_at":%q}}`,
		teamID, u.ID, role, time.Now().UTC().Format(time.RFC3339Nano)))
}

func (s *kubernetesSuite) member(teamID uuid.UUID, role string) user {
	u := s.signIn()
	s.addMember(teamID, u, role)

	return u
}

func (s *kubernetesSuite) setBilling(teamID uuid.UUID, status string, at time.Time) {
	s.consume(constants.TeamBillingStatusChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"status":%q,"changed_at":%q}}`,
		teamID, status, at.UTC().Format(time.RFC3339Nano)))
}

func (s *kubernetesSuite) announceRegion(code, city, country string, enabled bool) {
	s.announceRegionAt(code, city, country, enabled, s.clock.Now())
}

func (s *kubernetesSuite) announceRegionAt(code, city, country string, enabled bool, at time.Time) {
	s.consume(constants.CatalogRegionChanged, fmt.Sprintf(`{"payload":{"code":%q,"city":%q,"country":%q,"enabled":%t,"synced_at":%q}}`,
		code, city, country, enabled, at.UTC().Format(time.RFC3339Nano)))
}

func (s *kubernetesSuite) announcePlan(code, regionCode string, cpus, ram int, hourly, monthly string, soldOut, enabled bool) {
	s.announcePlanAt(code, regionCode, cpus, ram, hourly, monthly, soldOut, enabled, s.clock.Now())
}

func (s *kubernetesSuite) announcePlanAt(code, regionCode string, cpus, ram int, hourly, monthly string, soldOut, enabled bool, at time.Time) {
	s.consume(constants.CatalogPlanChanged, fmt.Sprintf(
		`{"payload":{"code":%q,"region_code":%q,"cpu_count":%d,"ram_gb":%d,"hourly_price":%q,"monthly_price":%q,"sold_out":%t,"enabled":%t,"synced_at":%q}}`,
		code, regionCode, cpus, ram, hourly, monthly, soldOut, enabled, at.UTC().Format(time.RFC3339Nano)))
}

// ---------------------------------------------------------------------------
// The droplet context's side of a Node
// ---------------------------------------------------------------------------

// dropletChanged is the droplet context announcing a Node Droplet's status.
func (s *kubernetesSuite) dropletChanged(nodeID, clusterID, dropletID string, status, ipv4 string, extra map[string]any) {
	payload := map[string]any{
		"droplet_id": dropletID, "team_id": uuid.New(), "project_id": uuid.New(), "hostname": "n", "ipv4": ipv4,
		"from_status": nil, "status": status, "node_id": nodeID, "cluster_id": clusterID, "changed_at": time.Now().UTC(),
	}
	for k, v := range extra {
		payload[k] = v
	}
	raw, err := json.Marshal(map[string]any{"payload": payload})
	s.Require().NoError(err)
	s.consume(constants.DropletStatusChanged, string(raw))
}

func (s *kubernetesSuite) nodeRejected(nodeID, clusterID, reason string) {
	s.consume(constants.NodeRejected, fmt.Sprintf(`{"payload":{"node_id":%q,"cluster_id":%q,"reason":%q,"rejected_at":%q}}`,
		nodeID, clusterID, reason, time.Now().UTC().Format(time.RFC3339Nano)))
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
func (s *kubernetesSuite) manage(method, path, body string, u user) *response {
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

func (r *response) list(key string) []map[string]any {
	raw, _ := r.Body[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, m)
	}

	return out
}

// expectError asserts the exact rerror answered.
func (s *kubernetesSuite) expectError(r *response, status, domain, code int, msg ...string) {
	s.Require().Equal(status, r.Code, strings.Join(msg, " ")+": "+r.Raw)
	d, c := r.errorCode()
	s.Equal(domain, d, r.Raw)
	s.Equal(code, c, r.Raw)
}

func projectPath(p project, suffix string) string {
	return "/v1/projects/" + p.ProjectID.String() + "/clusters" + suffix
}

func clusterPath(p project, id, suffix string) string {
	return projectPath(p, "/"+id+suffix)
}

func openBody(name, planCode string, count int) string {
	return fmt.Sprintf(`{"name":%q,"region":%q,"version":"1.37.0","node_pool":{"name":"workers","plan":%q,"count":%d}}`, name, region, planCode, count)
}

func (s *kubernetesSuite) open(p project, name string) *response {
	return s.manage(http.MethodPost, projectPath(p, ""), openBody(name, "c4-32", 2), p.Owner)
}

// opened opens a Cluster and asserts it was accepted.
func (s *kubernetesSuite) opened(p project, name string) *response {
	r := s.open(p, name)
	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)

	return r
}

func (s *kubernetesSuite) get(p project, id string) *response {
	r := s.manage(http.MethodGet, clusterPath(p, id, ""), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)

	return r
}

func (s *kubernetesSuite) nodes(p project, id string) []map[string]any {
	r := s.manage(http.MethodGet, clusterPath(p, id, "/nodes"), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)

	return r.List
}

func (s *kubernetesSuite) provision() { s.Require().NoError(s.jobs.Provision(s.T().Context())) }
func (s *kubernetesSuite) watch()     { s.Require().NoError(s.jobs.Watch(s.T().Context())) }
func (s *kubernetesSuite) release()   { s.Require().NoError(s.jobs.Release(s.T().Context())) }
func (s *kubernetesSuite) finish()    { s.Require().NoError(s.jobs.Delete(s.T().Context())) }

// ready opens a Cluster and walks it to a ready Control plane with its Nodes
// requested, answering the Cluster id and the Nodes.
func (s *kubernetesSuite) ready(p project, name string) (string, []map[string]any) {
	id := s.opened(p, name).str("id")
	s.engine.Advance()
	s.provision()
	nodes := s.nodes(p, id)
	s.Require().Len(nodes, 2)

	return id, nodes
}

// joined has the droplet context deliver every Node's Droplet and the kubelets
// report ready, so the Cluster runs.
func (s *kubernetesSuite) joined(p project, id string, nodes []map[string]any) {
	clusterID := uuid.MustParse(id)
	for i, n := range nodes {
		s.dropletChanged(fmt.Sprint(n["id"]), id, uuid.NewString(), "active", fmt.Sprintf("203.0.113.%d", i+1), nil)
		s.engine.Join(clusterID, fmt.Sprint(n["hostname"]), "nodera://"+fmt.Sprint(n["id"]), true)
	}
	s.watch()
	s.Require().Equal("running", s.get(p, id).str("status"))
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

func (s *kubernetesSuite) outboxPayloads(routingKey string) []map[string]any {
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

// statusChanges answers the status events of one Cluster, in order.
func (s *kubernetesSuite) statusChanges(clusterID string) []map[string]any {
	out := make([]map[string]any, 0)
	for _, payload := range s.outboxPayloads(constants.ClusterStatusChanged) {
		if payload["cluster_id"] == clusterID {
			out = append(out, payload)
		}
	}

	return out
}

// realtimeTypes answers the realtime message types published, in order.
func (s *kubernetesSuite) realtimeTypes() []string {
	out := make([]string, 0)
	for _, payload := range s.outboxPayloads(realtime.RoutingKey) {
		t, _ := payload["type"].(string)
		out = append(out, t)
	}

	return out
}

func (s *kubernetesSuite) clearOutbox() {
	_, err := s.mongoDB.Collection("outbox").DeleteMany(context.Background(), bson.M{})
	s.Require().NoError(err)
}

func (s *kubernetesSuite) userData(nodeID string) string {
	v, err := s.rawRedis.Get(context.Background(), fmt.Sprintf(constants.UserDataKeyFormat, nodeID)).Result()
	if err != nil {
		return ""
	}

	return v
}
