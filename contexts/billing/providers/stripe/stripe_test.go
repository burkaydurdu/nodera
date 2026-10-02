package stripe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v84/webhook"

	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/providers/stripe"
	"github.com/piggystats/nodera/logger"
)

const webhookSecret = "whsec_test_secret"

type recorded struct {
	status int
	body   string
}

// stripeStub answers by method and path with recorded Stripe shapes, trimmed
// to the fields the adapter reads, and keeps the form each call sent.
type stripeStub struct {
	mu        sync.Mutex
	responses map[string]recorded
	forms     map[string]url.Values
	headers   map[string]http.Header
}

func newStub(t *testing.T, responses map[string]recorded) (*stripeStub, *stripe.Stripe) {
	t.Helper()
	stub := &stripeStub{responses: responses, forms: map[string]url.Values{}, headers: map[string]http.Header{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		_ = r.ParseForm()

		stub.mu.Lock()
		stub.forms[key] = r.PostForm
		stub.headers[key] = r.Header.Clone()
		res, ok := stub.responses[key]
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such object"}}`))

			return
		}
		w.WriteHeader(res.status)
		_, _ = w.Write([]byte(res.body))
	}))
	t.Cleanup(server.Close)

	adapter, err := stripe.New(stripe.Config{SecretKey: "sk_test_x", WebhookSecret: webhookSecret, BaseURL: server.URL}, logger.Noop())
	require.NoError(t, err)

	return stub, adapter
}

func (s *stripeStub) form(key string) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.forms[key]
}

func (s *stripeStub) header(key string) http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.headers[key]
}

func TestNew_RefusesMissingCredentials(t *testing.T) {
	_, err := stripe.New(stripe.Config{SecretKey: "sk"}, logger.Noop())
	assert.ErrorIs(t, err, stripe.ErrMissingCredentials)
}

func TestStartCardVerification_CreatesASetupModeCheckoutSession(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /v1/customers":         {200, `{"id":"cus_1","object":"customer"}`},
		"POST /v1/checkout/sessions": {200, `{"id":"cs_1","object":"checkout.session","mode":"setup","url":"https://checkout.stripe.com/c/pay/cs_1"}`},
	})

	v, err := adapter.StartCardVerification(context.Background(), providers.VerificationRequest{
		Payer: providers.Payer{TeamID: uuid.New(), Email: "books@example.com", LegalName: "Acme GmbH"}, Currency: "USD",
		ReturnURL: "https://app.example/billing/cards/complete?verification=1", ReferenceID: "ver-1",
	})

	require.NoError(t, err)
	assert.Equal(t, providers.Verification{Ref: "cs_1", RedirectURL: "https://checkout.stripe.com/c/pay/cs_1"}, *v)
	session := stub.form("POST /v1/checkout/sessions")
	assert.Equal(t, "setup", session.Get("mode"))
	assert.Equal(t, "cus_1", session.Get("customer"))
	assert.Equal(t, "usd", session.Get("currency"))
	assert.Equal(t, "https://app.example/billing/cards/complete?verification=1", session.Get("success_url"))
}

