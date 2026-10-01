package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
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

//go:generate mockgen -destination=../../mock/producers/team_member_role_changed.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers ITeamMemberRoleChangedProducer
type ITeamMemberRoleChangedProducer interface {
	Publish(ctx context.Context, m *team.Member, changedAt time.Time) error
}

//go:generate mockgen -destination=../../mock/producers/team_member_removed.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers ITeamMemberRemovedProducer
type ITeamMemberRemovedProducer interface {
	Publish(ctx context.Context, m *team.Member, removedAt time.Time) error
}

//go:generate mockgen -destination=../../mock/producers/project_created.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers IProjectCreatedProducer
type IProjectCreatedProducer interface {
	Publish(ctx context.Context, p *project.Project) error
}

// IPermissionVersions is the per-User counter the access token carries as
// `pv` (docs/adr/0005); every membership change bumps the affected User's.
//
//go:generate mockgen -destination=../../mock/services/permversion.go -package=mockservices -mock_names=IPermissionVersions=MockIPermissionVersions github.com/piggystats/nodera/contexts/customer/application/handlers IPermissionVersions
type IPermissionVersions interface {
	Bump(ctx context.Context, userID uuid.UUID) error
	Current(ctx context.Context, userID uuid.UUID) (int64, error)
}

type Team struct {
	log            logger.ILog
	teams          team.IService
	projects       project.IService
	transactions   postgresql.ITransactionManager
	permVersions   IPermissionVersions
	teamCreated    ITeamCreatedProducer
	memberAdded    ITeamMemberAddedProducer
	roleChanged    ITeamMemberRoleChangedProducer
	memberRemoved  ITeamMemberRemovedProducer
	projectCreated IProjectCreatedProducer
}

func NewTeam(
	log logger.ILog,
	teams team.IService,
	projects project.IService,
	transactions postgresql.ITransactionManager,
	permVersions IPermissionVersions,
	teamCreated ITeamCreatedProducer,
	memberAdded ITeamMemberAddedProducer,
	roleChanged ITeamMemberRoleChangedProducer,
	memberRemoved ITeamMemberRemovedProducer,
	projectCreated IProjectCreatedProducer,
) *Team {
	return &Team{
		log:            log,
		teams:          teams,
		projects:       projects,
		transactions:   transactions,
		permVersions:   permVersions,
		teamCreated:    teamCreated,
		memberAdded:    memberAdded,
		roleChanged:    roleChanged,
		memberRemoved:  memberRemoved,
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
	bumpPermissionVersion(ctx, h.log, h.permVersions, userID)

	return dto.ToTeam(&team.Membership{Team: *newTeam, Role: team.RoleOwner}), nil
}

func (h *Team) ListMine(ctx context.Context) ([]*dto.Team, error) {
	memberships, err := h.teams.FindMembershipsByUserID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}

	return dto.ToTeams(memberships), nil
}

// ListProjects and CreateProject establish the caller's Role before touching
// the Team's resources; the Team id is caller-controlled.
func (h *Team) ListProjects(ctx context.Context, req *project.ListRequest) ([]*dto.Project, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamRead)
	if err != nil {
		return nil, err
	}

	projects, err := h.projects.FindByTeamID(ctx, m.Team.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToProjects(projects), nil
}

func (h *Team) CreateProject(ctx context.Context, req *project.CreateRequest) (*dto.Project, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.ProjectWrite)
	if err != nil {
		return nil, err
	}

	p := &project.Project{TeamID: m.Team.ID, Name: req.Name, Description: req.Description}
	if err := h.projects.Create(ctx, p); err != nil {
		return nil, err
	}

	if err := h.projectCreated.Publish(ctx, p); err != nil {
		h.log.Error(err)
	}

	return dto.ToProject(p), nil
}

func (h *Team) ListMembers(ctx context.Context, req *team.ListMembersRequest) ([]*dto.Member, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamRead)
	if err != nil {
		return nil, err
	}

	members, err := h.teams.FindMembers(ctx, m.Team.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToMembers(members), nil
}

