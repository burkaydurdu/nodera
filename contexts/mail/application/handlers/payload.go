package handlers

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
)

// webhookEvent is one Message event as a Webhook receives it.
type webhookEvent struct {
	ID                   uuid.UUID     `json:"id"`
	Type                 string        `json:"type"`
	MessageID            uuid.UUID     `json:"message_id"`
	Recipient            string        `json:"recipient"`
	OccurredAt           time.Time     `json:"occurred_at"`
	Tags                 []string      `json:"tags"`
	Response             *dto.Response `json:"response,omitempty"`
	BounceClassification string        `json:"bounce_classification,omitempty"`
}

func webhookPayload(m *message.Message, recipient string, e *message.Event) ([]byte, error) {
	out := webhookEvent{
		ID: e.ID, Type: string(e.Type), MessageID: m.ID, Recipient: recipient, OccurredAt: e.OccurredAt.UTC(), Tags: m.Tags,
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if e.Response != nil {
		out.Response = &dto.Response{Code: e.Response.Code, EnhancedCode: e.Response.EnhancedCode, Text: e.Response.Text}
	}
	if e.BounceClass != nil {
		out.BounceClassification = *e.BounceClass
	}

	return json.Marshal(out)
}
