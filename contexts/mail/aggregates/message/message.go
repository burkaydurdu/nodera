// Package message is the Message: one email a Project's software handed to
// Nodera, its recipients, the Message events that happened to each, and the
// spool its content waits in until the MTA has taken it. Logs keep metadata
// only; the content is deleted at hand-over.
package message

import (
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/paging"
	"github.com/piggystats/nodera/utils/str"
)

const (
	MaxRecipients = 50
	// MaxSizeBytes is the decoded size of a Message, attachments included.
	MaxSizeBytes = 10 << 20
	MaxTags      = 10
	MaxTagLength = 64
	// Retention is how long logs are kept.
	Retention = 30 * 24 * time.Hour
	// IdempotencyWindow is how long a repeated Idempotency-Key answers the
	// first Message instead of sending again.
	IdempotencyWindow = 24 * time.Hour
)

type Source string

const (
	SourceAPI  Source = "api"
	SourceSMTP Source = "smtp"
)

type Kind string

const (
	KindTo  Kind = "to"
	KindCc  Kind = "cc"
	KindBcc Kind = "bcc"
)

type EventType string

const (
	EventAccepted   EventType = "accepted"
	EventDelivered  EventType = "delivered"
	EventDeferred   EventType = "deferred"
	EventBounced    EventType = "bounced"
	EventSuppressed EventType = "suppressed"
	EventComplained EventType = "complained"
)

// EventTypes is every Message event, in the order the web app lists them.
var EventTypes = []EventType{EventAccepted, EventDelivered, EventDeferred, EventBounced, EventSuppressed, EventComplained}

func IsEventType(value string) bool {
	for _, t := range EventTypes {
		if string(t) == value {
			return true
		}
	}

	return false
}

var ranks = map[EventType]int{
	EventAccepted:   0,
	EventDeferred:   1,
	EventDelivered:  2,
	EventBounced:    2,
	EventComplained: 3,
	EventSuppressed: 4,
}

// Progress is a recipient's status after an event: a status never moves
// back (a late deferral does not undo a delivery), a delivery and a bounce
// replace each other in the order they arrive (a receiving server may bounce
// what it first accepted), and a suppressed recipient was never sent to.
func Progress(current, incoming EventType) EventType {
	if current == EventSuppressed {
		return current
	}
	if ranks[incoming] >= ranks[current] {
		return incoming
	}

	return current
}

type Address struct {
	Email string
	Name  string
}

type Message struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	TeamID          uuid.UUID
	SendingDomainID uuid.UUID
	APIKeyID        string
	Source          Source
	From            Address
	Subject         string
	MessageIDHeader string
	Tags            []string
	SizeBytes       int
	IdempotencyKey  *string
	CreatedAt       time.Time
	HandedOverAt    *time.Time
	Recipients      []*Recipient
}

type Recipient struct {
	Position  int
	Address   string
	Name      string
	Kind      Kind
	Status    EventType
	UpdatedAt time.Time
	Events    []*Event
}

type Response struct {
	Code         int
	EnhancedCode string
	Text         string
}

type Event struct {
	ID          uuid.UUID
	MessageID   uuid.UUID
	Position    int
	Type        EventType
	OccurredAt  time.Time
	Response    *Response
	BounceClass *string
}

// Accepted counts the recipients the Message was sent to.
func (m *Message) Accepted() int {
	n := 0
	for _, r := range m.Recipients {
		if r.Status != EventSuppressed {
			n++
		}
	}

	return n
}

// Suppressed is the addresses dropped at acceptance.
func (m *Message) Suppressed() []string {
	out := []string{}
	for _, r := range m.Recipients {
		if r.Status == EventSuppressed {
			out = append(out, r.Address)
		}
	}

	return out
}

// RecipientAt is the recipient a position names, the way the MTA's reports
// name it.
func (m *Message) RecipientAt(position int) *Recipient {
	for _, r := range m.Recipients {
		if r.Position == position {
			return r
		}
	}

	return nil
}

