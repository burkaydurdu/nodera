// Package consumers feeds the Project access read model from the customer
// context's events (docs/adr/0004).
package consumers

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/access"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
	"github.com/piggystats/nodera/utils/str"
)

const roleOwner = "owner"

// CustomerEvents applies the three Team and Project events; every write is
// an idempotent upsert so redelivery and replay are harmless.
type CustomerEvents struct {
	access access.IService
}

func NewCustomerEvents(accessService access.IService) *CustomerEvents {
	return &CustomerEvents{access: accessService}
}

// Consume dispatches on the routing key; a key this context does not consume
// is acknowledged and ignored.
func (c *CustomerEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()

	switch message.RoutingKey {
	case constants.TeamCreated:
		var event events.TeamCreatedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.TeamID, event.Payload.OwnerUserID); err != nil {
			return err
		}

		return c.access.UpsertTeamMember(ctx, event.Payload.TeamID, event.Payload.OwnerUserID, roleOwner)

	case constants.TeamMemberAdded:
		var event events.TeamMemberAddedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.TeamID, event.Payload.UserID); err != nil {
			return err
		}
		role := event.Payload.Role
		if str.IsEmpty(role) {
			role = roleOwner
		}

		return c.access.UpsertTeamMember(ctx, event.Payload.TeamID, event.Payload.UserID, role)

	case constants.ProjectCreated:
		var event events.ProjectCreatedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.ProjectID, event.Payload.TeamID); err != nil {
			return err
		}

		return c.access.UpsertProject(ctx, event.Payload.ProjectID, event.Payload.TeamID)

	default:
		return nil
	}
}

// A malformed payload is terminal: the plain unmarshal error would be retried
// forever, so it is wrapped into a domain error instead.
func decode(message *rabbitmq.Message, into any) error {
	if err := json.Unmarshal(message.Payload, into); err != nil {
		return access.ErrorMalformedEvent.Copy().SetError(err)
	}

	return nil
}

func requireIDs(ids ...uuid.UUID) error {
	for _, id := range ids {
		if id == uuid.Nil {
			return access.ErrorMalformedEvent.Copy().SetMessage("event payload is missing an id")
		}
	}

	return nil
}
