package email

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func billingParams() map[string]any {
	return map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "TeamName": "Acme", "InvoiceNumber": "NDR-2026-000042",
		"PeriodStart": time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "PeriodEnd": time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		"TotalDue": json.Number("125.40"), "Amount": json.Number("125.40"), "Currency": "USD",
		"SettlementAmount": json.Number("5230.18"), "SettlementCurrency": "TRY",
		"PdfURL":        "https://billing.nodera.test/pdf/42?sig=x",
		"PaidAt":        time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC),
		"FailureReason": "Insufficient funds", "Attempt": 2, "NextRetryAt": time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC),
		"ConfirmURL": "http://localhost:3002/billing/confirm?payment=p1&token=abc",
		"PayURL":     "http://localhost:3002/dashboard/team/billing?tab=invoices&invoice=i1",
		"DeletionAt": time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		"Brand":      "Visa", "Last4": "4242", "Expires": "03/2027",
	}
}

func TestSender_RendersEveryBillingKindInBothLocales(t *testing.T) {
	sender, client := newSender(t)
	kinds := []Kind{
		KindInvoiceIssued, KindPaymentSucceeded, KindPaymentFailed, KindPaymentConfirmationRequired,
		KindTeamSuspended, KindDropletsDeletionWarning, KindCardExpiring,
	}

	for _, kind := range kinds {
		for _, locale := range []string{"tr", "en"} {
			t.Run(string(kind)+"/"+locale, func(t *testing.T) {
				require.NoError(t, sender.Send(context.Background(), kind, "ada@example.com", locale, billingParams()))
				assert.Equal(t, []string{"ada@example.com"}, client.message.To)
				assert.NotEmpty(t, client.message.Subject)
				assert.Contains(t, client.message.Subject, "Acme")
				assert.Contains(t, client.message.Body, "Ada")
				assert.Contains(t, client.message.Body, "Acme")
				assert.Contains(t, client.message.Body, "ada@example.com")
			})
		}
	}
}

func TestSender_InvoiceIssued_English_ShowsTotalsPeriodAndPdfLink(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindInvoiceIssued, "ada@example.com", "en", billingParams()))

	assert.Equal(t, "Invoice NDR-2026-000042 for Acme is ready", client.message.Subject)
	assert.Contains(t, client.message.Body, "NDR-2026-000042")
	assert.Contains(t, client.message.Body, "125.40 USD")
	assert.Contains(t, client.message.Body, "5230.18 TRY")
	assert.Contains(t, client.message.Body, "September 1, 2026")
	assert.Contains(t, client.message.Body, "September 30, 2026")
	assert.Contains(t, client.message.Body, `href="https://billing.nodera.test/pdf/42?sig=x"`)
}

func TestSender_InvoiceIssued_Turkish_FormatsDatesLocally(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindInvoiceIssued, "ada@example.com", "tr", billingParams()))

	assert.Equal(t, "Acme için NDR-2026-000042 numaralı fatura hazır", client.message.Subject)
	assert.Contains(t, client.message.Body, "125.40 USD")
	assert.Contains(t, client.message.Body, "5230.18 TRY")
	assert.Contains(t, client.message.Body, "01.09.2026")
	assert.Contains(t, client.message.Body, "30.09.2026")
	assert.Contains(t, client.message.Body, `href="https://billing.nodera.test/pdf/42?sig=x"`)
}

// A USD-settled invoice has no settlement line; the template must not print
// an empty "charged as" clause.
func TestSender_InvoiceIssued_WithoutSettlement_OmitsTheLine(t *testing.T) {
	sender, client := newSender(t)
	params := billingParams()
	params["SettlementAmount"] = json.Number("")
	params["SettlementCurrency"] = ""

	require.NoError(t, sender.Send(context.Background(), KindInvoiceIssued, "ada@example.com", "en", params))

	assert.NotContains(t, client.message.Body, "charged as")
	assert.NotContains(t, client.message.Body, "TRY")
}

func TestSender_PaymentSucceeded_ShowsAmountAndPaidDate(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindPaymentSucceeded, "ada@example.com", "en", billingParams()))
	assert.Equal(t, "Payment received for Acme: 125.40 USD", client.message.Subject)
	assert.Contains(t, client.message.Body, "125.40 USD")
	assert.Contains(t, client.message.Body, "NDR-2026-000042")
	assert.Contains(t, client.message.Body, "October 2, 2026")

	require.NoError(t, sender.Send(context.Background(), KindPaymentSucceeded, "ada@example.com", "tr", billingParams()))
	assert.Equal(t, "Acme için ödeme alındı: 125.40 USD", client.message.Subject)
	assert.Contains(t, client.message.Body, "02.10.2026")
}