func (m *Message) Recipient(address string) *Recipient {
	for _, r := range m.Recipients {
		if r.Address == strings.ToLower(address) {
			return r
		}
	}

	return nil
}

// NormalizeRecipients validates and orders a Message's recipients: lower-case
// addresses, each once (its first kind wins), positions in order.
func NormalizeRecipients(to, cc, bcc []Address) ([]*Recipient, error) {
	var out []*Recipient
	seen := map[string]bool{}
	for _, group := range []struct {
		kind  Kind
		addrs []Address
	}{{KindTo, to}, {KindCc, cc}, {KindBcc, bcc}} {
		for _, a := range group.addrs {
			address, err := NormalizeAddress(a.Email)
			if err != nil {
				return nil, err
			}
			if seen[address] {
				continue
			}
			seen[address] = true
			out = append(out, &Recipient{Position: len(out), Address: address, Name: strings.TrimSpace(a.Name), Kind: group.kind})
		}
	}

	switch {
	case len(out) == 0:
		return nil, ErrorNoRecipients
	case len(out) > MaxRecipients:
		return nil, ErrorTooManyRecipients
	default:
		return out, nil
	}
}

// NormalizeAddress accepts a bare address only and answers it in lower case,
// the form Suppressions and Message events are matched in.
func NormalizeAddress(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address != trimmed || str.IsNotEmpty(parsed.Name) {
		return "", ErrorInvalidAddress.Copy().SetMessagef("not an email address: %q", raw)
	}

	return strings.ToLower(parsed.Address), nil
}

// DomainOf is the domain part of an address, in lower case.
func DomainOf(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 || at == len(address)-1 {
		return ""
	}

	return strings.ToLower(address[at+1:])
}

// Envelope is what the MTA is handed, fixed when the Message is accepted.
type Envelope struct {
	Recipients      []EnvelopeRecipient `json:"recipients"`
	ProjectID       uuid.UUID           `json:"project_id"`
	TeamID          uuid.UUID           `json:"team_id"`
	SendingDomainID uuid.UUID           `json:"sending_domain_id"`
	DKIMDomain      string              `json:"dkim_domain"`
}

// Only is the envelope narrowed to the recipients failed names, the ones the
// MTA refused to queue.
func (e Envelope) Only(failed map[string]string) Envelope {
	narrowed := e
	narrowed.Recipients = nil
	for _, r := range e.Recipients {
		if _, ok := failed[strings.ToLower(r.Email)]; ok {
			narrowed.Recipients = append(narrowed.Recipients, r)
		}
	}

	return narrowed
}

type EnvelopeRecipient struct {
	Email      string `json:"email"`
	Position   int    `json:"position"`
	ReturnPath string `json:"return_path"`
}

// SpoolEntry is a Message waiting for the MTA.
type SpoolEntry struct {
	MessageID     uuid.UUID
	Content       []byte
	Envelope      Envelope
	Attempts      int
	NextAttemptAt time.Time
	CreatedAt     time.Time
}

// SpoolBackoff spaces hand-over attempts while the MTA is unreachable or
// refuses copies; after SpoolGiveUp the recipients still waiting are bounced,
// as an MTA expires what it cannot deliver.
var SpoolBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

const SpoolGiveUp = 24 * time.Hour

// NextSpoolAttempt answers when to try again after attempts failures, and
// false once the Message has waited longer than SpoolGiveUp.
func NextSpoolAttempt(createdAt time.Time, attempts int, now time.Time) (time.Time, bool) {
	if now.Sub(createdAt) >= SpoolGiveUp {
		return time.Time{}, false
	}

	step := SpoolBackoff[min(max(attempts, 1), len(SpoolBackoff))-1]

	return now.Add(step), true
}

// Filter narrows the logs of one Project.
type Filter struct {
	ProjectID uuid.UUID
	Status    *EventType
	Recipient string
	From      string
	Tag       string
	Since     time.Time
	Until     *time.Time
	Cursor    *paging.Cursor
	Limit     int
}
