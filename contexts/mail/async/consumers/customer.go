// Package consumers feeds the read models from other contexts' events: Project
// access from the customer context (docs/adr/0004) and Billing status from
// the billing context (docs/adr/0006).
package consumers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/async/constants"
	"github.com/piggystats/nodera/contexts/mail/async/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

// CustomerEvents applies the Team and Project events. Every membership write
// is last-writer-wins on the event's own time, so redelivery, replay and the
// unordered arrival of the three membership events are harmless.
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
		// The creator's membership arrives as its own TeamMemberAdded;
		// acknowledged so the queue stays bound.
		return nil

	case constants.TeamMemberAdded:
		var event events.TeamMemberAddedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.TeamID, event.Payload.UserID); err != nil {
			return err
		}
		if err := requireTime(event.Payload.AddedAt); err != nil {
			return err
		}
		// Memberships that predate Roles were replayed without one; they are
		// the owners of their Teams.
		role := authz.Role(event.Payload.Role)
		if str.IsEmpty(event.Payload.Role) {
			role = authz.RoleOwner
		}

		return c.access.UpsertTeamMember(ctx, event.Payload.TeamID, event.Payload.UserID, role, event.Payload.AddedAt)

	case constants.TeamMemberRoleChanged:
		var event events.TeamMemberRoleChangedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.TeamID, event.Payload.UserID); err != nil {
			return err
		}
		if err := requireTime(event.Payload.ChangedAt); err != nil {
			return err
		}
		if str.IsBlank(event.Payload.Role) {
			return access.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the role")
		}

		return c.access.UpdateTeamMemberRole(ctx, event.Payload.TeamID, event.Payload.UserID, authz.Role(event.Payload.Role), event.Payload.ChangedAt)

	case constants.TeamMemberRemoved:
		var event events.TeamMemberRemovedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.TeamID, event.Payload.UserID); err != nil {
			return err
		}
		if err := requireTime(event.Payload.RemovedAt); err != nil {
			return err
		}

		return c.access.RemoveTeamMember(ctx, event.Payload.TeamID, event.Payload.UserID, event.Payload.RemovedAt)

	case constants.ProjectCreated:
		var event events.ProjectCreatedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.ProjectID, event.Payload.TeamID); err != nil {
			return err
		}

		return c.access.UpsertProject(ctx, &access.ProjectAccess{
			ProjectID: event.Payload.ProjectID,
			TeamID:    event.Payload.TeamID,
			Name:      strings.TrimSpace(event.Payload.Name),
		})

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

// A membership event without its time cannot be ordered against the others,
// so it is as malformed as one without an id.
func requireTime(at time.Time) error {
	if at.IsZero() {
		return access.ErrorMalformedEvent.Copy().SetMessage("event payload is missing its time")
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
