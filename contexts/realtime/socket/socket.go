// Package socket is the realtime WebSocket: one per browser tab, server to
// browser in this version, authenticated by the access token in its first
// frame because a browser cannot set headers on the handshake
// (docs/adr/0007).
package socket

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/realtime/auth"
	"github.com/piggystats/nodera/contexts/realtime/hub"
	"github.com/piggystats/nodera/logger"
)

// Close codes the web app acts on.
const (
	// CloseUnauthorized: refresh the access token, then reconnect.
	CloseUnauthorized websocket.StatusCode = 4401
	// CloseAuthTimeout: the first frame was late; reconnect.
	CloseAuthTimeout websocket.StatusCode = 4408
)

const (
	frameAuth  = "auth"
	frameReady = "ready"

	// readLimit fits an access token of someone in many Teams.
	readLimit    = 16 << 10
	writeTimeout = 10 * time.Second
)

type Settings struct {
	// OriginPatterns are the web app's hosts, matched as coder/websocket does;
	// any other site is refused at the handshake.
	OriginPatterns []string
	AuthTimeout    time.Duration
	// Heartbeat is how often a socket is pinged and its session and
	// permission version checked again.
	Heartbeat time.Duration
}

type Authenticator interface {
	Verify(ctx context.Context, raw string) (*auth.Identity, error)
	Check(ctx context.Context, identity *auth.Identity) error
}

type inbound struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type outbound struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
	At   *time.Time      `json:"at,omitempty"`
}

type Handler struct {
	log      logger.ILog
	settings Settings
	auth     Authenticator
	hub      *hub.Hub
	// open counts the sockets being served: the HTTP server forgets a
	// connection once it is hijacked, so Shutdown waits on this instead.
	open sync.WaitGroup
}

func NewHandler(log logger.ILog, settings Settings, authenticator Authenticator, h *hub.Hub) *Handler {
	return &Handler{log: log, settings: settings, auth: authenticator, hub: h}
}

func (h *Handler) SetRoutes(e *echo.Echo) {
	e.GET("/v1/realtime", h.Serve)
}

func (h *Handler) Serve(c echo.Context) error {
	conn, err := websocket.Accept(c.Response(), c.Request(), &websocket.AcceptOptions{OriginPatterns: h.settings.OriginPatterns})
	if err != nil {
		// Accept has already answered the refusal.
		return nil
	}
	conn.SetReadLimit(readLimit)

	h.open.Add(1)
	defer h.open.Done()
	h.run(c.Request().Context(), conn)

	return nil
}

// Shutdown tells every socket the replica is going away, so the web app
// reconnects to another one at once, and waits until each was told or ctx
// ends. Stop accepting new sockets first.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.hub.Each(func(c hub.Client) { c.GoAway() })

	done := make(chan struct{})
	go func() {
		h.open.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handler) run(ctx context.Context, conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	identity, err := h.authenticate(ctx, conn)
	if err != nil {
		h.closeFor(conn, err)

		return
	}

	c := newClient(identity)
	h.hub.Register(c)
	defer h.hub.Unregister(c)

	if err := write(ctx, conn, outbound{Type: frameReady}); err != nil {
		return
	}

	readErr := make(chan error, 1)
	go func() { readErr <- h.read(ctx, conn, c) }()

	ticker := time.NewTicker(h.settings.Heartbeat)
	defer ticker.Stop()

	for {
		select {
		case body := <-c.out:
			at := body.At
			if err := write(ctx, conn, outbound{Type: body.Type, Data: body.Data, At: &at}); err != nil {
				return
			}
		case <-ticker.C:
			if err := h.heartbeat(ctx, conn, c); err != nil {
				h.closeFor(conn, err)

				return
			}
		case request := <-c.closing:
			_ = conn.Close(request.code, request.reason)

			return
		case err := <-readErr:
			h.closeFor(conn, err)

			return
		}
	}
}

// authenticate waits for the first frame. The deadline closes the socket
// itself: coder/websocket drops a connection whose read context expires
// without a close frame, and the web app needs the code.
func (h *Handler) authenticate(ctx context.Context, conn *websocket.Conn) (*auth.Identity, error) {
	timer := time.AfterFunc(h.settings.AuthTimeout, func() {
		_ = conn.Close(CloseAuthTimeout, "no token in time")
	})
	frame, err := readFrame(ctx, conn)
	if !timer.Stop() {
		return nil, errClosed
	}
	if err != nil {
		return nil, err
	}
	if frame.Type != frameAuth {
		return nil, auth.ErrUnauthorized
	}

	return h.auth.Verify(ctx, frame.Token)
}

// read handles what the web app sends on an open socket: a refreshed token
// for the same User. Other frame types are left for later versions.
func (h *Handler) read(ctx context.Context, conn *websocket.Conn, c *client) error {
	for {
		frame, err := readFrame(ctx, conn)
		if err != nil {
			return err
		}
		if frame.Type != frameAuth {
			continue
		}

		identity, err := h.auth.Verify(ctx, frame.Token)
		if errors.Is(err, auth.ErrUnauthorized) {
			return err
		}
		if err != nil {
			// As on a heartbeat, Redis being unreachable is not the caller's
			// fault: the socket keeps its previous token until the next one.
			h.log.ErrorWithFields(err, logger.NewFields().Add("user", c.Identity().UserID.String()))

			continue
		}
		if identity.UserID != c.Identity().UserID {
			return auth.ErrUnauthorized
		}
		c.setIdentity(identity)
		if err := write(ctx, conn, outbound{Type: frameReady}); err != nil {
			return err
		}
	}
}

// heartbeat proves the browser is still there and the caller still who they
// were. Redis being unreachable is not the caller's fault and keeps the
// socket; the next heartbeat asks again.
func (h *Handler) heartbeat(ctx context.Context, conn *websocket.Conn, c *client) error {
	pingCtx, cancel := context.WithTimeout(ctx, h.settings.Heartbeat)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		return errClosed
	}

	err := h.auth.Check(ctx, c.Identity())
	if errors.Is(err, auth.ErrUnauthorized) {
		return err
	}
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("user", c.Identity().UserID.String()))
	}

	return nil
}

var (
	// errClosed is a socket that is already gone; there is nothing to tell it.
	errClosed = errors.New("socket closed")
	// errProtocol is a frame that is not JSON text.
	errProtocol = errors.New("malformed frame")
)

func (h *Handler) closeFor(conn *websocket.Conn, err error) {
	switch {
	case errors.Is(err, errClosed), websocket.CloseStatus(err) != -1:
		_ = conn.CloseNow()
	case errors.Is(err, auth.ErrUnauthorized):
		_ = conn.Close(CloseUnauthorized, "unauthorized")
	case errors.Is(err, errProtocol):
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
	default:
		h.log.Error(err)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
	}
}

func readFrame(ctx context.Context, conn *websocket.Conn) (*inbound, error) {
	kind, raw, err := conn.Read(ctx)
	if err != nil {
		return nil, errClosed
	}
	var frame inbound
	if kind != websocket.MessageText || json.Unmarshal(raw, &frame) != nil {
		return nil, errProtocol
	}

	return &frame, nil
}

func write(ctx context.Context, conn *websocket.Conn, frame outbound) error {
	raw, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	return conn.Write(ctx, websocket.MessageText, raw)
}

var _ hub.Client = (*client)(nil)
