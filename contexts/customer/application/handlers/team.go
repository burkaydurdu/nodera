package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// The domain events other contexts learn Teams and Projects from
// (docs/adr/0004). Each goes through the outbox after the commit it follows.

//go:generate mockgen -destination=../../mock/producers/team_created.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers ITeamCreatedProducer
type ITeamCreatedProducer interface {
	Publish(ctx context.Context, t *team.Team) error
}

//go:generate mockgen -destination=../../mock/producers/team_member_added.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers ITeamMemberAddedProducer
type ITeamMemberAddedProducer interface {
	Publish(ctx context.Context, m *team.Member) error
}

//go:generate mockgen -destination=../../mock/producers/project_created.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers IProjectCreatedProducer
type IProjectCreatedProducer interface {
	Publish(ctx context.Context, p *project.Project) error
}

type Team struct {
	log            logger.ILog
	teams          team.IService
	projects       project.IService
	transactions   postgresql.ITransactionManager
	teamCreated    ITeamCreatedProducer
	memberAdded    ITeamMemberAddedProducer
	projectCreated IProjectCreatedProducer
}

func NewTeam(
	log logger.ILog,
	teams team.IService,
	projects project.IService,
	transactions postgresql.ITransactionManager,
	teamCreated ITeamCreatedProducer,
	memberAdded ITeamMemberAddedProducer,
	projectCreated IProjectCreatedProducer,
) *Team {
	return &Team{
		log:            log,
		teams:          teams,
		projects:       projects,
		transactions:   transactions,
		teamCreated:    teamCreated,
		memberAdded:    memberAdded,
		projectCreated: projectCreated,
	}
}

// Create opens a new Team with the caller as its owner, in one transaction
// with the membership so a Team can never exist without a member.
func (h *Team) Create(ctx context.Context, req *team.CreateRequest) (*dto.Team, error) {
	userID := contextutil.CurrentUserID(ctx)

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}

	newTeam := &team.Team{Name: req.Name, OwnerUserID: userID}
	if err := h.teams.Create(txCtx, newTeam); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return nil, err
	}
	owner, err := h.teams.AddMember(txCtx, newTeam.ID, userID, team.RoleOwner)
	if err != nil {
		_ = h.transactions.Rollback(txCtx)

		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}

	publishTeamOpened(ctx, h.log, h.teamCreated, h.memberAdded, newTeam, owner)

	return dto.ToTeam(&team.Membership{Team: *newTeam, Role: team.RoleOwner}), nil
}

func (h *Team) ListMine(ctx context.Context) ([]*dto.Team, error) {
	memberships, err := h.teams.FindMembershipsByUserID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}

	return dto.ToTeams(memberships), nil
}

// ListProjects and CreateProject establish membership before touching the
// Team's resources; the Team id is caller-controlled.
func (h *Team) ListProjects(ctx context.Context, req *project.ListRequest) ([]*dto.Project, error) {
	teamID, err := requireMembership(ctx, h.teams, req.TeamID)
	if err != nil {
		return nil, err
	}

	projects, err := h.projects.FindByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return dto.ToProjects(projects), nil
}

func (h *Team) CreateProject(ctx context.Context, req *project.CreateRequest) (*dto.Project, error) {
	teamID, err := requireMembership(ctx, h.teams, req.TeamID)
	if err != nil {
		return nil, err
	}

	p := &project.Project{TeamID: teamID, Name: req.Name, Description: req.Description}
	if err := h.projects.Create(ctx, p); err != nil {
		return nil, err
	}

	if err := h.projectCreated.Publish(ctx, p); err != nil {
		h.log.Error(err)
	}

	return dto.ToProject(p), nil
}

func requireMembership(ctx context.Context, teams team.IService, rawTeamID string) (uuid.UUID, error) {
	teamID := uuid.MustParse(rawTeamID)
	if _, err := teams.FindMembership(ctx, teamID, contextutil.CurrentUserID(ctx)); err != nil {
		return uuid.Nil, err
	}

	return teamID, nil
}

// publishTeamOpened announces a new Team and its owner. The rows are already
// committed, so a failed publish is logged rather than turned into an error
// the caller cannot act on; `replay-projects` is the recovery path.
func publishTeamOpened(ctx context.Context, log logger.ILog, teamCreated ITeamCreatedProducer, memberAdded ITeamMemberAddedProducer, t *team.Team, owner *team.Member) {
	if err := teamCreated.Publish(ctx, t); err != nil {
		log.Error(err)
	}
	if err := memberAdded.Publish(ctx, owner); err != nil {
		log.Error(err)
	}
}
