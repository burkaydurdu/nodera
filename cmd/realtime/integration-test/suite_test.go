package integrationtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/piggystats/rabbitmq"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"

	"github.com/piggystats/nodera/cmd/realtime/app"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

const (
	testJWTSecret = "realtime-integration-test-jwt-secret"
	webOrigin     = "http://localhost:3002"
	waitFor       = 3 * time.Second
)

type realtimeSuite struct {
	suite.Suite
	pool          *dockertest.Pool
	redisResource *dockertest.Resource
	redisClient   redis.IClient
	rawRedis      *goredis.Client
	cancel        context.CancelFunc

	// Two replicas behind one Redis, as in production.
	a, b             *app.Realtime
	serverA, serverB *httptest.Server
}

func TestRealtimeSuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=true to run the dockertest suite")
	}
	suite.Run(t, new(realtimeSuite))
}

func (s *realtimeSuite) SetupSuite() {
	var err error
	s.pool, err = dockertest.NewPool("")
	s.Require().NoError(err)
	s.Require().NoError(s.pool.Client.Ping())
	s.pool.MaxWait = 3 * time.Minute

	s.redisResource, err = s.pool.RunWithOptions(
		&dockertest.RunOptions{Repository: "redis", Tag: "8.2"},
		func(hostConfig *docker.HostConfig) {
			hostConfig.AutoRemove = true
			hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
		},
	)
	s.Require().NoError(err)
	redisPort, _ := strconv.Atoi(s.redisResource.GetPort("6379/tcp"))

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
	s.redisClient = redisClient
	s.rawRedis = redisClient.GetClient()

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.a, s.serverA = s.replica(ctx, log, redisClient)
	s.b, s.serverB = s.replica(ctx, log, redisClient)
}

// replica is one realtime process with timings short enough for a test.
func (s *realtimeSuite) replica(ctx context.Context, log logger.ILog, redisClient redis.IClient) (*app.Realtime, *httptest.Server) {
	r := app.Build(app.Dependencies{
		Log:       log,
		Redis:     redisClient,
		RateLimit: 1_000_000,
		Settings: app.Settings{
			JWTSecret:      testJWTSecret,
			OriginPatterns: []string{"localhost:3002"},
			AuthTimeout:    300 * time.Millisecond,
			Heartbeat:      150 * time.Millisecond,
		},
	})
	s.Require().NoError(r.Start(ctx))

	return r, httptest.NewServer(r.Server)
}

func (s *realtimeSuite) TearDownSuite() {
	for _, server := range []*httptest.Server{s.serverA, s.serverB} {
		if server != nil {
			server.Close()
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.redisResource != nil {
		_ = s.pool.Purge(s.redisResource)
	}
}

func (s *realtimeSuite) TearDownTest() {
	_ = s.rawRedis.FlushDB(context.Background()).Err()
}

// ---------------------------------------------------------------------------
// People
// ---------------------------------------------------------------------------

type user struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	Token     string
}

// signIn stores a live session and mints an access token for it carrying the
// Teams and the permission version, as the customer service would.
func (s *realtimeSuite) signIn(teams map[uuid.UUID]authz.Role) user {
	u := user{ID: uuid.New(), SessionID: uuid.New()}
	s.Require().NoError(s.rawRedis.Set(context.Background(), "session:"+u.SessionID.String(), "1", time.Hour).Err())
	u.Token = s.token(u, teams, 0)

	return u
}

func (s *realtimeSuite) token(u user, teams map[uuid.UUID]authz.Role, pv int64) string {
	claim := map[string]any{}
	for teamID, role := range teams {
		claim[teamID.String()] = string(role)
	}
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "nodera-customer", "sub": u.ID.String(), "iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
		"typ": "access", "sid": u.SessionID.String(), "email": "x@example.com", "status": "active", "teams": claim, "pv": pv,
	}).SignedString([]byte(testJWTSecret))
	s.Require().NoError(err)

	return token
}

// ---------------------------------------------------------------------------
// Sockets
// ---------------------------------------------------------------------------

// socket reads all the time, as a browser does: control frames, and with
// them the server's pings, are only answered while something reads.
type socket struct {
	conn   *websocket.Conn
	frames chan map[string]any
	done   chan struct{}
	err    error
}

func newSocket(conn *websocket.Conn) *socket {
	k := &socket{conn: conn, frames: make(chan map[string]any, 64), done: make(chan struct{})}
	go func() {
		defer close(k.done)
		for {
			_, raw, err := conn.Read(context.Background())
			if err != nil {
				k.err = err

				return
			}
			var frame map[string]any
			if err := json.Unmarshal(raw, &frame); err != nil {
				k.err = err

				return
			}
			k.frames <- frame
		}
	}()

	return k
}

func (s *realtimeSuite) dial(server *httptest.Server, origin string) (*socket, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	conn, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, res, err
	}
	s.T().Cleanup(func() { _ = conn.CloseNow() })

	return newSocket(conn), res, nil
}

// connect opens a socket on the server and authenticates it.
func (s *realtimeSuite) connect(server *httptest.Server, token string) *socket {
	sock, _, err := s.dial(server, webOrigin)
	s.Require().NoError(err)
	sock.send(s, map[string]any{"type": "auth", "token": token})
	s.Require().Equal("ready", sock.next(s)["type"])

	return sock
}

func (k *socket) send(s *realtimeSuite, frame map[string]any) {
	raw, err := json.Marshal(frame)
	s.Require().NoError(err)
	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	s.Require().NoError(k.conn.Write(ctx, websocket.MessageText, raw))
}

// next answers the next frame, failing the test if none arrives in time.
func (k *socket) next(s *realtimeSuite) map[string]any {
	select {
	case frame := <-k.frames:
		return frame
	case <-k.done:
		s.FailNow("the socket closed", "%v", k.err)
	case <-time.After(waitFor):
		s.FailNow("no frame arrived in time")
	}

	return nil
}

// closedWith waits for the server to close the socket and answers the code.
func (k *socket) closedWith(s *realtimeSuite) websocket.StatusCode {
	select {
	case <-k.done:
	case <-time.After(waitFor):
		s.FailNow("the socket was not closed in time")
	}
	var closeErr websocket.CloseError
	s.Require().True(errors.As(k.err, &closeErr), "expected a close, got %v", k.err)

	return closeErr.Code
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// publish is a context's realtime message arriving on the replica's queue.
func (s *realtimeSuite) publish(r *app.Realtime, address realtime.Address, messageType string, data any) {
	s.Require().NoError(s.publishErr(r, address, messageType, data))
}

func (s *realtimeSuite) publishErr(r *app.Realtime, address realtime.Address, messageType string, data any) error {
	message, err := realtime.New(address, messageType, data, time.Now())
	s.Require().NoError(err)
	raw, err := json.Marshal(message)
	s.Require().NoError(err)

	return r.Consumer.Consume(&rabbitmq.Message{RoutingKey: realtime.RoutingKey, Payload: raw, CorrelationID: uuid.NewString()})
}

// nothingBut proves a socket received nothing before the marker: the marker
// is sent to its User after the message under test, and delivery per socket
// is in order.
func (s *realtimeSuite) nothingBut(sock *socket, u user, r *app.Realtime) {
	s.publish(r, realtime.ToUser(u.ID), "test.marker", map[string]any{})
	s.Equal("test.marker", sock.next(s)["type"])
}
