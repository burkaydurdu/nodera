// Package realtime is the contract between the contexts and the realtime
// service (docs/adr/0007): the one message a context publishes, through its
// outbox and RabbitMQ, when the web app should learn something while it is
// open. The realtime service knows nothing else about the contexts, so the
// address carries everything it needs to pick the sockets.
package realtime

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

// RoutingKey is the routing key of Message on the common exchange.
const RoutingKey = "RealtimeMessage"

// Message wraps Body in the payload envelope every event on the exchange uses.
type Message struct {
	Payload Body `json:"payload"`
}

// Body is what reaches the browser as {"type", "data", "at"}; the address
// stays on the server.
type Body struct {
	Address Address         `json:"address"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
	At      time.Time       `json:"at"`
}

// Address names the sockets a message is for: every socket of one User, or
// every socket of a Team's members whose Role holds the Permission.
type Address struct {
	UserID     *uuid.UUID       `json:"user_id,omitempty"`
	TeamID     *uuid.UUID       `json:"team_id,omitempty"`
	Permission authz.Permission `json:"permission,omitempty"`
}

func ToUser(userID uuid.UUID) Address {
	return Address{UserID: &userID}
}

func ToTeam(teamID uuid.UUID, permission authz.Permission) Address {
	return Address{TeamID: &teamID, Permission: permission}
}

// Valid is exactly one of the two shapes; a Team address without a known
// Permission would reach nobody or everybody, so it is refused.
func (a Address) Valid() bool {
	switch {
	case a.UserID != nil && a.TeamID == nil:
		return *a.UserID != uuid.Nil
	case a.TeamID != nil && a.UserID == nil:
		return *a.TeamID != uuid.Nil && authz.IsPermission(string(a.Permission))
	default:
		return false
	}
}

// New builds the message; at goes out in UTC like every event timestamp.
func New(address Address, messageType string, data any, at time.Time) (*Message, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &Message{Payload: Body{Address: address, Type: messageType, Data: raw, At: at.UTC()}}, nil
}
