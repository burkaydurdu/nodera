package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
)

// ReplayStatus republishes every Team's current Billing status, so the
// droplet context's read model can be rebuilt or catch up after a lost
// event. The droplet context applies the events last-writer-wins on
// changed_at and the row's own time is sent, so a replay never overrides a
// newer change and running it twice is harmless. A Team without a row is
// no_payment_method, the droplet context's default, and is skipped.
type ReplayStatus struct {
	statuses  billingstatus.IService
	published StatusPublisher
}

func NewReplayStatus(statuses billingstatus.IService, published StatusPublisher) *ReplayStatus {
	return &ReplayStatus{statuses: statuses, published: published}
}

func (h *ReplayStatus) Run(ctx context.Context) (int, error) {
	rows, err := h.statuses.FindAll(ctx)
	if err != nil {
		return 0, err
	}
	for i, st := range rows {
		if err := h.published.Publish(ctx, st.TeamID, string(st.Status), st.ChangedAt.UTC()); err != nil {
			return i, err
		}
	}

	return len(rows), nil
}
