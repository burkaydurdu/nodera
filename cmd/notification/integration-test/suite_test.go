package integrationtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
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
	"github.com/piggystats/rabbitmq"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"

	"github.com/piggystats/nodera/cmd/notification/app"
	"github.com/piggystats/nodera/cmd/notification/config"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/realtime"
)

const (
	testJWTSecret = "notification-integration-test-jwt-secret"
	sessionKey    = "session:%s"
)

var errUnavailable = errors.New("unavailable")

// sentEmail is one email as the sender was asked to send it.
type sentEmail struct {
	Kind   email.Kind
	To     string
	Locale string
	Params map[string]any
}

// fakeEmail stands in for SMTP: it records every email and can fail the next.
type fakeEmail struct {
	mu       sync.Mutex
	sent     []sentEmail
	failNext bool
}

func (f *fakeEmail) Send(_ context.Context, kind email.Kind, to, locale string, params map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false

		return errUnavailable
	}
	f.sent = append(f.sent, sentEmail{Kind: kind, To: to, Locale: locale, Params: params})

	return nil
}

func (f *fakeEmail) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent, f.failNext = nil, false
}

// fakeRealtime stands in for the broker: it records every realtime message.
type fakeRealtime struct {
	mu       sync.Mutex
	messages []realtime.Body
	failNext bool
}

func (f *fakeRealtime) Publish(_ context.Context, message *realtime.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false

		return errUnavailable
	}
	f.messages = append(f.messages, message.Payload)

	return nil
}

func (f *fakeRealtime) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages, f.failNext = nil, false
}

type notificationSuite struct {
	suite.Suite
	pool             *dockertest.Pool
	postgresResource *dockertest.Resource
	redisResource    *dockertest.Resource

	server    *echo.Echo
	pgPool    *pgxpool.Pool
	rawRedis  *goredis.Client
	email     *fakeEmail
	realtime  *fakeRealtime
	consumers *app.Consumers
}

func TestNotificationSuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(notificationSuite))
}

func (s *notificationSuite) SetupSuite() {
	var err error
	s.pool, err = dockertest.NewPool("")
	s.Require().NoError(err)
	s.Require().NoError(s.pool.Client.Ping())
	// Image pulls on a cold machine take longer than dockertest's default minute.
	s.pool.MaxWait = 3 * time.Minute

	s.postgresResource = s.run("postgres", "18", []string{"POSTGRES_USER=testuser", "POSTGRES_PASSWORD=testpassword", "POSTGRES_DB=testdb"})
	s.redisResource = s.run("redis", "8.2", nil)

	pgPort, _ := strconv.Atoi(s.postgresResource.GetPort("5432/tcp"))
	redisPort, _ := strconv.Atoi(s.redisResource.GetPort("6379/tcp"))
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
	s.Require().NoError(runMigrate(postgresURI, "../../../contexts/notification/db/migrations", "schema_migrations"))

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

	s.email = &fakeEmail{}
	s.realtime = &fakeRealtime{}
	conf := &config.Config{
		Environment: "dev", Server: config.Server{Name: "nodera-notification-test", Port: ":0"}, Secret: config.Secret{JWT: testJWTSecret},
		// Written with a trailing slash, as an operator may: it must not double
		// in the links the emails carry.
		Domain: config.Domain{Front: "http://localhost:3002/"},
	}
	deps := app.Dependencies{
		Log:      log,
		Conf:     conf,
		Pool:     s.pgPool,
		Redis:    redis.NewService(redisClient),
		Email:    s.email,
		Realtime: s.realtime,
	}

	const unlimited = 1_000_000
	s.server = app.BuildServer(deps, unlimited)
	s.consumers = app.BuildConsumers(deps)
}

func (s *notificationSuite) TearDownSuite() {
	if s.pgPool != nil {
		s.pgPool.Close()
	}
	for _, r := range []*dockertest.Resource{s.postgresResource, s.redisResource} {
		if r != nil {
			_ = s.pool.Purge(r)
		}
	}
}

