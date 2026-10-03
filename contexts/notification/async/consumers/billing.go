package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/events"
)

// BillingEvents makes a Notification of everything the billing context tells
// a Team about its money. Params are what the web app words it from; the
// email's own params keep the names the templates read, links included, and
// are never recorded.
type BillingEvents struct {
	notifier INotifier
}

func NewBillingEvents(notifier INotifier) *BillingEvents {
	return &BillingEvents{notifier: notifier}
}

func (c *BillingEvents) RoutingKeys() []string {
	return []string{
		constants.InvoiceIssued, constants.PaymentSucceeded, constants.PaymentFailed, constants.PaymentConfirmationRequired,
		constants.TeamSuspended, constants.DropletsDeletionWarning, constants.CardExpiring,
	}
}

func (c *BillingEvents) Consume(message *rabbitmq.Message) error {
	event, err := billingEvent(message)
	if err != nil {
		return err
	}
	if event == nil {
		return nil
	}

	return c.notifier.NotifyTeam(context.Background(), *event)
}

// billingEvent answers nil for a routing key this consumer does not handle.
func billingEvent(message *rabbitmq.Message) (*handlers.TeamEvent, error) {
	switch message.RoutingKey {
	case constants.InvoiceIssued:
		var e events.InvoiceIssuedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		params := invoiceParams(p.InvoiceID, p.InvoiceNumber)
		params["total_due"], params["currency"] = string(p.TotalDue), p.Currency
		params["period_start"], params["period_end"] = p.PeriodStart.UTC(), p.PeriodEnd.UTC()

		return teamEvent(p.Billing, notification.TypeInvoiceIssued, p.InvoiceID.String(), params, map[string]any{
			"InvoiceNumber": p.InvoiceNumber, "PeriodStart": p.PeriodStart, "PeriodEnd": p.PeriodEnd,
			"TotalDue": p.TotalDue, "Currency": p.Currency, "SettlementAmount": p.SettlementAmount,
			"SettlementCurrency": p.SettlementCurrency, "PdfURL": p.PdfURL,
		})
	case constants.PaymentSucceeded:
		var e events.PaymentSucceededEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		params := invoiceParams(p.InvoiceID, p.InvoiceNumber)
		params["amount"], params["currency"] = string(p.Amount), p.Currency

		return teamEvent(p.Billing, notification.TypePaymentSucceeded, p.PaymentID.String(), params, map[string]any{
			"InvoiceNumber": p.InvoiceNumber, "Amount": p.Amount, "Currency": p.Currency, "PaidAt": p.PaidAt,
		})
	case constants.PaymentFailed:
		var e events.PaymentFailedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		params := invoiceParams(p.InvoiceID, p.InvoiceNumber)
		params["amount"], params["currency"], params["attempt"] = string(p.Amount), p.Currency, p.Attempt
		emailParams := map[string]any{
			"InvoiceNumber": p.InvoiceNumber, "Amount": p.Amount, "Currency": p.Currency,
			"FailureReason": p.FailureReason, "Attempt": p.Attempt,
		}
		// A nil *time.Time is still truthy to html/template's "if"; the
		// template must see a plain value or nothing at all.
		if p.NextRetryAt != nil {
			params["next_retry_at"] = p.NextRetryAt.UTC()
			emailParams["NextRetryAt"] = *p.NextRetryAt
		}

		return teamEvent(p.Billing, notification.TypePaymentFailed, p.PaymentID.String(), params, emailParams)
	case constants.PaymentConfirmationRequired:
		var e events.PaymentConfirmationRequiredEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		params := invoiceParams(p.InvoiceID, p.InvoiceNumber)
		params["amount"], params["currency"] = string(p.Amount), p.Currency

		return teamEvent(p.Billing, notification.TypePaymentConfirmationRequired, p.PaymentID.String(), params, map[string]any{
			"InvoiceNumber": p.InvoiceNumber, "Amount": p.Amount, "Currency": p.Currency, "ConfirmURL": p.ConfirmURL,
		})
	case constants.TeamSuspended:
		var e events.TeamSuspendedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload

		return teamEvent(p.Billing, notification.TypeTeamSuspended, p.InvoiceID.String(), invoiceParams(p.InvoiceID, p.InvoiceNumber), map[string]any{
			"InvoiceNumber": p.InvoiceNumber, "PayURL": p.PayURL,
		})
	case constants.DropletsDeletionWarning:
		var e events.DropletsDeletionWarningEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		params := invoiceParams(p.InvoiceID, p.InvoiceNumber)
		params["deletion_at"] = p.DeletionAt.UTC()

		return teamEvent(p.Billing, notification.TypeDropletsDeletionWarning, p.InvoiceID.String()+":"+strconv.Itoa(p.DunningDay), params, map[string]any{
			"InvoiceNumber": p.InvoiceNumber, "DeletionAt": p.DeletionAt, "PayURL": p.PayURL,
		})
	case constants.CardExpiring:
		var e events.CardExpiringEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		params := map[string]any{"brand": p.Brand, "last4": p.Last4, "exp_month": p.ExpMonth, "exp_year": p.ExpYear}

		return teamEvent(p.Billing, notification.TypeCardExpiring, fmt.Sprintf("%s:%04d-%02d", p.PaymentMethodID, p.ExpYear, p.ExpMonth), params, map[string]any{
			"Brand": p.Brand, "Last4": p.Last4,
			// Card expiry is printed the way the card itself prints it.
			"Expires": fmt.Sprintf("%02d/%d", p.ExpMonth, p.ExpYear),
		})
	default:
		return nil, nil
	}
}

// teamEvent names the occasion by its type and the billing record it is
// about, so a redelivered event records and sends nothing twice.
func teamEvent(b events.Billing, t notification.Type, occasion string, params, emailParams map[string]any) (*handlers.TeamEvent, error) {
	if b.TeamID == uuid.Nil {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the team")
	}

	event := &handlers.TeamEvent{
		SourceKey: string(t) + ":" + occasion, Type: t, TeamID: b.TeamID, Params: params, EmailParams: emailParams,
	}
	if b.InvoiceAddress != nil {
		event.Also = []notification.Recipient{{Email: b.InvoiceAddress.Email, FullName: b.InvoiceAddress.FullName, Locale: b.InvoiceAddress.Locale}}
	}

	return event, nil
}

func invoiceParams(invoiceID uuid.UUID, number string) map[string]any {
	return map[string]any{"invoice_id": invoiceID.String(), "invoice_number": number}
}

// decodeEvent wraps a malformed payload into a domain error: the plain
// unmarshal error says nothing about which event was refused.
func decodeEvent(message *rabbitmq.Message, into any) error {
	if err := json.Unmarshal(message.Payload, into); err != nil {
		return notification.ErrorMalformedEvent.Copy().SetError(err)
	}

	return nil
}
