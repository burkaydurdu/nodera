package craftgate_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/providers/craftgate"
	"github.com/piggystats/nodera/logger"
)

const (
	webhookKey  = "merchant-hook-key"
	callbackKey = "merchant-3ds-callback-key"
)

// recorded is one canned Craftgate answer: the shapes below are copied from
// the API's documented responses, trimmed to the fields the adapter reads.
type recorded struct {
	status int
	body   string
}

// craftgateStub answers by method and path and keeps every request body.
type craftgateStub struct {
	mu        sync.Mutex
	responses map[string]recorded
	requests  map[string]map[string]any
	calls     []string
}

func newStub(t *testing.T, responses map[string]recorded) (*craftgateStub, *craftgate.Craftgate) {
	t.Helper()
	stub := &craftgateStub{responses: responses, requests: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		stub.mu.Lock()
		stub.calls = append(stub.calls, key)
		stub.requests[key] = body
		// A search is answered per conversation id when the stub has one.
		res, ok := stub.responses[key+"?conversationId="+r.URL.Query().Get("conversationId")]
		if !ok {
			res, ok = stub.responses[key]
		}
		stub.mu.Unlock()

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":{"errorCode":"404","errorDescription":"not stubbed","errorGroup":"NOT_FOUND"}}`))

			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.status)
		_, _ = w.Write([]byte(res.body))
	}))
	t.Cleanup(server.Close)

	adapter, err := craftgate.New(craftgate.Config{APIKey: "api", SecretKey: "secret", BaseURL: server.URL, WebhookKey: webhookKey, ThreeDSCallbackKey: callbackKey}, logger.Noop())
	require.NoError(t, err)

	return stub, adapter
}

func (s *craftgateStub) request(key string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.requests[key]
}

func (s *craftgateStub) called(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if c == key {
			return true
		}
	}

	return false
}

func TestNew_RefusesMissingCredentials(t *testing.T) {
	_, err := craftgate.New(craftgate.Config{APIKey: "api"}, logger.Noop())
	assert.ErrorIs(t, err, craftgate.ErrMissingCredentials)

	_, err = craftgate.New(craftgate.Config{APIKey: "api", SecretKey: "s", WebhookKey: "w"}, logger.Noop())
	assert.ErrorIs(t, err, craftgate.ErrMissingCredentials, "the 3-D Secure callback key is required")
}

func TestStartCardVerification_OpensTheHostedFormAsARecurringPreAuth(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/checkout-payments/init": {200, `{"data":{"token":"tok-123","pageUrl":"https://checkout.example/tok-123"}}`},
	})

	v, err := adapter.StartCardVerification(context.Background(), providers.VerificationRequest{
		Payer: providers.Payer{TeamID: uuid.New()}, Currency: "TRY", ReturnURL: "https://app.example/billing/cards/complete?verification=1", ReferenceID: "ver-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "tok-123", v.Ref)
	assert.Equal(t, "https://checkout.example/tok-123", v.RedirectURL)
	sent := stub.request("POST /payment/v1/checkout-payments/init")
	assert.Equal(t, "LISTING_OR_SUBSCRIPTION", sent["paymentGroup"])
	assert.Equal(t, "PRE_AUTH", sent["paymentPhase"])
	assert.Equal(t, true, sent["forceThreeDS"])
	assert.Equal(t, true, sent["alwaysStoreCardAfterPayment"])
	assert.Equal(t, "TRY", sent["currency"])
	assert.Equal(t, "https://app.example/billing/cards/complete?verification=1", sent["callbackUrl"])
}

func TestCompleteCardVerification_ReadsTheStoredCardAndReleasesThePreAuth(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"GET /payment/v1/checkout-payments/tok-123": {200, `{"data":{"id":101,"paymentStatus":"SUCCESS","cardUserKey":"cuk-1","cardToken":"ctk-1",
			"lastFourDigits":"0008","cardAssociation":"MASTER_CARD","cardBrand":"World","additionalData":{"cardExpireMonth":"07","cardExpireYear":"2031"}}}`},
		"POST /payment/v1/refunds": {200, `{"data":{"id":9001,"status":"SUCCESS"}}`},
	})

	card, err := adapter.CompleteCardVerification(context.Background(), "tok-123", nil)

	require.NoError(t, err)
	assert.Equal(t, providers.SavedCard{CustomerRef: "cuk-1", CardRef: "ctk-1", Brand: "mastercard", Last4: "0008", ExpMonth: 7, ExpYear: 2031}, *card)
	assert.True(t, stub.called("POST /payment/v1/refunds"), "the verification hold is released")
}