// ChangeMemberRole refuses the caller's own Role (another owner must do it)
// and the demotion of the last owner, then bumps the target's permission
// version so their next request refreshes into the new Role.
func (h *Team) ChangeMemberRole(ctx context.Context, req *team.ChangeRoleRequest) (*dto.Member, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamManage)
	if err != nil {
		return nil, err
	}
	targetID := uuid.MustParse(req.UserID)
	if targetID == contextutil.CurrentUserID(ctx) {
		return nil, team.ErrorOwnRole
	}
	role := team.Role(req.Role)

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.teams.Lock(txCtx, m.Team.ID); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return nil, err
	}

	target, err := h.teams.FindMember(txCtx, m.Team.ID, targetID)
	if err != nil {
		_ = h.transactions.Rollback(txCtx)

		return nil, err
	}
	if target.Role == team.RoleOwner && role != team.RoleOwner {
		if err := h.requireAnotherOwner(txCtx, m.Team.ID); err != nil {
			_ = h.transactions.Rollback(txCtx)

			return nil, err
		}
	}
	if err := h.teams.UpdateMemberRole(txCtx, m.Team.ID, targetID, role); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}

	target.Role = role
	bumpPermissionVersion(ctx, h.log, h.permVersions, targetID)
	if err := h.roleChanged.Publish(ctx, &target.Member, time.Now()); err != nil {
		h.log.Error(err)
	}

	return dto.ToMember(target), nil
}

// RemoveMember takes a member out of the Team. Removing oneself is leaving,
// with the same last-owner protection.
func (h *Team) RemoveMember(ctx context.Context, req *team.RemoveMemberRequest) error {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamManage)
	if err != nil {
		return err
	}

	return h.removeMembership(ctx, m.Team.ID, uuid.MustParse(req.UserID))
}

func (h *Team) Leave(ctx context.Context, req *team.LeaveRequest) error {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamRead)
	if err != nil {
		return err
	}

	return h.removeMembership(ctx, m.Team.ID, contextutil.CurrentUserID(ctx))
}

func (h *Team) removeMembership(ctx context.Context, teamID, userID uuid.UUID) error {
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	if err := h.teams.Lock(txCtx, teamID); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}

	target, err := h.teams.FindMember(txCtx, teamID, userID)
	if err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}
	if target.Role == team.RoleOwner {
		if err := h.requireAnotherOwner(txCtx, teamID); err != nil {
			_ = h.transactions.Rollback(txCtx)

			return err
		}
	}
	if err := h.teams.RemoveMember(txCtx, teamID, userID); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}

	bumpPermissionVersion(ctx, h.log, h.permVersions, userID)
	if err := h.memberRemoved.Publish(ctx, &target.Member, time.Now()); err != nil {
		h.log.Error(err)
	}

	return nil
}

// requireAnotherOwner runs inside the transaction that is about to take an
// owner away, after the Team row is locked, so the count it reads is the one
// the commit will act on.
func (h *Team) requireAnotherOwner(txCtx context.Context, teamID uuid.UUID) error {
	owners, err := h.teams.CountOwners(txCtx, teamID)
	if err != nil {
		return err
	}
	if owners <= 1 {
		return team.ErrorLastOwner
	}

	return nil
}

// requirePermission is the service-side gate every Team route passes: a
// stranger sees the same not-found as for any other Team route, a member
// whose Role lacks the Permission is told so.
func requirePermission(ctx context.Context, teams team.IService, rawTeamID string, permission authz.Permission) (*team.Membership, error) {
	m, err := teams.FindMembership(ctx, uuid.MustParse(rawTeamID), contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if !m.Role.Allows(permission) {
		return nil, team.ErrorForbidden
	}

	return m, nil
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

// bumpPermissionVersion follows a committed membership change. A Redis miss
// is logged, not returned: the row is already true, and the gateway reads a
// version that is at worst one refresh late.
func bumpPermissionVersion(ctx context.Context, log logger.ILog, versions IPermissionVersions, userID uuid.UUID) {
	if err := versions.Bump(ctx, userID); err != nil {
		log.Error(err)
	}
}
