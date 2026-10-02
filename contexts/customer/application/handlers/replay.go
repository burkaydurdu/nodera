package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
)

type ReplaySummary struct {
	Teams    int
	Members  int
	Projects int
}

// Replay republishes the Team, membership and Project events for every
// existing row, so a read model built after the rows can catch up
// (docs/adr/0004). Consumers treat the events as upserts, so running it
// twice is harmless.
type Replay struct {
	teams          team.IService
	projects       project.IService
	teamCreated    ITeamCreatedProducer
	memberAdded    ITeamMemberAddedProducer
	projectCreated IProjectCreatedProducer
}

func NewReplay(
	teams team.IService,
	projects project.IService,
	teamCreated ITeamCreatedProducer,
	memberAdded ITeamMemberAddedProducer,
	projectCreated IProjectCreatedProducer,
) *Replay {
	return &Replay{teams: teams, projects: projects, teamCreated: teamCreated, memberAdded: memberAdded, projectCreated: projectCreated}
}

// Run publishes Teams before memberships and memberships before Projects so a
// consumer relaying in order never sees a member of an unknown Team.
func (h *Replay) Run(ctx context.Context) (*ReplaySummary, error) {
	summary := &ReplaySummary{}

	teams, err := h.teams.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		if err := h.teamCreated.Publish(ctx, &t.Team, t.Owner); err != nil {
			return nil, err
		}
		summary.Teams++
	}

	members, err := h.teams.FindAllMembers(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if err := h.memberAdded.Publish(ctx, &m.Member, m.Person); err != nil {
			return nil, err
		}
		summary.Members++
	}

	projects, err := h.projects.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if err := h.projectCreated.Publish(ctx, p); err != nil {
			return nil, err
		}
		summary.Projects++
	}

	return summary, nil
}
