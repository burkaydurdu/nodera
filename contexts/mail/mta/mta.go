// Package mta is what the mail context and the MTA that delivers its
// Messages exchange (docs/adr/0009): Messages go out as Injections, and what
// happened to each recipient comes back as Outcomes.
package mta

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
)

// Injection is one Message for all its sent recipients.
type Injection struct {
	MessageID uuid.UUID
	Envelope  message.Envelope
	Content   []byte
}

// Result answers which recipients the MTA refused to queue; the others are
// on their way.
type Result struct {
	Failed map[string]string
}

// Outcome is one thing the MTA reports about one recipient, named by its
// position in the Message.
type Outcome struct {
	MessageID   uuid.UUID
	Position    int
	Type        message.EventType
	OccurredAt  time.Time
	Response    *message.Response
	BounceClass string
	// Permanent marks a bounce that proves the address bad, so it is
	// suppressed; a content block or an expiry says nothing about the address.
	Permanent bool
}
