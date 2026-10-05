package handlers

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/mta"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

const handOverFailure = "the message could not be handed to Nodera's delivery servers"

// MTA is the delivery port the dispatcher hands Messages to (docs/adr/0009).
//
//go:generate mockgen -destination=../../mock/handlers/mta.go -package=mockhandlers github.com/piggystats/nodera/contexts/mail/application/handlers MTA
type MTA interface {
	Inject(ctx context.Context, in *mta.Injection) (*mta.Result, error)
}

// Dispatcher hands spooled Messages to the MTA. Each Message is claimed with
// a row lock for its whole hand-over, in a transaction of its own, so the
// immediate attempt after acceptance and the job never inject it twice, and a
// failure halfway through a batch never takes back what the MTA already
// holds. Its content is deleted the moment the MTA holds every copy.
type Dispatcher struct {
	log          logger.ILog
	messages     message.IService
	webhooks     webhook.IService
	transactions postgresql.ITransactionManager
	mta          MTA
	now          Clock
}

func NewDispatcher(log logger.ILog, messages message.IService, webhooks webhook.IService, transactions postgresql.ITransactionManager, mtaPort MTA, now Clock) *Dispatcher {
	return &Dispatcher{log: log, messages: messages, webhooks: webhooks, transactions: transactions, mta: mtaPort, now: now}
}

// DispatchOne hands one Message over; one another dispatcher holds is left
// to it.
func (d *Dispatcher) DispatchOne(ctx context.Context, messageID uuid.UUID) error {
	txCtx, err := d.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = d.transactions.Rollback(txCtx) }()

	entry, err := d.messages.ClaimSpool(txCtx, messageID)
	if err != nil || entry == nil {
		return err
	}
	if err := d.handOver(txCtx, entry); err != nil {
		return err
	}

	return d.transactions.Commit(txCtx)
}

// DispatchDue hands over what is due: Messages whose earlier attempt failed
// and the ones a crash left behind.
func (d *Dispatcher) DispatchDue(ctx context.Context, limit int) error {
	due, err := d.messages.DueSpool(ctx, d.now(), limit)
	if err != nil {
		return err
	}
	for _, messageID := range due {
		if err := d.DispatchOne(ctx, messageID); err != nil {
			d.log.ErrorWithFields(err, logger.NewFields().Add("message_id", messageID))
		}
	}

	return nil
}

func (d *Dispatcher) handOver(ctx context.Context, entry *message.SpoolEntry) error {
	result, err := d.mta.Inject(ctx, &mta.Injection{MessageID: entry.MessageID, Envelope: entry.Envelope, Content: entry.Content})
	now := d.now()
	if err != nil {
		d.log.WarnWithFields("the MTA did not take a message", logger.NewFields().Add("message_id", entry.MessageID).Add("error", err.Error()))

		return d.retry(ctx, entry, entry.Envelope, err.Error(), now)
	}

	refused := entry.Envelope.Only(result.Failed)
	if len(refused.Recipients) == 0 {
		return d.messages.HandOver(ctx, entry.MessageID, now)
	}

	// A refusal at the door is never the receiving server's verdict: it is
	// the MTA unable to queue a copy, as while it cannot fetch the Sending
	// domain's signing key. The other copies are on their way.
	reasons := describeRefusals(result.Failed)
	d.log.WarnWithFields("the MTA refused copies of a message", logger.NewFields().Add("message_id", entry.MessageID).Add("error", reasons))

	return d.retry(ctx, entry, refused, reasons, now)
}

// retry keeps the recipients still to hand over for a later attempt, and
// bounces them once the Message has waited too long.
func (d *Dispatcher) retry(ctx context.Context, entry *message.SpoolEntry, pending message.Envelope, lastError string, now time.Time) error {
	attempts := entry.Attempts + 1
	if next, ok := message.NextSpoolAttempt(entry.CreatedAt, attempts, now); ok {
		return d.messages.RetrySpool(ctx, entry.MessageID, pending, attempts, next, lastError)
	}

	return d.giveUp(ctx, entry.MessageID, pending)
}

// giveUp bounces the recipients the MTA never took; the content goes with
// them.
func (d *Dispatcher) giveUp(ctx context.Context, messageID uuid.UUID, pending message.Envelope) error {
	m, err := d.messages.FindForEvent(ctx, messageID)
	if err != nil {
		return err
	}
	for _, r := range pending.Recipients {
		if rcpt := m.RecipientAt(r.Position); rcpt != nil {
			if err := d.bounce(ctx, m, rcpt, handOverFailure); err != nil {
				return err
			}
		}
	}

	return d.messages.DropSpool(ctx, messageID)
}

// bounce records a refusal on Nodera's side. It is not the receiving
// server's verdict on the address, so it neither suppresses nor counts
// towards the Project's bounce rate.
func (d *Dispatcher) bounce(ctx context.Context, m *message.Message, rcpt *message.Recipient, reason string) error {
	e := &message.Event{
		ID: uuid.New(), MessageID: m.ID, Position: rcpt.Position, Type: message.EventBounced, OccurredAt: d.now(),
		Response: &message.Response{Text: reason},
	}
	if _, err := d.messages.RecordEvent(ctx, e); err != nil {
		return err
	}
	if err := d.messages.UpdateRecipientStatus(ctx, m.ID, rcpt.Position, message.Progress(rcpt.Status, message.EventBounced), e.OccurredAt); err != nil {
		return err
	}

	payload, err := webhookPayload(m, rcpt.Address, e)
	if err != nil {
		return err
	}

	return d.webhooks.Enqueue(ctx, m.ProjectID, string(e.Type), payload, e.OccurredAt)
}

func describeRefusals(failed map[string]string) string {
	parts := make([]string, 0, len(failed))
	for address, reason := range failed {
		parts = append(parts, address+": "+reason)
	}
	slices.Sort(parts)

	return strings.Join(parts, "; ")
}
