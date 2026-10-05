package handlers

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/contexts/mail/secrets"
	"github.com/piggystats/nodera/pkg/authz"
)

type Webhooks struct {
	authorizer
	webhooks webhook.IService
	sealer   *secrets.Sealer
	// allowLocal lets development and test point Webhooks at loopback.
	allowLocal bool
	now        Clock
}

func NewWebhooks(accessService access.IService, webhooks webhook.IService, sealer *secrets.Sealer, allowLocal bool, now Clock) *Webhooks {
	return &Webhooks{authorizer: authorizer{access: accessService}, webhooks: webhooks, sealer: sealer, allowLocal: allowLocal, now: now}
}

func (h *Webhooks) List(ctx context.Context, req *webhook.ListRequest) ([]*dto.Webhook, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	list, err := h.webhooks.List(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return dto.ToWebhooks(list), nil
}

// Create answers the signing secret this once; Nodera keeps it sealed.
func (h *Webhooks) Create(ctx context.Context, req *webhook.CreateRequest) (*dto.Webhook, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailWrite); err != nil {
		return nil, err
	}
	if err := webhook.ValidateURL(req.URL, h.allowLocal); err != nil {
		return nil, err
	}

	count, err := h.webhooks.Count(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if count >= webhook.MaxPerProject {
		return nil, webhook.ErrorTooMany
	}

	secret, err := webhook.NewSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := h.sealer.Seal([]byte(secret))
	if err != nil {
		return nil, err
	}

	now := h.now()
	w := &webhook.Webhook{ID: uuid.New(), ProjectID: projectID, URL: req.URL, Events: events(req.Events), Active: true, Secret: sealed, CreatedAt: now, UpdatedAt: now}
	if err := h.webhooks.Create(ctx, w); err != nil {
		return nil, err
	}

	out := dto.ToWebhook(w)
	out.Secret = secret

	return out, nil
}

func (h *Webhooks) Update(ctx context.Context, req *webhook.UpdateRequest) (*dto.Webhook, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailWrite); err != nil {
		return nil, err
	}
	if err := webhook.ValidateURL(req.URL, h.allowLocal); err != nil {
		return nil, err
	}

	w, err := h.webhooks.Find(ctx, projectID, uuid.MustParse(req.WebhookID))
	if err != nil {
		return nil, err
	}
	w.URL, w.Events, w.Active, w.UpdatedAt = req.URL, events(req.Events), *req.Active, h.now()
	if err := h.webhooks.Update(ctx, w); err != nil {
		return nil, err
	}

	return dto.ToWebhook(w), nil
}

func (h *Webhooks) Delete(ctx context.Context, req *webhook.GetRequest) error {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailWrite); err != nil {
		return err
	}

	return h.webhooks.Delete(ctx, projectID, uuid.MustParse(req.WebhookID))
}

func (h *Webhooks) Deliveries(ctx context.Context, req *webhook.DeliveriesRequest) (*dto.DeliveryPage, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	w, err := h.webhooks.Find(ctx, projectID, uuid.MustParse(req.WebhookID))
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultPageSize
	}
	before, err := pageBefore(req.Cursor, webhook.ErrorInvalidCursor)
	if err != nil {
		return nil, err
	}

	list, next, err := h.webhooks.ListDeliveries(ctx, w.ID, before, limit)
	if err != nil {
		return nil, err
	}

	return dto.ToDeliveryPage(list, nextCursor(next)), nil
}

// events keeps the Message events in their usual order, each once.
func events(requested []string) []string {
	out := []string{}
	for _, t := range message.EventTypes {
		if slices.Contains(requested, string(t)) {
			out = append(out, string(t))
		}
	}

	return out
}
