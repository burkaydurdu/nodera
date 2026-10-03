package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/dto"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/utils/str"
)

type liveRead struct {
	NotificationIDs []uuid.UUID `json:"notification_ids,omitempty"`
	All             bool        `json:"all,omitempty"`
}

// Inbox is the caller's own Inbox across every Team they are in. A Team's
// Notifications show only while the caller is still a member: someone removed
// stops seeing what happened to Droplets they can no longer see.
type Inbox struct {
	log           logger.ILog
	notifications notification.IService
	access        access.IService
	realtime      IRealtimePublisher
	now           func() time.Time
}

func NewInbox(log logger.ILog, notifications notification.IService, accessService access.IService, publisher IRealtimePublisher, now func() time.Time) *Inbox {
	return &Inbox{log: log, notifications: notifications, access: accessService, realtime: publisher, now: now}
}

func (h *Inbox) List(ctx context.Context, req *notification.ListRequest) ([]*dto.Notification, error) {
	userID := contextutil.CurrentUserID(ctx)
	teams, err := h.access.TeamsOf(ctx, userID)
	if err != nil {
		return nil, err
	}

	var before *uuid.UUID
	if str.IsNotEmpty(req.Before) {
		id := uuid.MustParse(req.Before)
		before = &id
	}

	ns, err := h.notifications.ListInbox(ctx, userID, teams, before, req.PageSize())
	if err != nil {
		return nil, err
	}

	return dto.ToNotifications(ns), nil
}

func (h *Inbox) UnreadCount(ctx context.Context) (*dto.UnreadCount, error) {
	userID := contextutil.CurrentUserID(ctx)
	teams, err := h.access.TeamsOf(ctx, userID)
	if err != nil {
		return nil, err
	}

	count, err := h.notifications.CountUnread(ctx, userID, teams)
	if err != nil {
		return nil, err
	}

	return &dto.UnreadCount{Count: count}, nil
}

func (h *Inbox) Read(ctx context.Context, req *notification.ReadRequest) (*dto.Notification, error) {
	userID := contextutil.CurrentUserID(ctx)
	teams, err := h.access.TeamsOf(ctx, userID)
	if err != nil {
		return nil, err
	}

	n, err := h.notifications.MarkRead(ctx, userID, teams, uuid.MustParse(req.NotificationID), h.now())
	if err != nil {
		return nil, err
	}
	h.tellOtherTabs(ctx, userID, liveRead{NotificationIDs: []uuid.UUID{n.ID}})

	return dto.ToNotification(n), nil
}

func (h *Inbox) ReadAll(ctx context.Context) (*dto.ReadAll, error) {
	userID := contextutil.CurrentUserID(ctx)
	teams, err := h.access.TeamsOf(ctx, userID)
	if err != nil {
		return nil, err
	}

	ids, err := h.notifications.MarkAllRead(ctx, userID, teams, h.now())
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		h.tellOtherTabs(ctx, userID, liveRead{All: true})
	}

	return &dto.ReadAll{Count: len(ids)}, nil
}

// tellOtherTabs is best effort: the read is committed, and a tab that misses
// it catches up on its next refetch.
func (h *Inbox) tellOtherTabs(ctx context.Context, userID uuid.UUID, read liveRead) {
	message, err := realtime.New(realtime.ToUser(userID), LiveNotificationRead, read, h.now())
	if err == nil {
		err = h.realtime.Publish(ctx, message)
	}
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("user", userID.String()))
	}
}
