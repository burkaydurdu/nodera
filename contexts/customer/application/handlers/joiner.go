package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Joiner turns a pending Invitation into a membership. Accept by token,
// accept by id and the verification of a User who signed up from an
// Invitation all end here, so the three paths cannot drift apart.
type Joiner struct {
	log          logger.ILog
	teams        team.IService
	invitations  invitation.IService
	transactions postgresql.ITransactionManager
	permVersions IPermissionVersions
	memberAdded  ITeamMemberAddedProducer
}

func NewJoiner(
	log logger.ILog,
	teams team.IService,
	invitations invitation.IService,
	transactions postgresql.ITransactionManager,
	permVersions IPermissionVersions,
	memberAdded ITeamMemberAddedProducer,
) *Joiner {
	return &Joiner{log: log, teams: teams, invitations: invitations, transactions: transactions, permVersions: permVersions, memberAdded: memberAdded}
}

// Accept seats the User with the offered Role and closes the Invitation in
// one transaction. A User who is already a member keeps their Role and the
// Invitation is still marked accepted: the offer was answered, not refused.
func (j *Joiner) Accept(ctx context.Context, inv *invitation.Invitation, u *user.User) (*team.Membership, error) {
	if err := requireAnswerable(inv, u); err != nil {
		return nil, err
	}

	existing, err := j.teams.FindMembership(ctx, inv.TeamID, u.ID)
	if err == nil {
		if err := j.invitations.Respond(ctx, inv.ID, invitation.StatusAccepted, &u.ID); err != nil {
			return nil, err
		}

		return existing, nil
	}
	if !errors.Is(err, team.ErrorNotMember) {
		return nil, err
	}

	txCtx, err := j.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// Closing the offer first is what makes two accepts racing safe: the
	// conditional update lets exactly one of them through, and the loser is
	// told the offer is gone before any membership is written.
	if err := j.invitations.Respond(txCtx, inv.ID, invitation.StatusAccepted, &u.ID); err != nil {
		_ = j.transactions.Rollback(txCtx)

		return nil, err
	}
	member, err := j.teams.AddMember(txCtx, inv.TeamID, u.ID, team.Role(inv.Role))
	if err != nil {
		_ = j.transactions.Rollback(txCtx)

		return nil, err
	}
	if err := j.transactions.Commit(txCtx); err != nil {
		return nil, err
	}

	bumpPermissionVersion(ctx, j.log, j.permVersions, u.ID)
	if err := j.memberAdded.Publish(ctx, member); err != nil {
		j.log.Error(err)
	}

	return j.teams.FindMembership(ctx, inv.TeamID, u.ID)
}

// Decline closes the offer without a membership.
func (j *Joiner) Decline(ctx context.Context, inv *invitation.Invitation, u *user.User) error {
	if err := requireAnswerable(inv, u); err != nil {
		return err
	}

	return j.invitations.Respond(ctx, inv.ID, invitation.StatusDeclined, nil)
}

// requireAnswerable is the order the spec fixes: a dead offer is reported
// before anything about the caller, so a stale link tells nobody whose it was.
func requireAnswerable(inv *invitation.Invitation, u *user.User) error {
	if !inv.IsPending(time.Now()) {
		return invitation.ErrorNoLongerValid
	}
	if inv.Email != user.NormalizeEmail(u.Email) {
		return invitation.ErrorForAnotherEmail
	}

	return nil
}