func TestCompleteCardVerification_ReadsTheCardOffTheSetupIntent(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /v1/checkout/sessions/cs_1": {200, `{"id":"cs_1","object":"checkout.session","customer":"cus_1","status":"complete",
			"setup_intent":{"id":"seti_1","object":"setup_intent","status":"succeeded",
				"payment_method":{"id":"pm_1","object":"payment_method","card":{"brand":"visa","last4":"4242","exp_month":12,"exp_year":2030}}}}`},
	})

	card, err := adapter.CompleteCardVerification(context.Background(), "cs_1", nil)

	require.NoError(t, err)
	assert.Equal(t, providers.SavedCard{CustomerRef: "cus_1", CardRef: "pm_1", Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030}, *card)
}

func TestCompleteCardVerification_UnfinishedSetupIsADecline(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /v1/checkout/sessions/cs_2": {200, `{"id":"cs_2","object":"checkout.session","customer":"cus_1","status":"open",
			"setup_intent":{"id":"seti_2","object":"setup_intent","status":"requires_payment_method",
				"last_setup_error":{"type":"card_error","code":"card_declined","decline_code":"insufficient_funds"}}}`},
	})

	_, err := adapter.CompleteCardVerification(context.Background(), "cs_2", nil)

	reason, declined := providers.Declined(err)
	require.True(t, declined, err)
	assert.Equal(t, providers.ReasonInsufficientFunds, reason)
}

func TestCompleteCardVerification_UnknownSessionIsUnknownRef(t *testing.T) {
	_, adapter := newStub(t, nil)

	_, err := adapter.CompleteCardVerification(context.Background(), "cs_missing", nil)

	assert.ErrorIs(t, err, providers.ErrUnknownRef)
}

func charge() providers.ChargeRequest {
	return providers.ChargeRequest{
		Card:   providers.CardRef{CustomerRef: "cus_1", CardRef: "pm_1"},
		Amount: decimal.RequireFromString("120.50"), Currency: "USD", IdempotencyKey: "invoice:abc:attempt:1", Description: "NDR-2026-000001",
	}
}

func TestCharge_OffSessionSucceeded(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /v1/payment_intents": {200, `{"id":"pi_1","object":"payment_intent","status":"succeeded","amount":12050,"currency":"usd"}`},
	})

	result, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.ChargeResult{State: providers.StateSucceeded, ProviderPaymentRef: "pi_1"}, *result)
	sent := stub.form("POST /v1/payment_intents")
	assert.Equal(t, "12050", sent.Get("amount"))
	assert.Equal(t, "true", sent.Get("off_session"))
	assert.Equal(t, "true", sent.Get("confirm"))
	assert.Equal(t, "invoice:abc:attempt:1", stub.header("POST /v1/payment_intents").Get("Idempotency-Key"))
}

func TestCharge_AuthenticationRequiredIsConfirmationRequired(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"POST /v1/payment_intents": {402, `{"error":{"type":"card_error","code":"authentication_required","message":"This payment requires authentication.",
			"payment_intent":{"id":"pi_2","object":"payment_intent","status":"requires_payment_method"}}}`},
	})

	result, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.ChargeResult{State: providers.StateConfirmationRequired, ProviderPaymentRef: "pi_2"}, *result)
}

func TestCharge_DeclineIsFailedWithACategory(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"POST /v1/payment_intents": {402, `{"error":{"type":"card_error","code":"card_declined","decline_code":"insufficient_funds","message":"Your card has insufficient funds.",
			"payment_intent":{"id":"pi_3","object":"payment_intent","status":"requires_payment_method"}}}`},
	})

	result, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.StateFailed, result.State)
	assert.Equal(t, providers.ReasonInsufficientFunds, result.FailureReason)
	assert.Equal(t, "pi_3", result.ProviderPaymentRef)
}

func TestFetchPayment_MapsStatuses(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /v1/payment_intents/pi_ok":   {200, `{"id":"pi_ok","object":"payment_intent","status":"succeeded"}`},
		"GET /v1/payment_intents/pi_auth": {200, `{"id":"pi_auth","object":"payment_intent","status":"requires_action","next_action":{"type":"redirect_to_url","redirect_to_url":{"url":"https://hooks.stripe.com/3ds"}}}`},
		"GET /v1/payment_intents/pi_bad":  {200, `{"id":"pi_bad","object":"payment_intent","status":"requires_payment_method"}`},
		"GET /v1/payment_intents/pi_wait": {200, `{"id":"pi_wait","object":"payment_intent","status":"processing"}`},
	})

	cases := map[string]providers.PaymentState{
		"pi_ok": providers.StateSucceeded, "pi_auth": providers.StateConfirmationRequired,
		"pi_bad": providers.StateFailed, "pi_wait": providers.StatePending,
	}
	for ref, want := range cases {
		result, err := adapter.FetchPayment(context.Background(), ref)
		require.NoError(t, err)
		assert.Equal(t, want, result.State, ref)
	}
}

func TestCharge_RecordsTheAttemptKeyForTheLookup(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /v1/payment_intents": {200, `{"id":"pi_1","object":"payment_intent","status":"succeeded"}`},
	})

	_, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, "invoice:abc:attempt:1", stub.form("POST /v1/payment_intents").Get("metadata[idempotency_key]"))
}

func TestStartConfirmation_ConfirmsOnSessionAndAnswersTheRedirect(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /v1/payment_intents/pi_2/confirm": {200, `{"id":"pi_2","object":"payment_intent","status":"requires_action",
			"next_action":{"type":"redirect_to_url","redirect_to_url":{"url":"https://hooks.stripe.com/3ds/pi_2"}}}`},
	})
	req := charge()
	req.ProviderPaymentRef, req.ReturnURL = "pi_2", "https://billing.example/3ds/callback"

	confirmation, err := adapter.StartConfirmation(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, providers.Confirmation{ProviderPaymentRef: "pi_2", RedirectURL: "https://hooks.stripe.com/3ds/pi_2"}, *confirmation)
	assert.Equal(t, "https://billing.example/3ds/callback", stub.form("POST /v1/payment_intents/pi_2/confirm").Get("return_url"))
}

func TestCompleteConfirmation_TrustsTheIntentNotTheRedirect(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /v1/payment_intents/pi_2": {200, `{"id":"pi_2","object":"payment_intent","status":"succeeded","metadata":{"idempotency_key":"invoice:abc:attempt:1"}}`},
	})

	result, err := adapter.CompleteConfirmation(context.Background(), map[string]string{"payment_intent": "pi_2", "redirect_status": "failed"})

	require.NoError(t, err)
	assert.Equal(t, providers.ChargeResult{State: providers.StateSucceeded, ProviderPaymentRef: "pi_2", IdempotencyKey: "invoice:abc:attempt:1"}, *result)
	_, err = adapter.CompleteConfirmation(context.Background(), map[string]string{})
	assert.ErrorIs(t, err, providers.ErrBadSignature)
}

func TestFindCharge_SearchesByTheAttemptKey(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /v1/payment_intents/search": {200, `{"object":"search_result","has_more":false,"data":[
			{"id":"pi_1","object":"payment_intent","status":"succeeded","metadata":{"idempotency_key":"invoice:abc:attempt:1"}}]}`},
	})

	result, err := adapter.FindCharge(context.Background(), "invoice:abc:attempt:1")

	require.NoError(t, err)
	assert.Equal(t, providers.StateSucceeded, result.State)
	assert.Equal(t, "pi_1", result.ProviderPaymentRef)
}

func TestCardStatus_ReadsTheExpiry(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /v1/payment_methods/pm_1": {200, `{"id":"pm_1","object":"payment_method","card":{"brand":"visa","last4":"4242","exp_month":11,"exp_year":2026}}`},
	})

	status, err := adapter.CardStatus(context.Background(), providers.CardRef{CustomerRef: "cus_1", CardRef: "pm_1"})

	require.NoError(t, err)
	assert.Equal(t, providers.CardStatus{ExpMonth: 11, ExpYear: 2026}, *status)
}

const paymentSucceededEvent = `{"id":"evt_1","object":"event","type":"payment_intent.succeeded","api_version":"2020-08-27",
	"data":{"object":{"id":"pi_1","object":"payment_intent","status":"succeeded"}}}`

func TestVerifyWebhook_AcceptsAValidSignature(t *testing.T) {
	_, adapter := newStub(t, nil)
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: []byte(paymentSucceededEvent), Secret: webhookSecret, Timestamp: time.Now()})
	headers := http.Header{}
	headers.Set(stripe.SignatureHeader, signed.Header)

	event, err := adapter.VerifyWebhook(context.Background(), headers, signed.Payload)

	require.NoError(t, err)
	assert.Equal(t, providers.WebhookEvent{ProviderEventID: "evt_1", Kind: providers.WebhookPayment, ProviderPaymentRef: "pi_1", State: providers.StateSucceeded}, *event)
}

func TestVerifyWebhook_RejectsAWrongSecret(t *testing.T) {
	_, adapter := newStub(t, nil)
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: []byte(paymentSucceededEvent), Secret: "whsec_other", Timestamp: time.Now()})
	headers := http.Header{}
	headers.Set(stripe.SignatureHeader, signed.Header)

	_, err := adapter.VerifyWebhook(context.Background(), headers, signed.Payload)

	assert.ErrorIs(t, err, providers.ErrBadSignature)
}
