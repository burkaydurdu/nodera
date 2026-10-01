package handlers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/contexts/customer/async/events"
	"github.com/piggystats/nodera/contexts/customer/config"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/pkg/ratelimit"
)

// Budgets against using Invitations to spam addresses: per Team, per inviter
// and, for the public-facing token routes, per source address.
var (
	ruleInviteTeam    = ratelimit.Rule{Window: time.Hour, Limit: 20}
	ruleInviteInviter = ratelimit.Rule{Window: 24 * time.Hour, Limit: 50}
	ruleInviteTokenIP = ratelimit.Rule{Window: 10 * time.Minute, Limit: 20}
)

//go:generate mockgen -destination=../../mock/producers/team_invitation.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers ITeamInvitationProducer
type ITeamInvitationProducer interface {
	Publish(ctx context.Context, body events.TeamInvitationBody) error
}

type Invitation struct {
	log         logger.ILog
	config      config.Auth
	users       user.IService
	teams       team.IService
	invitations invitation.IService
	limiter     ratelimit.ILimiter
	joiner      *Joiner
	mailer      ITeamInvitationProducer
}

func NewInvitation(
	log logger.ILog,
	cfg config.Auth,
	users user.IService,
	teams team.IService,
	invitations invitation.IService,
	limiter ratelimit.ILimiter,
	joiner *Joiner,
	mailer ITeamInvitationProducer,
) *Invitation {
	return &Invitation{log: log, config: cfg, users: users, teams: teams, invitations: invitations, limiter: limiter, joiner: joiner, mailer: mailer}
}

// Create offers a Role to an address. A current member and an open offer are
// both conflicts the owner is told about, so they do not send duplicates.
func (h *Invitation) Create(ctx context.Context, req *invitation.CreateRequest) (*dto.Invitation, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamManage)
	if err != nil {
		return nil, err
	}
	inviterID := contextutil.CurrentUserID(ctx)
	if err := h.limiter.Allow(ctx, "invitation:team", m.Team.ID.String(), ruleInviteTeam); err != nil {
		return nil, err
	}
	if err := h.limiter.Allow(ctx, "invitation:inviter", inviterID.String(), ruleInviteInviter); err != nil {
		return nil, err
	}

	invitee, err := h.users.FindByEmail(ctx, req.Email)
	if err != nil && !errors.Is(err, user.ErrorNotFound) {
		return nil, err
	}
	if invitee != nil {
		if _, err := h.teams.FindMembership(ctx, m.Team.ID, invitee.ID); err == nil {
			return nil, invitation.ErrorAlreadyMember
		} else if !errors.Is(err, team.ErrorNotMember) {
			return nil, err
		}
	}

	if err := h.invitations.ExpireStale(ctx, m.Team.ID, req.Email); err != nil {
		return nil, err
	}

	rawToken, tokenHash, err := invitation.NewToken()
	if err != nil {
		return nil, err
	}
	inv := &invitation.Invitation{
		TeamID:    m.Team.ID,
		Email:     req.Email,
		Role:      authz.Role(req.Role),
		InvitedBy: inviterID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(invitation.TTL),
	}
	if err := h.invitations.Create(ctx, inv); err != nil {
		return nil, err
	}

	// Re-read for the joined names the response and the email both carry.
	created, err := h.invitations.FindByID(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	h.sendEmail(ctx, created, invitee, rawToken)

	return dto.ToInvitation(created, time.Now()), nil
}

// Resend replaces the token, so a lost email is not a dead end and the old
// link stops working, under a short cooldown per Invitation.
func (h *Invitation) Resend(ctx context.Context, req *invitation.TeamScopedRequest) (*dto.Invitation, error) {
	inv, err := h.findInTeam(ctx, req)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if !inv.IsPending(now) {
		return nil, invitation.ErrorNoLongerValid
	}
	if wait := inv.ResendAvailableIn(now); wait > 0 {
		return nil, invitation.ErrorResendCooldown.Copy().Add("retry_after_seconds", int(math.Ceil(wait.Seconds())))
	}

	rawToken, tokenHash, err := invitation.NewToken()
	if err != nil {
		return nil, err
	}
	if err := h.invitations.Reissue(ctx, inv.ID, tokenHash, now.Add(invitation.TTL)); err != nil {
		return nil, err
	}

	reissued, err := h.invitations.FindByID(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	invitee, err := h.users.FindByEmail(ctx, reissued.Email)
	if err != nil && !errors.Is(err, user.ErrorNotFound) {
		return nil, err
	}
	h.sendEmail(ctx, reissued, invitee, rawToken)

	return dto.ToInvitation(reissued, time.Now()), nil
}

func (h *Invitation) Revoke(ctx context.Context, req *invitation.TeamScopedRequest) error {
	inv, err := h.findInTeam(ctx, req)
	if err != nil {
		return err
	}
	if !inv.IsPending(time.Now()) {
		return invitation.ErrorNoLongerValid
	}

	return h.invitations.Respond(ctx, inv.ID, invitation.StatusRevoked, nil)
}

func (h *Invitation) ListForTeam(ctx context.Context, req *invitation.ListRequest) ([]*dto.Invitation, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamManage)
	if err != nil {
		return nil, err
	}

	invitations, err := h.invitations.FindByTeamID(ctx, m.Team.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToInvitations(invitations, time.Now()), nil
}

// ListMine answers the open offers to the caller's address, read from the
// User row rather than the token so a stale claim cannot widen it.
func (h *Invitation) ListMine(ctx context.Context) ([]*dto.Invitation, error) {
	u, err := h.users.FindByID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}

	invitations, err := h.invitations.FindPendingByEmail(ctx, user.NormalizeEmail(u.Email))
	if err != nil {
		return nil, err
	}

	return dto.ToInvitations(invitations, time.Now()), nil
}

