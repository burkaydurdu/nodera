package socket

import (
	"sync"

	"github.com/coder/websocket"

	"github.com/piggystats/nodera/contexts/realtime/auth"
	"github.com/piggystats/nodera/pkg/realtime"
)

// sendBuffer is how many messages a socket may fall behind before it is
// closed; the web app then reconnects and refetches.
const sendBuffer = 64

type closeRequest struct {
	code   websocket.StatusCode
	reason string
}

// client is one socket as the hub sees it. The identity changes when the web
// app presents a refreshed token on the open socket.
type client struct {
	mu       sync.RWMutex
	identity *auth.Identity
	out      chan realtime.Body
	closing  chan closeRequest
	once     sync.Once
}

func newClient(identity *auth.Identity) *client {
	return &client{identity: identity, out: make(chan realtime.Body, sendBuffer), closing: make(chan closeRequest, 1)}
}

func (c *client) Identity() *auth.Identity {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.identity
}

func (c *client) setIdentity(identity *auth.Identity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.identity = identity
}

func (c *client) Deliver(body realtime.Body) {
	select {
	case c.out <- body:
	default:
		c.close(websocket.StatusTryAgainLater, "too slow")
	}
}

func (c *client) GoAway() {
	c.close(websocket.StatusGoingAway, "restarting")
}

// close asks the socket's writer to close it; the first reason wins.
func (c *client) close(code websocket.StatusCode, reason string) {
	c.once.Do(func() { c.closing <- closeRequest{code: code, reason: reason} })
}
