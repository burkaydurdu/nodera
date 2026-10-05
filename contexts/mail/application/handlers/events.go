package handlers

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/suppression"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/mta"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/utils/str"
)

// Records reads what the MTA's log hook posts into Outcomes.
//
//go:generate mockgen -destination=../../mock/handlers/records.go -package=mockhandlers github.com/piggystats/nodera/contexts/mail/application/handlers Records
type Records interface {
	Parse(raw []byte) ([]mta.Outcome, error)
}

// Events records what the MTA reports about each recipient.
type Events struct {
	records      Records
	messages     message.IService
	suppressions suppression.IService
	limits       limits.IService
	webhooks     webhook.IService
	transactions postgresql.ITransactionManager
}

func NewEvents(
	records Records,
	messages message.IService,
	suppressions suppression.IService,
	limitService limits.IService,
	webhooks webhook.IService,
	transactions postgresql.ITransactionManager,
) *Events {
	return &Events{records: records, messages: messages, suppressions: suppressions, limits: limitService, webhooks: webhooks, transactions: transactions}
}

// Apply records each outcome of one post once: a record the MTA posts twice
// changes nothing the second time, and one about a Message past retention,
// or not sent by Nodera, is dropped.
func (h *Events) Apply(ctx context.Context, raw []byte) error {
	outcomes, err := h.records.Parse(raw)
	if err != nil {
		return message.ErrorMalformedRecords.Copy().SetError(err)
	}

	for i := range outcomes {
		if err := h.record(ctx, &outcomes[i]); err != nil {
			return err
		}
	}

	return nil
}

func (h *Events) record(ctx context.Context, o *mta.Outcome) error {
	ctx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	m, err := h.messages.FindForEvent(ctx, o.MessageID)
	if errors.Is(err, message.ErrorNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	rcpt := m.RecipientAt(o.Position)
	if rcpt == nil {
		return nil
	}

	e := &message.Event{ID: uuid.New(), MessageID: m.ID, Position: rcpt.Position, Type: o.Type, OccurredAt: o.OccurredAt, Response: o.Response}
	if str.IsNotEmpty(o.BounceClass) {
		e.BounceClass = &o.BounceClass
	}
	recorded, err := h.messages.RecordEvent(ctx, e)
	if err != nil || !recorded {
		return err
	}

	if next := message.Progress(rcpt.Status, o.Type); next != rcpt.Status {
		if err := h.messages.UpdateRecipientStatus(ctx, m.ID, rcpt.Position, next, o.OccurredAt); err != nil {
			return err
		}
	}
	if err := h.count(ctx, m, o); err != nil {
		return err
	}
	if err := h.suppress(ctx, m, rcpt, o); err != nil {
		return err
	}

	payload, err := webhookPayload(m, rcpt.Address, e)
	if err != nil {
		return err
	}
	if err := h.webhooks.Enqueue(ctx, m.ProjectID, string(e.Type), payload, o.OccurredAt); err != nil {
		return err
	}

	return h.transactions.Commit(ctx)
}

// count adds the outcome to the day it happened on, which the Daily limit
// and the seven-day rates are judged from. Only a bounce that proves the
// address bad counts: the rates judge the Project's lists, and an expiry or a
// content block says nothing about them.
func (h *Events) count(ctx context.Context, m *message.Message, o *mta.Outcome) error {
	var outcome limits.Outcome
	switch {
	case o.Type == message.EventDelivered:
		outcome = limits.OutcomeDelivered
	case o.Type == message.EventBounced && o.Permanent:
		outcome = limits.OutcomeBounced
	case o.Type == message.EventComplained:
		outcome = limits.OutcomeComplained
	default:
		return nil
	}

	return h.limits.AddOutcome(ctx, m.ProjectID, limits.DayOf(o.OccurredAt), outcome, 1)
}

// suppress keeps the Project from mailing an address that proved bad or whose
// recipient complained.
func (h *Events) suppress(ctx context.Context, m *message.Message, rcpt *message.Recipient, o *mta.Outcome) error {
	var reason suppression.Reason
	switch {
	case o.Type == message.EventComplained:
		reason = suppression.ReasonComplaint
	case o.Type == message.EventBounced && o.Permanent:
		reason = suppression.ReasonBounce
	default:
		return nil
	}

	messageID := m.ID

	return h.suppressions.Add(ctx, &suppression.Suppression{
		ID: uuid.New(), ProjectID: m.ProjectID, Address: rcpt.Address, Reason: reason, MessageID: &messageID, CreatedAt: o.OccurredAt,
	})
}