func TestCompleteCardVerification_FailedCheckoutIsADecline(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /payment/v1/checkout-payments/tok-9": {200, `{"data":{"id":102,"paymentStatus":"FAILURE","paymentError":{"errorCode":"10051","errorDescription":"Yetersiz bakiye","errorGroup":"NOT_SUFFICIENT_FUNDS"}}}`},
	})

	_, err := adapter.CompleteCardVerification(context.Background(), "tok-9", nil)

	reason, declined := providers.Declined(err)
	require.True(t, declined, err)
	assert.Equal(t, providers.ReasonInsufficientFunds, reason)
}

func TestCompleteCardVerification_UnknownTokenIsUnknownRef(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /payment/v1/checkout-payments/nope": {400, `{"errors":{"errorCode":"5001","errorDescription":"checkout not found","errorGroup":"NOT_FOUND"}}`},
	})

	_, err := adapter.CompleteCardVerification(context.Background(), "nope", nil)

	assert.ErrorIs(t, err, providers.ErrUnknownRef)
}

func charge() providers.ChargeRequest {
	return providers.ChargeRequest{
		Card:   providers.CardRef{CustomerRef: "cuk-1", CardRef: "ctk-1"},
		Amount: decimal.RequireFromString("120.50"), Currency: "USD",
		IdempotencyKey: "invoice:abc:attempt:1", Description: "NDR-2026-000001", ReturnURL: "https://billing.example/3ds",
	}
}

func TestCharge_SucceedsWithoutThreeDS(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/card-payments": {200, `{"data":{"id":555,"paymentStatus":"SUCCESS","price":120.5,"paidPrice":120.5,"currency":"USD"}}`},
	})

	result, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.ChargeResult{State: providers.StateSucceeded, ProviderPaymentRef: "555", IdempotencyKey: "invoice:abc:attempt:1"}, *result)
	sent := stub.request("POST /payment/v1/card-payments")
	assert.Equal(t, "LISTING_OR_SUBSCRIPTION", sent["paymentGroup"])
	assert.Equal(t, "invoice:abc:attempt:1", sent["conversationId"])
	assert.InDelta(t, 120.5, sent["price"], 0.0001)
	card, _ := sent["card"].(map[string]any)
	assert.Equal(t, "cuk-1", card["cardUserKey"])
	assert.Equal(t, "ctk-1", card["cardToken"])
}

func TestCharge_HardDeclineIsFinal(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/card-payments": {400, `{"errors":{"errorCode":"10051","errorDescription":"Kart limiti yetersiz","errorGroup":"NOT_SUFFICIENT_FUNDS"}}`},
	})

	result, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.StateFailed, result.State)
	assert.Equal(t, providers.ReasonInsufficientFunds, result.FailureReason)
	assert.False(t, stub.called("POST /payment/v1/card-payments/3ds-init"))
}

// A refusal 3-D Secure can turn around waits for the cardholder: nothing is
// opened until they follow the link.
func TestCharge_SoftRefusalAwaitsTheCardholderWithoutOpening3DS(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/card-payments": {422, `{"errors":{"errorCode":"10005","errorDescription":"Islem onaylanmadi","errorGroup":"DO_NOT_HONOUR"}}`},
	})

	result, err := adapter.Charge(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.StateConfirmationRequired, result.State)
	assert.False(t, stub.called("POST /payment/v1/card-payments/3ds-init"))
}

// Only a bank decline is a decline: a validation or system error means
// nobody decided, so the attempt must stay open rather than fail.
func TestCharge_NonDeclineErrorIsUnavailable(t *testing.T) {
	for name, body := range map[string]string{
		"system error":     `{"errors":{"errorCode":"0","errorDescription":"Sistem hatasi","errorGroup":"SYSTEM_ERROR"}}`,
		"validation error": `{"errors":{"errorCode":"1","errorDescription":"price gecersiz","errorGroup":"VALIDATION_ERROR"}}`,
		"no group":         `{"errors":{"errorCode":"2","errorDescription":"?"}}`,
	} {
		_, adapter := newStub(t, map[string]recorded{"POST /payment/v1/card-payments": {500, body}})

		result, err := adapter.Charge(context.Background(), charge())

		require.Error(t, err, name)
		assert.Nil(t, result, name)
		_, declined := providers.Declined(err)
		assert.False(t, declined, name)
	}
}