func (s *notificationSuite) TearDownTest() {
	ctx := context.Background()
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM notification_deliveries`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM notifications`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM team_members`)
	_, _ = s.pgPool.Exec(ctx, `DELETE FROM teams`)
	_ = s.rawRedis.FlushDB(ctx).Err()
	s.email.reset()
	s.realtime.reset()
}

func (s *notificationSuite) run(repository, tag string, env []string) *dockertest.Resource {
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
// People and Teams
// ---------------------------------------------------------------------------

// user is a signed-in customer: an id, the contact details the customer
// context would publish, and an access token whose session is alive.
type user struct {
	ID       uuid.UUID
	Email    string
	FullName string
	Locale   string
	Token    string
}

// signIn mints an access token exactly as the customer service would and
// stores the session record it points at.
func (s *notificationSuite) signIn(fullName, locale string) user {
	userID, sessionID := uuid.New(), uuid.New()
	now := time.Now()
	address := strings.ToLower(fullName) + "-" + userID.String()[:8] + "@example.com"
	claims := jwt.MapClaims{
		"iss": "nodera-customer", "sub": userID.String(), "iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
		"typ": "access", "sid": sessionID.String(), "email": address, "status": "active", "teams": map[string]any{}, "pv": 0,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	s.Require().NoError(err)
	s.Require().NoError(s.rawRedis.Set(context.Background(), fmt.Sprintf(sessionKey, sessionID), "1", time.Hour).Err())

	return user{ID: userID, Email: address, FullName: fullName, Locale: locale, Token: token}
}

func (s *notificationSuite) consume(routingKey, payload string) {
	s.Require().NoError(s.consumeErr(routingKey, payload))
}

// consumeErr hands the message to the consumer whose queue it would land on.
func (s *notificationSuite) consumeErr(routingKey, payload string) error {
	return s.deliver(&rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload), MessageID: uuid.NewString(), CorrelationID: uuid.NewString()})
}

// deliver is the broker handing over one message; a redelivery hands over the
// same message again, its id included.
func (s *notificationSuite) deliver(message *rabbitmq.Message) error {
	for _, source := range s.consumers.Sources() {
		if slices.Contains(source.RoutingKeys(), message.RoutingKey) {
			return source.Consume(message)
		}
	}
	s.FailNow("no consumer for " + message.RoutingKey)

	return nil
}

// team is the customer context creating a Team with its owner.
func (s *notificationSuite) team(name string, owner user) uuid.UUID {
	teamID := uuid.New()
	s.consume(constants.TeamCreated, s.json(map[string]any{"payload": map[string]any{
		"team_id": teamID, "name": name, "owner_user_id": owner.ID, "owner_email": owner.Email,
		"owner_full_name": owner.FullName, "owner_locale": owner.Locale, "created_at": time.Now().UTC(),
	}}))

	return teamID
}

func (s *notificationSuite) addMember(teamID uuid.UUID, member user, role string) {
	s.consume(constants.TeamMemberAdded, s.json(map[string]any{"payload": map[string]any{
		"team_id": teamID, "user_id": member.ID, "role": role, "email": member.Email,
		"full_name": member.FullName, "locale": member.Locale, "added_at": time.Now().UTC(),
	}}))
}

func (s *notificationSuite) removeMember(teamID uuid.UUID, member user) {
	s.consume(constants.TeamMemberRemoved, s.json(map[string]any{"payload": map[string]any{
		"team_id": teamID, "user_id": member.ID, "removed_at": time.Now().UTC(),
	}}))
}

// droplet is a Droplet of the Team as its status changes are reported.
type droplet struct {
	ID        uuid.UUID
	TeamID    uuid.UUID
	ProjectID uuid.UUID
	Hostname  string
}

func newDroplet(teamID uuid.UUID, hostname string) droplet {
	return droplet{ID: uuid.New(), TeamID: teamID, ProjectID: uuid.New(), Hostname: hostname}
}

// changed is the droplet context publishing DropletStatusChanged.
func (s *notificationSuite) changed(d droplet, from *string, to string, extra map[string]any) error {
	payload := map[string]any{
		"droplet_id": d.ID, "team_id": d.TeamID, "project_id": d.ProjectID, "hostname": d.Hostname,
		"ipv4": "", "from_status": from, "status": to, "changed_at": time.Now().UTC(),
	}
	for k, v := range extra {
		payload[k] = v
	}

	return s.consumeErr(constants.DropletStatusChanged, s.json(map[string]any{"payload": payload}))
}

// becameActive walks a Droplet from provisioning to active.
func (s *notificationSuite) becameActive(d droplet, ipv4 string) {
	from := "provisioning"
	s.Require().NoError(s.changed(d, &from, "active", map[string]any{"ipv4": ipv4}))
}

func (s *notificationSuite) failed(d droplet, code string) error {
	from := "provisioning"

	return s.changed(d, &from, "failed", map[string]any{"failure_code": code, "failure_reason": "the server request was refused"})
}

func (s *notificationSuite) json(v any) string {
	raw, err := json.Marshal(v)
	s.Require().NoError(err)

	return string(raw)
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

func (s *notificationSuite) request(method, path, token string) *response {
	var body io.Reader
	req := httptest.NewRequest(method, path, body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, req)

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

func (r *response) errorCode() (domain, code int) {
	d, _ := r.Body["domain"].(float64)
	c, _ := r.Body["code"].(float64)

	return int(d), int(c)
}

// inbox answers the caller's first Inbox page.
func (s *notificationSuite) inbox(u user) []map[string]any {
	res := s.request(http.MethodGet, "/v1/notifications", u.Token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return res.List
}

func (s *notificationSuite) unread(u user) int {
	res := s.request(http.MethodGet, "/v1/notifications/unread-count", u.Token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	count, _ := res.Body["count"].(float64)

	return int(count)
}

// pushedTo answers the realtime messages addressed to the User, in order.
func (s *notificationSuite) pushedTo(u user, messageType string) []realtime.Body {
	s.realtime.mu.Lock()
	defer s.realtime.mu.Unlock()
	out := make([]realtime.Body, 0)
	for _, m := range s.realtime.messages {
		if m.Type == messageType && m.Address.UserID != nil && *m.Address.UserID == u.ID {
			out = append(out, m)
		}
	}

	return out
}

func (s *notificationSuite) emailsTo(u user) []sentEmail {
	s.email.mu.Lock()
	defer s.email.mu.Unlock()
	out := make([]sentEmail, 0)
	for _, e := range s.email.sent {
		if e.To == u.Email {
			out = append(out, e)
		}
	}

	return out
}