func TestSender_PaymentFailed_ShowsReasonAttemptAndNextRetry(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindPaymentFailed, "ada@example.com", "en", billingParams()))
	assert.Equal(t, "Payment failed for Acme: invoice NDR-2026-000042", client.message.Subject)
	assert.Contains(t, client.message.Body, "125.40 USD")
	assert.Contains(t, client.message.Body, "attempt 2")
	assert.Contains(t, client.message.Body, "Insufficient funds")
	assert.Contains(t, client.message.Body, "October 5, 2026")

	require.NoError(t, sender.Send(context.Background(), KindPaymentFailed, "ada@example.com", "tr", billingParams()))
	assert.Contains(t, client.message.Body, "2. deneme")
	assert.Contains(t, client.message.Body, "05.10.2026")
}

func TestSender_PaymentFailed_WithoutNextRetry_PromisesNoRetry(t *testing.T) {
	sender, client := newSender(t)
	params := billingParams()
	delete(params, "NextRetryAt")
	params["FailureReason"] = ""

	require.NoError(t, sender.Send(context.Background(), KindPaymentFailed, "ada@example.com", "en", params))

	assert.NotContains(t, client.message.Body, "try again")
	assert.NotContains(t, client.message.Body, "Your bank said")
}

func TestSender_PaymentConfirmationRequired_LinksTheConfirmURL(t *testing.T) {
	sender, client := newSender(t)

	for _, locale := range []string{"en", "tr"} {
		require.NoError(t, sender.Send(context.Background(), KindPaymentConfirmationRequired, "ada@example.com", locale, billingParams()))
		assert.Contains(t, client.message.Body, "125.40 USD")
		assert.Contains(t, client.message.Body, "NDR-2026-000042")
		assert.Contains(t, client.message.Body, `href="http://localhost:3002/billing/confirm?payment=p1&amp;token=abc"`)
	}
}

func TestSender_TeamSuspended_LinksThePayURL(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindTeamSuspended, "ada@example.com", "en", billingParams()))
	assert.Equal(t, "Acme has been suspended on Nodera", client.message.Subject)
	assert.Contains(t, client.message.Body, "NDR-2026-000042")
	assert.Contains(t, client.message.Body, `href="http://localhost:3002/dashboard/team/billing?tab=invoices&amp;invoice=i1"`)

	require.NoError(t, sender.Send(context.Background(), KindTeamSuspended, "ada@example.com", "tr", billingParams()))
	assert.Equal(t, "Acme Nodera'da askıya alındı", client.message.Subject)
	assert.Contains(t, client.message.Body, `href="http://localhost:3002/dashboard/team/billing?tab=invoices&amp;invoice=i1"`)
}

func TestSender_DropletsDeletionWarning_ShowsTheDeadlineAndPayURL(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindDropletsDeletionWarning, "ada@example.com", "en", billingParams()))
	assert.Contains(t, client.message.Body, "October 22, 2026")
	assert.Contains(t, client.message.Body, "NDR-2026-000042")
	assert.Contains(t, client.message.Body, `href="http://localhost:3002/dashboard/team/billing?tab=invoices&amp;invoice=i1"`)

	require.NoError(t, sender.Send(context.Background(), KindDropletsDeletionWarning, "ada@example.com", "tr", billingParams()))
	assert.Contains(t, client.message.Body, "22.10.2026")
}

func TestSender_CardExpiring_ShowsBrandLast4AndExpiry(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindCardExpiring, "ada@example.com", "en", billingParams()))
	assert.Equal(t, "Your card ending in 4242 for Acme is about to expire", client.message.Subject)
	assert.Contains(t, client.message.Body, "Visa")
	assert.Contains(t, client.message.Body, "4242")
	assert.Contains(t, client.message.Body, "03/2027")

	require.NoError(t, sender.Send(context.Background(), KindCardExpiring, "ada@example.com", "tr", billingParams()))
	assert.Contains(t, client.message.Body, "4242")
	assert.Contains(t, client.message.Body, "03/2027")
}