func TestStartConfirmation_OpensThe3DSPaymentAndDecodesItsPage(t *testing.T) {
	html := base64.StdEncoding.EncodeToString([]byte("<html><body><form>3ds</form></body></html>"))
	stub, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/card-payments/3ds-init": {200, fmt.Sprintf(`{"data":{"paymentId":777,"paymentStatus":"INIT_THREEDS","htmlContent":%q}}`, html)},
	})

	confirmation, err := adapter.StartConfirmation(context.Background(), charge())

	require.NoError(t, err)
	assert.Equal(t, providers.Confirmation{ProviderPaymentRef: "777", HTML: "<html><body><form>3ds</form></body></html>"}, *confirmation)
	sent := stub.request("POST /payment/v1/card-payments/3ds-init")
	assert.Equal(t, "invoice:abc:attempt:1:3ds", sent["conversationId"])
	assert.Equal(t, "https://billing.example/3ds", sent["callbackUrl"])
}

func TestStartConfirmation_NeitherPageNorRedirectIsUnavailable(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/card-payments/3ds-init": {200, `{"data":{"paymentId":778,"paymentStatus":"INIT_THREEDS"}}`},
	})

	_, err := adapter.StartConfirmation(context.Background(), charge())

	require.Error(t, err)
	_, declined := providers.Declined(err)
	assert.False(t, declined)
}

// callback is Craftgate's 3-D Secure callback with its documented hash.
func callback(paymentID, status string) map[string]string {
	params := map[string]string{
		"status": status, "completeStatus": "WAITING", "paymentId": paymentID, "conversationData": "cd",
		"conversationId": "invoice:abc:attempt:1:3ds", "callbackStatus": "SUCCESS",
	}
	joined := strings.Join([]string{callbackKey, params["status"], params["completeStatus"], params["paymentId"],
		params["conversationData"], params["conversationId"], params["callbackStatus"]}, "###")
	sum := sha256.Sum256([]byte(joined))
	params["hash"] = hex.EncodeToString(sum[:])

	return params
}

func TestCompleteConfirmation_VerifiesTheHashThenCompletes(t *testing.T) {
	stub, adapter := newStub(t, map[string]recorded{
		"POST /payment/v1/card-payments/3ds-complete": {200, `{"data":{"id":777,"paymentStatus":"SUCCESS","conversationId":"invoice:abc:attempt:1:3ds"}}`},
	})

	result, err := adapter.CompleteConfirmation(context.Background(), callback("777", "SUCCESS"))

	require.NoError(t, err)
	assert.Equal(t, providers.ChargeResult{State: providers.StateSucceeded, ProviderPaymentRef: "777", IdempotencyKey: "invoice:abc:attempt:1"}, *result)
	assert.InDelta(t, 777, stub.request("POST /payment/v1/card-payments/3ds-complete")["paymentId"], 0)
}

func TestCompleteConfirmation_ForgedCallbackNeverReachesTheBank(t *testing.T) {
	stub, adapter := newStub(t, nil)
	forged := callback("777", "SUCCESS")
	forged["paymentId"] = "778"

	_, err := adapter.CompleteConfirmation(context.Background(), forged)

	assert.ErrorIs(t, err, providers.ErrBadSignature)
	assert.False(t, stub.called("POST /payment/v1/card-payments/3ds-complete"))
}

func TestCompleteConfirmation_FailedCardholderCheckIsFailed(t *testing.T) {
	stub, adapter := newStub(t, nil)

	result, err := adapter.CompleteConfirmation(context.Background(), callback("777", "FAILURE"))

	require.NoError(t, err)
	assert.Equal(t, providers.StateFailed, result.State)
	assert.False(t, stub.called("POST /payment/v1/card-payments/3ds-complete"))
}

// Craftgate does not dedupe by conversation id, so a lost answer is looked
// up by it: the captured payment wins over the 3-D Secure one still open.
func TestFindCharge_AdoptsTheCapturedPaymentOfTheAttempt(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /payment-reporting/v1/payments?conversationId=invoice:abc:attempt:1":     {200, `{"data":{"items":[{"id":555,"paymentStatus":"SUCCESS","conversationId":"invoice:abc:attempt:1"}]}}`},
		"GET /payment-reporting/v1/payments?conversationId=invoice:abc:attempt:1:3ds": {200, `{"data":{"items":[{"id":556,"paymentStatus":"INIT_THREEDS"}]}}`},
	})

	result, err := adapter.FindCharge(context.Background(), "invoice:abc:attempt:1")

	require.NoError(t, err)
	assert.Equal(t, providers.ChargeResult{State: providers.StateSucceeded, ProviderPaymentRef: "555", IdempotencyKey: "invoice:abc:attempt:1"}, *result)
}