func (h *Invitation) AcceptByToken(ctx context.Context, req *invitation.TokenRequest) (*dto.AcceptedInvitation, error) {
	inv, err := h.findByToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}

	return h.accept(ctx, inv)
}

func (h *Invitation) DeclineByToken(ctx context.Context, req *invitation.TokenRequest) error {
	inv, err := h.findByToken(ctx, req.Token)
	if err != nil {
		return err
	}

	return h.decline(ctx, inv)
}

func (h *Invitation) AcceptByID(ctx context.Context, req *invitation.ByIDRequest) (*dto.AcceptedInvitation, error) {
	inv, err := h.invitations.FindByID(ctx, uuid.MustParse(req.InvitationID))
	if err != nil {
		return nil, err
	}

	return h.accept(ctx, inv)
}

func (h *Invitation) DeclineByID(ctx context.Context, req *invitation.ByIDRequest) error {
	inv, err := h.invitations.FindByID(ctx, uuid.MustParse(req.InvitationID))
	if err != nil {
		return err
	}

	return h.decline(ctx, inv)
}

// Lookup is the token-scoped public read behind the accept page. The token
// is the whole secret: whoever holds the link is the invitee. It shares the
// token routes' per-address budget, being the one of them that needs no
// session.
func (h *Invitation) Lookup(ctx context.Context, req *invitation.LookupRequest) (*dto.InvitationLookup, error) {
	inv, err := h.findByToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}

	return dto.ToInvitationLookup(inv, time.Now()), nil
}

func (h *Invitation) accept(ctx context.Context, inv *invitation.Invitation) (*dto.AcceptedInvitation, error) {
	u, err := h.users.FindByID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}

	membership, err := h.joiner.Accept(ctx, inv, u)
	if err != nil {
		return nil, err
	}

	return &dto.AcceptedInvitation{Team: dto.ToTeam(membership)}, nil
}

func (h *Invitation) decline(ctx context.Context, inv *invitation.Invitation) error {
	u, err := h.users.FindByID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return err
	}

	return h.joiner.Decline(ctx, inv, u)
}

// findByToken guards the token routes per source address: a token is
// unguessable, but a loop of guesses should still cost its owner.
func (h *Invitation) findByToken(ctx context.Context, rawToken string) (*invitation.Invitation, error) {
	if err := h.limiter.Allow(ctx, "invitation:token:ip", contextutil.ClientIP(ctx), ruleInviteTokenIP); err != nil {
		return nil, err
	}

	return h.invitations.FindByTokenHash(ctx, invitation.HashToken(rawToken))
}

// findInTeam resolves an Invitation for an owner of its Team; one that
// belongs to another Team is not found, whatever the caller owns elsewhere.
func (h *Invitation) findInTeam(ctx context.Context, req *invitation.TeamScopedRequest) (*invitation.Invitation, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamManage)
	if err != nil {
		return nil, err
	}

	inv, err := h.invitations.FindByID(ctx, uuid.MustParse(req.InvitationID))
	if err != nil {
		return nil, err
	}
	if inv.TeamID != m.Team.ID {
		return nil, invitation.ErrorNotFound
	}

	return inv, nil
}

// sendEmail queues the Invitation email. The row is committed, so a failed
// publish is logged and the owner's Resend is the recovery path. The
// recipient is the invitee's account when one exists, otherwise the bare
// address in the inviter's Locale.
func (h *Invitation) sendEmail(ctx context.Context, inv *invitation.Invitation, invitee *user.User, rawToken string) {
	inviter, err := h.users.FindByID(ctx, inv.InvitedBy)
	if err != nil {
		h.log.Error(err)

		return
	}

	recipient := events.InvitationRecipient{Email: inv.Email, Locale: string(inviter.Locale)}
	if invitee != nil {
		recipient = events.InvitationRecipient{UserID: invitee.ID, Email: invitee.Email, FullName: invitee.FullName, Locale: string(invitee.Locale)}
	}

	err = h.mailer.Publish(ctx, events.TeamInvitationBody{
		Recipient:   recipient,
		TeamName:    inv.TeamName,
		Role:        string(inv.Role),
		InviterName: inv.InviterName,
		AcceptURL:   fmt.Sprintf("%s/invitations/accept?token=%s", h.config.FrontURL, rawToken),
		ExpiresAt:   inv.ExpiresAt,
	})
	if err != nil {
		h.log.Error(err)
	}
}
