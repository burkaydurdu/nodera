package consumers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/piggystats/rerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/clients/mail"
)

type sent struct {
	kind   email.Kind
	to     string
	locale string
	params map[string]any
}

// fanOutSender keeps every call, since a billing event addresses several readers.
type fanOutSender struct {
	calls []sent
	err   error
}

func (f *fanOutSender) Send(_ context.Context, kind email.Kind, to, locale string, params map[string]any) error {
	f.calls = append(f.calls, sent{kind: kind, to: to, locale: locale, params: params})

	return f.err
}

const recipients = `"recipients":[` +
	`{"user_id":"00000000-0000-0000-0000-000000000000","email":"billing@acme.test","full_name":"","locale":"tr"},` +
	`{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada Lovelace","locale":"en"}]`

func TestInvoiceIssued_SendsOneEmailPerRecipientInTheirLocale(t *testing.T) {
	sender := &fanOutSender{}

	err := NewInvoiceIssued(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042",` +
			`"period_start":"2026-09-01T00:00:00Z","period_end":"2026-10-01T00:00:00Z","total_due":"125.40","currency":"USD",` +
			`"settlement_amount":"5230.18","settlement_currency":"TRY","pdf_url":"https://billing.nodera.test/pdf/42?sig=x"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)

	first, second := sender.calls[0], sender.calls[1]
	assert.Equal(t, email.KindInvoiceIssued, first.kind)
	assert.Equal(t, "billing@acme.test", first.to)
	assert.Equal(t, "tr", first.locale)
	assert.Equal(t, "", first.params["FullName"])
	assert.Equal(t, "billing@acme.test", first.params["Email"])

	assert.Equal(t, email.KindInvoiceIssued, second.kind)
	assert.Equal(t, "ada@example.com", second.to)
	assert.Equal(t, "en", second.locale)
	assert.Equal(t, "Ada Lovelace", second.params["FullName"])

	assert.Equal(t, "Acme", second.params["TeamName"])
	assert.Equal(t, "NDR-2026-000042", second.params["InvoiceNumber"])
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), second.params["PeriodStart"])
	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), second.params["PeriodEnd"])
	assert.EqualValues(t, "125.40", second.params["TotalDue"])
	assert.Equal(t, "USD", second.params["Currency"])
	assert.EqualValues(t, "5230.18", second.params["SettlementAmount"])
	assert.Equal(t, "TRY", second.params["SettlementCurrency"])
	assert.Equal(t, "https://billing.nodera.test/pdf/42?sig=x", second.params["PdfURL"])
}

// Billing emits money as shopspring/decimal does, a quoted string; a bare
// number must decode the same way so the two never drift.
func TestInvoiceIssued_AcceptsBareNumbersForAmounts(t *testing.T) {
	sender := &fanOutSender{}

	err := NewInvoiceIssued(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042",` +
			`"period_start":"2026-09-01T00:00:00Z","period_end":"2026-10-01T00:00:00Z","total_due":125.4,"currency":"USD","pdf_url":"https://x/pdf"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.EqualValues(t, "125.4", sender.calls[0].params["TotalDue"])
	assert.EqualValues(t, "", sender.calls[0].params["SettlementAmount"], "absent settlement stays falsy for the template")
}

func TestInvoiceIssued_InvalidRecipientIsSkippedAndTheRestStillSent(t *testing.T) {
	sender := &fanOutSender{}

	err := NewInvoiceIssued(sender, logger.Noop()).Consume(message(
		`{"payload":{"recipients":[` +
			`{"user_id":"00000000-0000-0000-0000-000000000000","email":"","full_name":"","locale":"en"},` +
			`{"user_id":"019a0000-0000-7000-8000-000000000002","email":"not-an-address","full_name":"Bob","locale":"en"},` +
			`{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada","locale":"tr"}],` +
			`"team_name":"Acme","invoice_number":"NDR-2026-000042","period_start":"2026-09-01T00:00:00Z","period_end":"2026-10-01T00:00:00Z",` +
			`"total_due":"1.00","currency":"USD","pdf_url":"https://x/pdf"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 1)
	assert.Equal(t, "ada@example.com", sender.calls[0].to)
	assert.Equal(t, "tr", sender.calls[0].locale)
}

func TestInvoiceIssued_NoUsableRecipient_SendsNothingWithoutFailing(t *testing.T) {
	sender := &fanOutSender{}

	err := NewInvoiceIssued(sender, logger.Noop()).Consume(message(
		`{"payload":{"recipients":[],"team_name":"Acme","invoice_number":"NDR-2026-000042",` +
			`"period_start":"2026-09-01T00:00:00Z","period_end":"2026-10-01T00:00:00Z","total_due":"1.00","currency":"USD","pdf_url":"https://x/pdf"}}`))

	require.NoError(t, err)
	assert.Empty(t, sender.calls)
}

// A transport failure for one reader must not silence the others, and must
// still surface so the broker redelivers the message.
func TestInvoiceIssued_TransportFailure_TriesEveryoneAndIsRetryable(t *testing.T) {
	sender := &fanOutSender{err: mail.ErrFailedToSendEmail.Copy().SetError(errors.New("smtp down"))}

	err := NewInvoiceIssued(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042",` +
			`"period_start":"2026-09-01T00:00:00Z","period_end":"2026-10-01T00:00:00Z","total_due":"1.00","currency":"USD","pdf_url":"https://x/pdf"}}`))

	assert.True(t, rerror.IsRetryable(err))
	assert.Len(t, sender.calls, 2)
}

func TestPaymentSucceeded_SendsAmountAndDate(t *testing.T) {
	sender := &fanOutSender{}

	err := NewPaymentSucceeded(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042","amount":"125.40","currency":"USD","paid_at":"2026-10-02T09:30:00Z"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.Equal(t, email.KindPaymentSucceeded, sender.calls[0].kind)
	assert.Equal(t, "tr", sender.calls[0].locale)
	assert.Equal(t, "en", sender.calls[1].locale)
	assert.EqualValues(t, "125.40", sender.calls[1].params["Amount"])
	assert.Equal(t, "USD", sender.calls[1].params["Currency"])
	assert.Equal(t, "NDR-2026-000042", sender.calls[1].params["InvoiceNumber"])
	assert.Equal(t, time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC), sender.calls[1].params["PaidAt"])
}

func TestPaymentFailed_SendsReasonAttemptAndNextRetry(t *testing.T) {
	sender := &fanOutSender{}

	err := NewPaymentFailed(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042","amount":"125.40","currency":"USD",` +
			`"failure_reason":"Insufficient funds","attempt":2,"next_retry_at":"2026-10-05T09:30:00Z"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.Equal(t, email.KindPaymentFailed, sender.calls[0].kind)
	assert.Equal(t, "Insufficient funds", sender.calls[1].params["FailureReason"])
	assert.Equal(t, 2, sender.calls[1].params["Attempt"])
	assert.Equal(t, time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC), sender.calls[1].params["NextRetryAt"])
}

// The last Dunning attempt has no successor; the template must be able to
// tell, so the key is absent rather than a nil pointer.
func TestPaymentFailed_WithoutNextRetry_OmitsTheKey(t *testing.T) {
	sender := &fanOutSender{}

	err := NewPaymentFailed(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042","amount":"125.40","currency":"USD","failure_reason":"","attempt":3}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	_, present := sender.calls[0].params["NextRetryAt"]
	assert.False(t, present)
}

func TestPaymentConfirmationRequired_SendsTheConfirmLink(t *testing.T) {
	sender := &fanOutSender{}

	err := NewPaymentConfirmationRequired(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042","amount":"125.40","currency":"USD",` +
			`"confirm_url":"http://localhost:3002/billing/confirm?payment=p1&token=abc"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.Equal(t, email.KindPaymentConfirmationRequired, sender.calls[0].kind)
	assert.Equal(t, "http://localhost:3002/billing/confirm?payment=p1&token=abc", sender.calls[1].params["ConfirmURL"])
	assert.EqualValues(t, "125.40", sender.calls[1].params["Amount"])
}

func TestTeamSuspended_SendsThePayLink(t *testing.T) {
	sender := &fanOutSender{}

	err := NewTeamSuspended(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042",` +
			`"pay_url":"http://localhost:3002/dashboard/team/billing?tab=invoices&invoice=i1"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.Equal(t, email.KindTeamSuspended, sender.calls[0].kind)
	assert.Equal(t, "Acme", sender.calls[0].params["TeamName"])
	assert.Equal(t, "NDR-2026-000042", sender.calls[0].params["InvoiceNumber"])
	assert.Equal(t, "http://localhost:3002/dashboard/team/billing?tab=invoices&invoice=i1", sender.calls[0].params["PayURL"])
}

func TestDropletsDeletionWarning_SendsTheDeadlineAndPayLink(t *testing.T) {
	sender := &fanOutSender{}

	err := NewDropletsDeletionWarning(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","invoice_number":"NDR-2026-000042","deletion_at":"2026-10-22T00:00:00Z",` +
			`"pay_url":"http://localhost:3002/dashboard/team/billing?tab=invoices&invoice=i1"}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.Equal(t, email.KindDropletsDeletionWarning, sender.calls[0].kind)
	assert.Equal(t, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), sender.calls[0].params["DeletionAt"])
	assert.Equal(t, "http://localhost:3002/dashboard/team/billing?tab=invoices&invoice=i1", sender.calls[0].params["PayURL"])
}

func TestCardExpiring_SendsTheCardAndItsExpiry(t *testing.T) {
	sender := &fanOutSender{}

	err := NewCardExpiring(sender, logger.Noop()).Consume(message(
		`{"payload":{` + recipients + `,"team_name":"Acme","brand":"Visa","last4":"4242","exp_month":3,"exp_year":2027}}`))

	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	assert.Equal(t, email.KindCardExpiring, sender.calls[0].kind)
	assert.Equal(t, "Visa", sender.calls[0].params["Brand"])
	assert.Equal(t, "4242", sender.calls[0].params["Last4"])
	assert.Equal(t, "03/2027", sender.calls[0].params["Expires"])
}

func TestBillingConsumers_MalformedPayload_IsTerminal(t *testing.T) {
	log := logger.Noop()

	for name, consume := range map[string]func(*fanOutSender) error{
		"invoice_issued": func(s *fanOutSender) error {
			return NewInvoiceIssued(s, log).Consume(message(`{"payload":{"period_start":"not a date"}}`))
		},
		"payment_succeeded": func(s *fanOutSender) error {
			return NewPaymentSucceeded(s, log).Consume(message(`{"payload":{"paid_at":"not a date"}}`))
		},
		"payment_failed": func(s *fanOutSender) error {
			return NewPaymentFailed(s, log).Consume(message(`{"payload":{"attempt":"two"}}`))
		},
		"payment_confirmation_required": func(s *fanOutSender) error {
			return NewPaymentConfirmationRequired(s, log).Consume(message(`{not json`))
		},
		"team_suspended": func(s *fanOutSender) error {
			return NewTeamSuspended(s, log).Consume(message(`{"payload":{"recipients":"nobody"}}`))
		},
		"droplets_deletion_warning": func(s *fanOutSender) error {
			return NewDropletsDeletionWarning(s, log).Consume(message(`{"payload":{"deletion_at":"not a date"}}`))
		},
		"card_expiring": func(s *fanOutSender) error {
			return NewCardExpiring(s, log).Consume(message(`{"payload":{"exp_month":"March"}}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			sender := &fanOutSender{}

			err := consume(sender)

			var rerr rerror.Error
			require.True(t, errors.As(err, &rerr))
			assert.False(t, rerr.IsRetryable)
			assert.Empty(t, sender.calls, "nothing was sent")
		})
	}
}
