package message

import "strings"

type AddressInput struct {
	Email string `json:"email" validate:"required,max=254"`
	Name  string `json:"name" validate:"max=128"`
}

type AttachmentInput struct {
	Filename    string `json:"filename" validate:"required,max=255"`
	ContentType string `json:"content_type" validate:"required,max=255"`
	// Content is base64; its decoded size counts towards MaxSizeBytes.
	Content   string `json:"content" validate:"required"`
	ContentID string `json:"content_id" validate:"max=255"`
}

// SendRequest is the HTTP sending API's body (POST /v1/mail/messages).
type SendRequest struct {
	IdempotencyKey string            `header:"Idempotency-Key" json:"-" validate:"max=128"`
	From           AddressInput      `json:"from" validate:"required"`
	To             []AddressInput    `json:"to" validate:"max=50,dive"`
	Cc             []AddressInput    `json:"cc" validate:"max=50,dive"`
	Bcc            []AddressInput    `json:"bcc" validate:"max=50,dive"`
	ReplyTo        *AddressInput     `json:"reply_to"`
	Subject        string            `json:"subject" validate:"required,max=998"`
	Text           string            `json:"text"`
	HTML           string            `json:"html"`
	Attachments    []AttachmentInput `json:"attachments" validate:"max=50,dive"`
	Headers        map[string]string `json:"headers" validate:"max=50"`
	Tags           []string          `json:"tags"`
}

func (r *SendRequest) Normalize() {
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	r.Subject = strings.TrimSpace(r.Subject)
	for i := range r.Tags {
		r.Tags[i] = strings.TrimSpace(r.Tags[i])
	}
}

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	Status    string `query:"status" json:"-" validate:"omitempty,oneof=accepted delivered deferred bounced suppressed complained"`
	Recipient string `query:"recipient" json:"-" validate:"max=254"`
	From      string `query:"from" json:"-" validate:"max=254"`
	Tag       string `query:"tag" json:"-" validate:"max=64"`
	Since     string `query:"since" json:"-" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	Until     string `query:"until" json:"-" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	Cursor    string `query:"cursor" json:"-" validate:"max=200"`
	Limit     int    `query:"limit" json:"-" validate:"omitempty,min=1,max=100"`
}

func (r *ListRequest) Normalize() {
	r.Recipient = strings.ToLower(strings.TrimSpace(r.Recipient))
	r.From = strings.ToLower(strings.TrimSpace(r.From))
	r.Tag = strings.TrimSpace(r.Tag)
}

type GetRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	MessageID string `param:"messageID" json:"-" validate:"required,uuid"`
}
