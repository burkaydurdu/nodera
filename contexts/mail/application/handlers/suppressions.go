package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/suppression"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
)

type Suppressions struct {
	authorizer
	suppressions suppression.IService
}

func NewSuppressions(accessService access.IService, suppressions suppression.IService) *Suppressions {
	return &Suppressions{authorizer: authorizer{access: accessService}, suppressions: suppressions}
}

func (h *Suppressions) List(ctx context.Context, req *suppression.ListRequest) (*dto.SuppressionPage, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultPageSize
	}
	before, err := pageBefore(req.Cursor, suppression.ErrorInvalidCursor)
	if err != nil {
		return nil, err
	}

	list, next, err := h.suppressions.List(ctx, projectID, req.Search, before, limit)
	if err != nil {
		return nil, err
	}

	return dto.ToSuppressionPage(list, nextCursor(next)), nil
}

// Delete lets a Team mail an address again once it is fixed; a complaint is
// the recipient's own wish and stays.
func (h *Suppressions) Delete(ctx context.Context, req *suppression.DeleteRequest) error {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailWrite); err != nil {
		return err
	}

	s, err := h.suppressions.Find(ctx, projectID, uuid.MustParse(req.SuppressionID))
	if err != nil {
		return err
	}
	if !s.Removable() {
		return suppression.ErrorComplaintKept
	}

	return h.suppressions.Delete(ctx, projectID, s.ID)
}
