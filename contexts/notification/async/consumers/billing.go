package consumers

import (
	"context"
	"fmt"
	"maps"
	"net/mail"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/logger"
)

// billing is what the billing consumers share: a billing event names several
// readers (the invoice email and every owner), each of whom gets their own
// email in their own locale.
type billing struct {
	sender ISender
	log    logger.ILog
}

// fanOut sends kind to every recipient. One unusable address is skipped and
// logged rather than failing the message: the payload would carry the same
// address on every redelivery, and the other readers still need their copy.
// A delivery failure is reported after every recipient has been tried, so
// the broker retries the whole message and nobody is left out.
func (b billing) fanOut(kind email.Kind, recipients []events.Recipient, shared map[string]any) error {
	var first error
	for _, r := range recipients {
		if _, err := mail.ParseAddress(r.Email); err != nil {
			b.log.WarnWithFields("billing email skipped: invalid recipient address", logger.NewFields().
				Add("kind", string(kind)).Add("user_id", r.UserID.String()))

			continue
		}

		params := recipientParams(r)
		maps.Copy(params, shared)

		if err := b.sender.Send(context.Background(), kind, r.Email, r.Locale, params); err != nil && first == nil {
			first = err
		}
	}

	return first
}

type InvoiceIssued struct{ billing }

func NewInvoiceIssued(sender ISender, log logger.ILog) *InvoiceIssued {
	return &InvoiceIssued{billing{sender: sender, log: log}}
}

func (c *InvoiceIssued) Consume(message *rabbitmq.Message) error {
	var event events.InvoiceIssuedEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload

	return c.fanOut(email.KindInvoiceIssued, p.Recipients, map[string]any{
		"TeamName":           p.TeamName,
		"InvoiceNumber":      p.InvoiceNumber,
		"PeriodStart":        p.PeriodStart,
		"PeriodEnd":          p.PeriodEnd,
		"TotalDue":           p.TotalDue,
		"Currency":           p.Currency,
		"SettlementAmount":   p.SettlementAmount,
		"SettlementCurrency": p.SettlementCurrency,
		"PdfURL":             p.PdfURL,
	})
}

type PaymentSucceeded struct{ billing }

func NewPaymentSucceeded(sender ISender, log logger.ILog) *PaymentSucceeded {
	return &PaymentSucceeded{billing{sender: sender, log: log}}
}

func (c *PaymentSucceeded) Consume(message *rabbitmq.Message) error {
	var event events.PaymentSucceededEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload

	return c.fanOut(email.KindPaymentSucceeded, p.Recipients, map[string]any{
		"TeamName":      p.TeamName,
		"InvoiceNumber": p.InvoiceNumber,
		"Amount":        p.Amount,
		"Currency":      p.Currency,
		"PaidAt":        p.PaidAt,
	})
}

type PaymentFailed struct{ billing }

func NewPaymentFailed(sender ISender, log logger.ILog) *PaymentFailed {
	return &PaymentFailed{billing{sender: sender, log: log}}
}

func (c *PaymentFailed) Consume(message *rabbitmq.Message) error {
	var event events.PaymentFailedEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload
	params := map[string]any{
		"TeamName":      p.TeamName,
		"InvoiceNumber": p.InvoiceNumber,
		"Amount":        p.Amount,
		"Currency":      p.Currency,
		"FailureReason": p.FailureReason,
		"Attempt":       p.Attempt,
	}
	// A nil *time.Time is still truthy to html/template's "if"; the template
	// must see a plain value or nothing at all.
	if p.NextRetryAt != nil {
		params["NextRetryAt"] = *p.NextRetryAt
	}

	return c.fanOut(email.KindPaymentFailed, p.Recipients, params)
}

type PaymentConfirmationRequired struct{ billing }

func NewPaymentConfirmationRequired(sender ISender, log logger.ILog) *PaymentConfirmationRequired {
	return &PaymentConfirmationRequired{billing{sender: sender, log: log}}
}

func (c *PaymentConfirmationRequired) Consume(message *rabbitmq.Message) error {
	var event events.PaymentConfirmationRequiredEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload

	return c.fanOut(email.KindPaymentConfirmationRequired, p.Recipients, map[string]any{
		"TeamName":      p.TeamName,
		"InvoiceNumber": p.InvoiceNumber,
		"Amount":        p.Amount,
		"Currency":      p.Currency,
		"ConfirmURL":    p.ConfirmURL,
	})
}

type TeamSuspended struct{ billing }

func NewTeamSuspended(sender ISender, log logger.ILog) *TeamSuspended {
	return &TeamSuspended{billing{sender: sender, log: log}}
}

func (c *TeamSuspended) Consume(message *rabbitmq.Message) error {
	var event events.TeamSuspendedEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload

	return c.fanOut(email.KindTeamSuspended, p.Recipients, map[string]any{
		"TeamName":      p.TeamName,
		"InvoiceNumber": p.InvoiceNumber,
		"PayURL":        p.PayURL,
	})
}

type DropletsDeletionWarning struct{ billing }

func NewDropletsDeletionWarning(sender ISender, log logger.ILog) *DropletsDeletionWarning {
	return &DropletsDeletionWarning{billing{sender: sender, log: log}}
}

func (c *DropletsDeletionWarning) Consume(message *rabbitmq.Message) error {
	var event events.DropletsDeletionWarningEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload

	return c.fanOut(email.KindDropletsDeletionWarning, p.Recipients, map[string]any{
		"TeamName":      p.TeamName,
		"InvoiceNumber": p.InvoiceNumber,
		"DeletionAt":    p.DeletionAt,
		"PayURL":        p.PayURL,
	})
}

type CardExpiring struct{ billing }

func NewCardExpiring(sender ISender, log logger.ILog) *CardExpiring {
	return &CardExpiring{billing{sender: sender, log: log}}
}

func (c *CardExpiring) Consume(message *rabbitmq.Message) error {
	var event events.CardExpiringEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	p := event.Payload

	return c.fanOut(email.KindCardExpiring, p.Recipients, map[string]any{
		"TeamName": p.TeamName,
		"Brand":    p.Brand,
		"Last4":    p.Last4,
		// Card expiry is printed the way the card itself prints it.
		"Expires": fmt.Sprintf("%02d/%d", p.ExpMonth, p.ExpYear),
	})
}
