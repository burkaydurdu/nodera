package integrationtest

import (
	"context"
	"net/http"

	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/providers/fake"
)

const craftgateWebhook = "/v1/billing/webhooks/craftgate"

func (s *billingSuite) webhookRows() int {
	var n int
	s.Require().NoError(s.pgPool.QueryRow(context.Background(), `SELECT count(*) FROM webhook_events`).Scan(&n))

	return n
}

func (s *billingSuite) TestWebhook_BadSignatureIsUnauthorized() {
	headers, body := fake.Webhook(fake.WebhookBody{EventID: "evt-1", Kind: "payment", PaymentRef: "pay-1", Status: "succeeded"})
	headers.Set(fake.SignatureHeader, "forged")

	res := s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers)

	s.assertError(res, http.StatusUnauthorized, int(payment.Domain), payment.ErrorWebhookSignature.Code)
	s.Equal(0, s.webhookRows())
}

func (s *billingSuite) TestWebhook_ValidEventIsStoredOnce() {
	headers, body := fake.Webhook(fake.WebhookBody{EventID: "evt-2", Kind: "payment", PaymentRef: "pay-2", Status: "succeeded"})

	first := s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers)
	replay := s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers)

	s.Equal(http.StatusOK, first.Code, first.Raw)
	s.Equal(true, first.Body["received"])
	s.Equal(http.StatusOK, replay.Code, "a replay is acknowledged: %s", replay.Raw)
	s.Equal(1, s.webhookRows())

	pending, err := s.services.Payments.FindUnprocessedWebhookEvents(context.Background(), 10)
	s.Require().NoError(err)
	s.Require().Len(pending, 1)
	s.Equal("craftgate", pending[0].Provider)
	s.Equal("evt-2", pending[0].ProviderEventID)
	s.Equal("pay-2", pending[0].ProviderPaymentRef)
	s.Equal("succeeded", pending[0].Status)
	s.Equal(body, pending[0].Body, "the raw body is kept as received")
}

// The Stripe adapter is built but not configured, so its route has nothing
// to verify against.
func (s *billingSuite) TestWebhook_UnconfiguredProviderIsNotFound() {
	headers, body := fake.Webhook(fake.WebhookBody{EventID: "evt-3"})

	res := s.requestWith(http.MethodPost, "/v1/billing/webhooks/stripe", string(body), "", headers)

	s.assertError(res, http.StatusNotFound, int(payment.Domain), payment.ErrorUnknownWebhook.Code)
}
