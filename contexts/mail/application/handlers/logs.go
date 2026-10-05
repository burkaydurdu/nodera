package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

const defaultPageSize = 50

// Logs is a Project's Messages of the last 30 days.
type Logs struct {
	authorizer
	messages message.IService
	now      Clock
}

func NewLogs(accessService access.IService, messages message.IService, now Clock) *Logs {
	return &Logs{authorizer: authorizer{access: accessService}, messages: messages, now: now}
}

func (h *Logs) List(ctx context.Context, req *message.ListRequest) (*dto.MessagePage, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	filter := message.Filter{
		ProjectID: projectID, Recipient: req.Recipient, From: req.From, Tag: req.Tag,
		Since: h.now().Add(-message.Retention), Limit: req.Limit,
	}
	if filter.Limit == 0 {
		filter.Limit = defaultPageSize
	}
	if str.IsNotEmpty(req.Status) {
		status := message.EventType(req.Status)
		filter.Status = &status
	}
	// A since older than retention finds nothing more; it is clamped.
	if str.IsNotEmpty(req.Since) {
		since, _ := time.Parse(time.RFC3339, req.Since)
		if since.After(filter.Since) {
			filter.Since = since
		}
	}
	if str.IsNotEmpty(req.Until) {
		until, _ := time.Parse(time.RFC3339, req.Until)
		filter.Until = &until
	}
	cursor, err := pageBefore(req.Cursor, message.ErrorInvalidCursor)
	if err != nil {
		return nil, err
	}
	filter.Cursor = cursor

	list, next, err := h.messages.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	return dto.ToMessagePage(list, next), nil
}

func (h *Logs) Get(ctx context.Context, req *message.GetRequest) (*dto.MessageDetail, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	m, err := h.messages.Find(ctx, projectID, uuid.MustParse(req.MessageID))
	if err != nil {
		return nil, err
	}

	return dto.ToMessageDetail(m), nil
}
