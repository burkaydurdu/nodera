package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/utils/hash"
)

//go:generate mockgen -destination=../../mock/producers/user_profile_updated.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/application/handlers IUserProfileUpdatedProducer
type IUserProfileUpdatedProducer interface {
	Publish(ctx context.Context, u *user.User, updatedAt time.Time) error
}

type User struct {
	log            logger.ILog
	users          user.IService
	teams          team.IService
	sessions       session.IManager
	profileUpdated IUserProfileUpdatedProducer
}

func NewUser(log logger.ILog, users user.IService, teams team.IService, sessions session.IManager, profileUpdated IUserProfileUpdatedProducer) *User {
	return &User{log: log, users: users, teams: teams, sessions: sessions, profileUpdated: profileUpdated}
}

func (h *User) GetMe(ctx context.Context) (*dto.User, error) {
	u, err := h.users.FindByID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}

	return h.withTeam(ctx, u)
}

func (h *User) UpdateMe(ctx context.Context, req *user.UpdateMeRequest) (*dto.User, error) {
	userID := contextutil.CurrentUserID(ctx)
	if err := h.users.UpdateProfile(ctx, userID, req.FullName, user.Locale(req.Locale), req.Country); err != nil {
		return nil, err
	}

	u, err := h.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	// The profile is saved; a failed publish leaves the read models with the
	// old name until the next change, which is logged rather than refused.
	if err := h.profileUpdated.Publish(ctx, u, time.Now()); err != nil {
		h.log.Error(err)
	}

	return h.withTeam(ctx, u)
}

// ChangePassword keeps the session it was called from and ends the others.
func (h *User) ChangePassword(ctx context.Context, req *user.ChangePasswordRequest) error {
	u, err := h.requirePassword(ctx, req.CurrentPassword)
	if err != nil {
		return err
	}

	passwordHash, err := hash.GeneratePasswordHash(req.NewPassword)
	if err != nil {
		return user.ErrorCannotUpdate.Copy().SetError(err)
	}

	if err := h.users.UpdatePassword(ctx, u.ID, passwordHash); err != nil {
		return err
	}

	return h.sessions.RevokeAllExcept(ctx, u.ID, contextutil.CurrentSessionID(ctx))
}

func (h *User) UpdateMFA(ctx context.Context, req *user.UpdateMFARequest) (*dto.User, error) {
	u, err := h.requirePassword(ctx, req.Password)
	if err != nil {
		return nil, err
	}
	if u.MFAMethod == req.Method {
		return nil, user.ErrorMFAMethodAlreadySet
	}

	if err := h.users.UpdateMFAMethod(ctx, u.ID, req.Method); err != nil {
		return nil, err
	}
	u.MFAMethod = req.Method

	return h.withTeam(ctx, u)
}

func (h *User) ListSessions(ctx context.Context) ([]*dto.SessionItem, error) {
	sessions, err := h.sessions.List(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}

	return dto.ToSessionItems(sessions, contextutil.CurrentSessionID(ctx)), nil
}

// DeleteSession ends one of the caller's own sessions; a session id that is
// not theirs is a 404, never a hint that it exists.
func (h *User) DeleteSession(ctx context.Context, req *user.DeleteSessionRequest) error {
	userID := contextutil.CurrentUserID(ctx)
	sessionID := uuid.MustParse(req.SessionID)

	sessions, err := h.sessions.List(ctx, userID)
	if err != nil {
		return err
	}

	for _, s := range sessions {
		if s.ID == sessionID {
			return h.sessions.Revoke(ctx, userID, sessionID)
		}
	}

	return session.ErrorNotFound
}

func (h *User) requirePassword(ctx context.Context, password string) (*user.User, error) {
	u, err := h.users.FindByID(ctx, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if !hash.ComparePasswordHash(password, u.Password) {
		return nil, user.ErrorIncorrectPassword
	}

	return u, nil
}

func (h *User) withTeam(ctx context.Context, u *user.User) (*dto.User, error) {
	memberships, err := h.teams.FindMembershipsByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	var first *team.Membership
	if len(memberships) > 0 {
		first = memberships[0]
	}

	return dto.ToUser(u, first), nil
}
