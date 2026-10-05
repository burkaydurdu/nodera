package kumomta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/mta"
)

// badAddress are the classifications that prove an address bad; a bounce
// for anything else (content, policy, reputation) leaves it deliverable.
var badAddress = []string{"InvalidRecipient", "BadDomain", "InactiveMailbox"}

type record struct {
	Type                 string          `json:"type"`
	Sender               string          `json:"sender"`
	Timestamp            int64           `json:"timestamp"`
	EventTime            string          `json:"event_time"`
	Response             response        `json:"response"`
	BounceClassification string          `json:"bounce_classification"`
	Meta                 map[string]any  `json:"meta"`
	FeedbackReport       *feedbackReport `json:"feedback_report"`
}

type response struct {
	Code         int           `json:"code"`
	EnhancedCode *enhancedCode `json:"enhanced_code"`
	Content      string        `json:"content"`
}

type enhancedCode struct {
	Class   int `json:"class"`
	Subject int `json:"subject"`
	Detail  int `json:"detail"`
}

type feedbackReport struct {
	SupplementalTrace map[string]any `json:"supplemental_trace"`
}

// Records reads what the log hook posts into Outcomes. A record names its
// recipient by position: KumoMTA's own records carry it in the metadata the
// policy set, and a bounce or complaint from outside in the return path the
// recipient's copy went out with, which only Nodera can make.
type Records struct {
	paths message.ReturnPaths
}

func NewRecords(paths message.ReturnPaths) *Records {
	return &Records{paths: paths}
}

// Parse takes one record or an array of them. Records that are not delivery
// outcomes, and records of mail this context did not send, are skipped.
func (p *Records) Parse(data []byte) ([]mta.Outcome, error) {
	var records []record
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("[")) {
		if err := json.Unmarshal(trimmed, &records); err != nil {
			return nil, err
		}
	} else {
		var r record
		if err := json.Unmarshal(trimmed, &r); err != nil {
			return nil, err
		}
		records = []record{r}
	}

	var out []mta.Outcome
	for i := range records {
		if o, ok := p.outcome(&records[i]); ok {
			out = append(out, o)
		}
	}

	return out, nil
}

func (p *Records) outcome(r *record) (mta.Outcome, bool) {
	o := mta.Outcome{OccurredAt: r.occurredAt(), BounceClass: r.BounceClassification}
	if r.Response.Code != 0 {
		o.Response = &message.Response{Code: r.Response.Code, EnhancedCode: r.Response.EnhancedCode.String(), Text: r.Response.Content}
	}

	var ok bool
	switch r.Type {
	case "Delivery":
		o.Type = message.EventDelivered
		o.MessageID, o.Position, ok = metaRecipient(r.Meta)
	case "TransientFailure":
		o.Type = message.EventDeferred
		o.MessageID, o.Position, ok = metaRecipient(r.Meta)
	case "Bounce", "AdminBounce":
		o.Type = message.EventBounced
		o.Permanent = r.badAddress()
		o.MessageID, o.Position, ok = metaRecipient(r.Meta)
	case "Expiration":
		o.Type = message.EventBounced
		o.BounceClass = "MessageExpired"
		o.MessageID, o.Position, ok = metaRecipient(r.Meta)
	case "OOB":
		// A bounce report carries none of the Message's metadata; its sender
		// is the return path the recipient's copy went out with.
		o.Type = message.EventBounced
		o.Permanent = r.badAddress()
		o.MessageID, o.Position, ok = p.paths.Parse(r.Sender)
	case "Feedback":
		// A feedback loop often hides the recipient's address; the trace
		// header the copy carried names its return path.
		o.Type = message.EventComplained
		if r.FeedbackReport != nil {
			returnPath, _ := r.FeedbackReport.SupplementalTrace["x_return_path"].(string)
			o.MessageID, o.Position, ok = p.paths.Parse(returnPath)
		}
	}

	return o, ok
}

func (r *record) badAddress() bool {
	if slices.Contains(badAddress, r.BounceClassification) {
		return true
	}
	ec := r.Response.EnhancedCode

	return ec != nil && ec.Class == 5 && ec.Subject == 1
}

func (r *record) occurredAt() time.Time {
	if t, err := time.Parse(time.RFC3339Nano, r.EventTime); err == nil {
		return t.UTC()
	}

	return time.Unix(r.Timestamp, 0).UTC()
}

// metaRecipient reads the Message and the position the policy put in the
// metadata of every copy it queued.
func metaRecipient(meta map[string]any) (uuid.UUID, int, bool) {
	rawID, _ := meta["x_message_id"].(string)
	id, err := uuid.Parse(rawID)
	if err != nil {
		return uuid.Nil, 0, false
	}
	rawPosition, _ := meta["x_position"].(string)
	position, err := strconv.Atoi(rawPosition)
	if err != nil || position < 0 {
		return uuid.Nil, 0, false
	}

	return id, position, true
}

func (e *enhancedCode) String() string {
	if e == nil {
		return ""
	}

	return fmt.Sprintf("%d.%d.%d", e.Class, e.Subject, e.Detail)
}
