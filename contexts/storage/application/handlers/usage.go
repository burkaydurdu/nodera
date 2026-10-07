package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/aggregates/usage"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
)

// Usage shows a Storage's size and traffic per day.
type Usage struct {
	authorizer
	usage usage.IService
	now   Clock
}

func NewUsage(accessService access.IService, storageService storage.IService, usageService usage.IService, now Clock) *Usage {
	return &Usage{authorizer: authorizer{access: accessService, storages: storageService}, usage: usageService, now: now}
}

func (h *Usage) Show(ctx context.Context, req *usage.Request) (*dto.Usage, error) {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageRead)
	if err != nil {
		return nil, err
	}
	today := usage.DayOf(h.now())
	days, err := h.usage.ListDays(ctx, s.ID, today.AddDate(0, 0, 1-usage.Days), today.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}

	return dto.ToUsage(s, days), nil
}
