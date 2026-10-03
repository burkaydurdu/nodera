package app

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/realtime/auth"
	"github.com/piggystats/nodera/contexts/realtime/consumers"
	"github.com/piggystats/nodera/contexts/realtime/fanout"
	"github.com/piggystats/nodera/contexts/realtime/hub"
	"github.com/piggystats/nodera/contexts/realtime/socket"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	nodhttp "github.com/piggystats/nodera/pkg/http"
)

type Settings struct {
	// JWTSecret is the customer service's signing secret; this service only
	// verifies.
	JWTSecret      string
	OriginPatterns []string
	AuthTimeout    time.Duration
	Heartbeat      time.Duration
}

type Dependencies struct {
	Log      logger.ILog
	Redis    redis.IClient
	Settings Settings
	// RateLimit is handshakes per second per IP; zero is the platform default.
	RateLimit int
}

// Realtime is one replica: the socket server, the consumer of the contexts'
// messages and the fan-out between them. The binary and the integration
// suite both build it here.
type Realtime struct {
	Server   *echo.Echo
	Consumer *consumers.Messages
	hub      *hub.Hub
	fanout   *fanout.Fanout
	sockets  *socket.Handler
}

func Build(d Dependencies) *Realtime {
	h := hub.New()
	f := fanout.New(d.Log, d.Redis.GetClient())
	authenticator := auth.NewAuthenticator(d.Settings.JWTSecret, redis.NewService(d.Redis))
	sockets := socket.NewHandler(d.Log, socket.Settings{
		OriginPatterns: d.Settings.OriginPatterns,
		AuthTimeout:    d.Settings.AuthTimeout,
		Heartbeat:      d.Settings.Heartbeat,
	}, authenticator, h)

	server := nodhttp.CreateEcho(d.Log, d.RateLimit)
	server.GET(nodhttp.HealthPath, func(c echo.Context) error {
		return c.JSON(http.StatusOK, "OK")
	})
	sockets.SetRoutes(server)

	return &Realtime{Server: server, Consumer: consumers.NewMessages(f), hub: h, fanout: f, sockets: sockets}
}

// Start subscribes this replica to the fan-out; call it before serving.
func (r *Realtime) Start(ctx context.Context) error {
	return r.fanout.Start(ctx, r.hub)
}

// Shutdown closes every socket as going away, so the web app reconnects to
// another replica at once, and waits for them until ctx ends.
func (r *Realtime) Shutdown(ctx context.Context) error {
	return r.sockets.Shutdown(ctx)
}