func TestFindCharge_NothingFoundIsUnknownRef(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /payment-reporting/v1/payments": {200, `{"data":{"items":[]}}`},
	})

	_, err := adapter.FindCharge(context.Background(), "invoice:abc:attempt:2")

	assert.ErrorIs(t, err, providers.ErrUnknownRef)
}

func TestCardStatus_ReadsTheExpiryStatus(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /payment/v1/cards": {200, `{"data":{"items":[{"cardUserKey":"cuk-1","cardToken":"ctk-1","cardExpiryStatus":"WILL_EXPIRE_NEXT_MONTH"}]}}`},
	})

	status, err := adapter.CardStatus(context.Background(), providers.CardRef{CustomerRef: "cuk-1", CardRef: "ctk-1"})

	require.NoError(t, err)
	assert.Equal(t, providers.CardStatus{ExpiringSoon: true}, *status, "Craftgate reports the status, not the month")
}

func TestFetchPayment_MapsStatuses(t *testing.T) {
	_, adapter := newStub(t, map[string]recorded{
		"GET /payment/v1/card-payments/1": {200, `{"data":{"id":1,"paymentStatus":"SUCCESS","conversationId":"invoice:abc:attempt:1:3ds"}}`},
		"GET /payment/v1/card-payments/2": {200, `{"data":{"id":2,"paymentStatus":"FAILURE","paymentError":{"errorGroup":"EXPIRED_CARD"}}}`},
		"GET /payment/v1/card-payments/3": {200, `{"data":{"id":3,"paymentStatus":"INIT_THREEDS"}}`},
		"GET /payment/v1/card-payments/4": {200, `{"data":{"id":4,"paymentStatus":"WAITING"}}`},
	})

	cases := map[string]providers.PaymentState{
		"1": providers.StateSucceeded, "2": providers.StateFailed, "3": providers.StateConfirmationRequired, "4": providers.StatePending,
	}
	for ref, want := range cases {
		result, err := adapter.FetchPayment(context.Background(), ref)
		require.NoError(t, err)
		assert.Equal(t, want, result.State, ref)
	}
	_, err := adapter.FetchPayment(context.Background(), "not-a-number")
	assert.ErrorIs(t, err, providers.ErrUnknownRef)
	found, err := adapter.FetchPayment(context.Background(), "1")
	require.NoError(t, err)
	assert.Equal(t, "invoice:abc:attempt:1", found.IdempotencyKey, "a webhook naming an unknown payment is traced back to its attempt")
}

// sign reproduces Craftgate's documented webhook signature.
func sign(eventType string, timestamp int64, status, payloadID string) string {
	mac := hmac.New(sha256.New, []byte(webhookKey))
	_, _ = fmt.Fprintf(mac, "%s%d%s%s", eventType, timestamp, status, payloadID)

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook_AcceptsTheMerchantSignature(t *testing.T) {
	_, adapter := newStub(t, nil)
	body := []byte(`{"eventType":"API_AUTH","eventTime":"2026-10-01T10:00:00","eventTimestamp":1790000000,"status":"SUCCESS","payloadId":"555"}`)
	headers := http.Header{}
	headers.Set(craftgate.SignatureHeader, sign("API_AUTH", 1790000000, "SUCCESS", "555"))

	event, err := adapter.VerifyWebhook(context.Background(), headers, body)

	require.NoError(t, err)
	assert.Equal(t, providers.WebhookPayment, event.Kind)
	assert.Equal(t, "555", event.ProviderPaymentRef)
	assert.Equal(t, providers.StateSucceeded, event.State)
	assert.Equal(t, "API_AUTH:555:1790000000:SUCCESS", event.ProviderEventID)
}

func TestVerifyWebhook_RejectsATamperedBody(t *testing.T) {
	_, adapter := newStub(t, nil)
	body := []byte(`{"eventType":"API_AUTH","eventTimestamp":1790000000,"status":"SUCCESS","payloadId":"556"}`)
	headers := http.Header{}
	headers.Set(craftgate.SignatureHeader, sign("API_AUTH", 1790000000, "SUCCESS", "555"))

	_, err := adapter.VerifyWebhook(context.Background(), headers, body)
	assert.ErrorIs(t, err, providers.ErrBadSignature)

	_, err = adapter.VerifyWebhook(context.Background(), http.Header{}, body)
	assert.ErrorIs(t, err, providers.ErrBadSignature)
}
