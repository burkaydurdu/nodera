package consumers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

//go:generate mockgen -destination=../../mock/consumers/teams_handler.go -package=mockconsumers github.com/piggystats/nodera/contexts/notification/async/consumers ITeamsHandler
type ITeamsHandler interface {
	RoleChanged(ctx context.Context, change handlers.RoleChange) error
	MemberRemoved(ctx context.Context, removal handlers.Removal) error
}

// CustomerEvents keeps the membership read model (docs/adr/0008) and hands a
// role change or a removal on to be told to the member. Every write is
// last-writer-wins on the event's own time, so redelivery, replay and the
// unordered arrival of the membership events are harmless.
type CustomerEvents struct {
	access access.IService
	teams  ITeamsHandler
}

func NewCustomerEvents(accessService access.IService, teams ITeamsHandler) *CustomerEvents {
	return &CustomerEvents{access: accessService, teams: teams}
}

func (c *CustomerEvents) RoutingKeys() []string {
	return []string{
		constants.TeamCreated, constants.TeamMemberAdded, constants.TeamMemberRoleChanged, constants.TeamMemberRemoved,
		constants.UserProfileUpdated,
	}
}

// Consume dispatches on the routing key; a key this consumer does not handle
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
	case constants.UserProfileUpdated:
		return c.profileUpdated(ctx, message)
	default:
		return nil
	}
}

// teamCreated records the Team's name and, when the producer sent them, the
// owner's contact details; an older producer sends neither and the owner then
// arrives through their own TeamMemberAdded.
func (c *CustomerEvents) teamCreated(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamCreatedEvent
	if err := decodeMembership(message, &event); err != nil {
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

	return c.access.UpsertTeamMember(ctx, &access.Member{
		TeamID: p.TeamID, UserID: p.OwnerUserID, Role: authz.RoleOwner,
		Email: p.OwnerEmail, FullName: p.OwnerFullName, Locale: p.OwnerLocale, UpdatedAt: p.CreatedAt,
	})
}

func (c *CustomerEvents) memberAdded(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamMemberAddedEvent
	if err := decodeMembership(message, &event); err != nil {
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

	return c.access.UpsertTeamMember(ctx, &access.Member{
		TeamID: p.TeamID, UserID: p.UserID, Role: role,
		Email: p.Email, FullName: p.FullName, Locale: p.Locale, UpdatedAt: p.AddedAt,
	})
}

func (c *CustomerEvents) roleChanged(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamMemberRoleChangedEvent
	if err := decodeMembership(message, &event); err != nil {
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

	if err := c.access.UpdateTeamMemberRole(ctx, p.TeamID, p.UserID, authz.Role(p.Role), p.ChangedAt); err != nil {
		return err
	}

	return c.teams.RoleChanged(ctx, handlers.RoleChange{
		TeamID: p.TeamID, UserID: p.UserID, Role: p.Role, ChangedBy: p.ChangedBy, ChangedAt: p.ChangedAt,
	})
}

func (c *CustomerEvents) memberRemoved(ctx context.Context, message *rabbitmq.Message) error {
	var event events.TeamMemberRemovedEvent
	if err := decodeMembership(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if err := requireIDs(p.TeamID, p.UserID); err != nil {
		return err
	}
	if err := requireTime(p.RemovedAt); err != nil {
		return err
	}

	if err := c.access.RemoveTeamMember(ctx, p.TeamID, p.UserID, p.RemovedAt); err != nil {
		return err
	}

	return c.teams.MemberRemoved(ctx, handlers.Removal{TeamID: p.TeamID, UserID: p.UserID, RemovedBy: p.RemovedBy, RemovedAt: p.RemovedAt})
}

func (c *CustomerEvents) profileUpdated(ctx context.Context, message *rabbitmq.Message) error {
	var event events.UserProfileUpdatedEvent
	if err := decodeMembership(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if err := requireIDs(p.UserID); err != nil {
		return err
	}
	if err := requireTime(p.UpdatedAt); err != nil {
		return err
	}

	return c.access.UpdateProfile(ctx, p.UserID, p.FullName, p.Locale, p.UpdatedAt)
}

func decodeMembership(message *rabbitmq.Message, into any) error {
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
