package webhook

import "strings"

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}

type CreateRequest struct {
	ProjectID string   `param:"projectID" json:"-" validate:"required,uuid"`
	URL       string   `json:"url" validate:"required,max=2048"`
	Events    []string `json:"events" validate:"required,min=1,max=6,dive,oneof=accepted delivered deferred bounced suppressed complained"`
}

func (r *CreateRequest) Normalize() {
	r.URL = strings.TrimSpace(r.URL)
}

type UpdateRequest struct {
	ProjectID string   `param:"projectID" json:"-" validate:"required,uuid"`
	WebhookID string   `param:"webhookID" json:"-" validate:"required,uuid"`
	URL       string   `json:"url" validate:"required,max=2048"`
	Events    []string `json:"events" validate:"required,min=1,max=6,dive,oneof=accepted delivered deferred bounced suppressed complained"`
	Active    *bool    `json:"active" validate:"required"`
}

func (r *UpdateRequest) Normalize() {
	r.URL = strings.TrimSpace(r.URL)
}

type GetRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	WebhookID string `param:"webhookID" json:"-" validate:"required,uuid"`
}

type DeliveriesRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	WebhookID string `param:"webhookID" json:"-" validate:"required,uuid"`
	Cursor    string `query:"cursor" json:"-" validate:"max=200"`
	Limit     int    `query:"limit" json:"-" validate:"omitempty,min=1,max=100"`
}
