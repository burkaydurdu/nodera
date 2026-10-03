// Package hub holds the sockets of one replica and hands each message to the
// ones it is addressed to.
package hub

import (
	"sync"

	"github.com/piggystats/nodera/contexts/realtime/auth"
	"github.com/piggystats/nodera/pkg/realtime"
)

// Client is one open socket.
type Client interface {
	Identity() *auth.Identity
	// Deliver queues the message without blocking; a socket too slow to keep
	// up is closed by its owner rather than slowing every other socket.
	Deliver(body realtime.Body)
	// GoAway closes the socket as going away: the replica is stopping and the
	// web app should reconnect to another one.
	GoAway()
}

type Hub struct {
	mu      sync.RWMutex
	clients map[Client]struct{}
}

func New() *Hub {
	return &Hub{clients: make(map[Client]struct{})}
}

func (h *Hub) Register(c Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) Unregister(c Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// Deliver offers the message to every socket whose identity accepts it.
func (h *Hub) Deliver(body realtime.Body) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.Identity().Accepts(body.Address) {
			c.Deliver(body)
		}
	}
}

// Each answers every socket, for the shutdown that closes them all.
func (h *Hub) Each(fn func(Client)) {
	h.mu.RLock()
	clients := make([]Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		fn(c)
	}
}
