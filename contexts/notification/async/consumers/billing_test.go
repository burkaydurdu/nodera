package consumers_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	mockconsumers "github.com/piggystats/nodera/contexts/notification/mock/consumers"
)

func toldTeam(t *testing.T, notifier *mockconsumers.MockINotifier) *handlers.TeamEvent {
	t.Helper()
	var event handlers.TeamEvent
	notifier.EXPECT().NotifyTeam(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, e handlers.TeamEvent) error {
		event = e

		return nil
	})

	return &event
}

func billing(teamID uuid.UUID, fields map[string]any) map[string]any {
	payload := map[string]any{"team_id": teamID, "invoice_address": map[string]any{"email": "books@acme.example", "full_name": "Acme GmbH", "locale": "en"}}
	for k, v := range fields {
		payload[k] = v
	}

	return payload
}

func TestInvoiceIssued_NamesTheInvoiceAndAlsoTellsTheInvoiceAddress(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, invoiceID := uuid.New(), uuid.New()

	err := consumers.NewBillingEvents(notifier).Consume(message(t, constants.InvoiceIssued, billing(teamID, map[string]any{
		"invoice_id": invoiceID, "invoice_number": "NOD-1", "total_due": "120.00", "currency": "USD", "pdf_url": "https://app/pdf?token=secret",
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeInvoiceIssued, event.Type)
	assert.Equal(t, "invoice_issued:"+invoiceID.String(), event.SourceKey)
	assert.Equal(t, teamID, event.TeamID)
	assert.Equal(t, []notification.Recipient{{Email: "books@acme.example", FullName: "Acme GmbH", Locale: "en"}}, event.Also)
	assert.Equal(t, "NOD-1", event.Params["invoice_number"])
	assert.NotContains(t, event.Params, "pdf_url")
	assert.Equal(t, "https://app/pdf?token=secret", event.EmailParams["PdfURL"])
}

func TestPaymentFailed_IsNamedByItsAttemptAndPromisesNoRetryItHasNot(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	paymentID := uuid.New()

	err := consumers.NewBillingEvents(notifier).Consume(message(t, constants.PaymentFailed, billing(uuid.New(), map[string]any{
		"invoice_id": uuid.New(), "payment_id": paymentID, "invoice_number": "NOD-1", "amount": "10.00", "currency": "USD", "attempt": 4,
	})))

	require.NoError(t, err)
	assert.Equal(t, "payment_failed:"+paymentID.String(), event.SourceKey)
	assert.NotContains(t, event.Params, "next_retry_at")
	assert.NotContains(t, event.EmailParams, "NextRetryAt", "a nil time is truthy to html/template")
}

func TestDropletsDeletionWarning_IsNamedByItsDay(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	invoiceID := uuid.New()

	err := consumers.NewBillingEvents(notifier).Consume(message(t, constants.DropletsDeletionWarning, billing(uuid.New(), map[string]any{
		"invoice_id": invoiceID, "invoice_number": "NOD-1", "dunning_day": 20, "deletion_at": time.Now().UTC(),
	})))

	require.NoError(t, err)
	assert.Equal(t, "droplets_deletion_warning:"+invoiceID.String()+":20", event.SourceKey)
}

func TestCardExpiring_IsNamedByTheCardAndItsExpiry(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	methodID := uuid.New()

	err := consumers.NewBillingEvents(notifier).Consume(message(t, constants.CardExpiring, billing(uuid.New(), map[string]any{
		"payment_method_id": methodID, "brand": "visa", "last4": "4242", "exp_month": 3, "exp_year": 2027,
	})))

	require.NoError(t, err)
	assert.Equal(t, "card_expiring:"+methodID.String()+":2027-03", event.SourceKey)
	assert.Equal(t, "03/2027", event.EmailParams["Expires"], "printed the way the card prints it")
}

func TestBillingEvent_WithoutAnInvoiceAddressTellsTheMembersOnly(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)

	err := consumers.NewBillingEvents(notifier).Consume(message(t, constants.TeamSuspended, map[string]any{
		"team_id": uuid.New(), "invoice_id": uuid.New(), "invoice_number": "NOD-1",
	}))

	require.NoError(t, err)
	assert.Empty(t, event.Also)
}

func TestBillingEvent_WithoutItsTeamIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewBillingEvents(notifier).Consume(message(t, constants.PaymentSucceeded, map[string]any{"payment_id": uuid.New()}))

	require.Error(t, err)
}
