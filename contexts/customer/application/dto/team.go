package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
)

type Team struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func ToTeam(m *team.Membership) *Team {
	return &Team{ID: m.Team.ID, Name: m.Team.Name, Role: string(m.Role), CreatedAt: m.Team.CreatedAt}
}

func ToTeams(memberships team.Memberships) []*Team {
	out := make([]*Team, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, ToTeam(m))
	}

	return out
}

type Project struct {
	ID          uuid.UUID `json:"id"`
	TeamID      uuid.UUID `json:"team_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func ToProject(p *project.Project) *Project {
	return &Project{ID: p.ID, TeamID: p.TeamID, Name: p.Name, Description: p.Description, CreatedAt: p.CreatedAt}
}

func ToProjects(projects project.Projects) []*Project {
	out := make([]*Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, ToProject(p))
	}

	return out
}
