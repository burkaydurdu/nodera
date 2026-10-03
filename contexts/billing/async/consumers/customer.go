// Package consumers feeds the billing context's read models from other
// contexts' events: Teams and members from the customer context
// (docs/adr/0004) and, with usage, Droplet hours from the droplet context.
package consumers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

// CustomerEvents applies the Team events. Every write is last-writer-wins on
// the event's own time, so redelivery, replay and the unordered arrival of the
// membership events are harmless.
type CustomerEvents struct {
	access access.IService
}

func NewCustomerEvents(accessService access.IService) *CustomerEvents {
	return &CustomerEvents{access: accessService}
}

// RoutingKeys are the customer events this consumer handles.
func (c *CustomerEvents) RoutingKeys() []string {
	return []string{constants.TeamCreated, constants.TeamMemberAdded, constants.TeamMemberRoleChanged, constants.TeamMemberRemoved}
}

// Consume dispatches on the routing key; a key this context does not consume
// is acknowledged and ignored.
func (c *CustomerEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()

	switch message.RoutingKey {
	case constants.TeamCreated:
		return c.teamCreated(ctx, message)
	case constants.TeamMemberAdded:
		return c.memberAdded(ctx, message)
	case constants.TeamMemberRoleChanged:
		return c.roleChanged(ctx, message)
	case constants.TeamMemberRemoved:
		return c.memberRemoved(ctx, message)
	default:
		return nil
	}
}

// teamCreated records the Team's name and its owner. A replayed TeamCreated
// cannot re-grant an owner who has left since: it carries the creation time,
// older than the tombstone.
func (c *CustomerEvents) teamCreated(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamCreatedEvent
	if err := decode(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if err := requireIDs(p.TeamID); err != nil {
		return err
	}
	if err := requireTime(p.CreatedAt); err != nil {
		return err
	}

	if err := c.access.UpsertTeam(ctx, p.TeamID, p.Name, p.CreatedAt); err != nil {
		return err
	}
	if p.OwnerUserID == uuid.Nil {
		return nil
	}

	return c.access.UpsertTeamMember(ctx, &access.Member{TeamID: p.TeamID, UserID: p.OwnerUserID, Role: authz.RoleOwner, UpdatedAt: p.CreatedAt})
}

func (c *CustomerEvents) memberAdded(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamMemberAddedEvent
	if err := decode(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if err := requireIDs(p.TeamID, p.UserID); err != nil {
		return err
	}
	if err := requireTime(p.AddedAt); err != nil {
		return err
	}
	// Memberships that predate Roles were replayed without one; they are the
	// owners of their Teams.
	role := authz.Role(p.Role)
	if str.IsEmpty(p.Role) {
		role = authz.RoleOwner
	}

	return c.access.UpsertTeamMember(ctx, &access.Member{TeamID: p.TeamID, UserID: p.UserID, Role: role, UpdatedAt: p.AddedAt})
}

func (c *CustomerEvents) roleChanged(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamMemberRoleChangedEvent
	if err := decode(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if err := requireIDs(p.TeamID, p.UserID); err != nil {
		return err
	}
	if err := requireTime(p.ChangedAt); err != nil {
		return err
	}
	if str.IsBlank(p.Role) {
		return access.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the role")
	}

	return c.access.UpdateTeamMemberRole(ctx, p.TeamID, p.UserID, authz.Role(p.Role), p.ChangedAt)
}

func (c *CustomerEvents) memberRemoved(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamMemberRemovedEvent
	if err := decode(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if err := requireIDs(p.TeamID, p.UserID); err != nil {
		return err
	}
	if err := requireTime(p.RemovedAt); err != nil {
		return err
	}

	return c.access.RemoveTeamMember(ctx, p.TeamID, p.UserID, p.RemovedAt)
}

// A malformed payload is terminal: the plain unmarshal error would be retried
// forever, so it is wrapped into a domain error instead.
func decode(message *rabbitmq.Message, into any) error {
	if err := json.Unmarshal(message.Payload, into); err != nil {
		return access.ErrorMalformedEvent.Copy().SetError(err)
	}

	return nil
}

// An event without its time cannot be ordered against the others, so it is
// as malformed as one without an id.
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
