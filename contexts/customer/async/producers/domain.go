package producers

import (
	"context"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/contexts/customer/async/events"
)

// Timestamps go out in UTC so every consumer reads the same instant whatever
// zone the database handed back.

type TeamCreated struct{ client OutboxClient }

func NewTeamCreated(client OutboxClient) *TeamCreated {
	return &TeamCreated{client: client}
}

func (p *TeamCreated) Publish(ctx context.Context, t *team.Team) error {
	return publish(ctx, p.client, constants.TeamCreated, &events.TeamCreatedEvent{
		Payload: events.TeamCreatedBody{TeamID: t.ID, OwnerUserID: t.OwnerUserID, CreatedAt: t.CreatedAt.UTC()},
	})
}

type TeamMemberAdded struct{ client OutboxClient }

func NewTeamMemberAdded(client OutboxClient) *TeamMemberAdded {
	return &TeamMemberAdded{client: client}
}

func (p *TeamMemberAdded) Publish(ctx context.Context, m *team.Member) error {
	return publish(ctx, p.client, constants.TeamMemberAdded, &events.TeamMemberAddedEvent{
		Payload: events.TeamMemberAddedBody{TeamID: m.TeamID, UserID: m.UserID, Role: string(m.Role), AddedAt: m.CreatedAt.UTC()},
	})
}

type ProjectCreated struct{ client OutboxClient }

func NewProjectCreated(client OutboxClient) *ProjectCreated {
	return &ProjectCreated{client: client}
}

func (p *ProjectCreated) Publish(ctx context.Context, pr *project.Project) error {
	return publish(ctx, p.client, constants.ProjectCreated, &events.ProjectCreatedEvent{
		Payload: events.ProjectCreatedBody{ProjectID: pr.ID, TeamID: pr.TeamID, CreatedAt: pr.CreatedAt.UTC()},
	})
}
