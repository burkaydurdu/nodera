package kumomta_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/mta"
	"github.com/piggystats/nodera/contexts/mail/mta/kumomta"
)

func injection() *mta.Injection {
	id := uuid.MustParse(id)

	return &mta.Injection{
		MessageID: id,
		Content:   []byte("From: a@example.com\r\n\r\nhi"),
		Envelope: message.Envelope{
			Recipients: []message.EnvelopeRecipient{
				{Email: "ana@example.net", Position: 0, ReturnPath: "b+" + id.String() + ".0.0123456789ab@n4k2x9q1z.example.com"},
				{Email: "bob@example.net", Position: 1, ReturnPath: "b+" + id.String() + ".1.ba9876543210@n4k2x9q1z.example.com"},
			},
			ProjectID:       uuid.New(),
			TeamID:          uuid.New(),
			SendingDomainID: uuid.New(),
			DKIMDomain:      "example.com",
		},
	}
}

func TestInject_SendsWhatThePolicyExpects(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "mail-service", user)
		assert.Equal(t, "secret", password)
		assert.Equal(t, "/api/inject/v1", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &got))
		_, _ = w.Write([]byte(`{"success_count":2,"fail_count":0,"failed_recipients":[],"errors":[]}`))
	}))
	defer server.Close()

	in := injection()
	result, err := kumomta.New(kumomta.Config{InjectionURL: server.URL + "/api/inject/v1", Username: "mail-service", Password: "secret"}).Inject(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, result.Failed)

	assert.Equal(t, in.Envelope.Recipients[0].ReturnPath, got["envelope_sender"])
	assert.Equal(t, "Static", got["template_dialect"])
	assert.Equal(t, string(in.Content), got["content"])
	trace := got["trace_headers"].(map[string]any)
	assert.Equal(t, false, trace["received_header"], "a trace_headers object turns the Received header on unless told otherwise")
	assert.Contains(t, trace["include_meta_names"], "x_return_path", "a feedback report names the copy by its return path")
	recipients := got["recipients"].([]any)
	require.Len(t, recipients, 2)
	meta := recipients[1].(map[string]any)["metadata"].(map[string]any)
	assert.Equal(t, in.MessageID.String(), meta["message_id"])
	assert.Equal(t, "1", meta["position"])
	assert.Equal(t, in.Envelope.Recipients[1].ReturnPath, meta["return_path"], "each copy goes out with its own return path")
	assert.Equal(t, in.Envelope.SendingDomainID.String(), meta["sending_domain_id"])
	assert.Equal(t, "example.com", meta["dkim_domain"])
	assert.Equal(t, in.Envelope.ProjectID.String(), meta["tenant"])
}

// KumoMTA answers 200 even when recipients fail; the failures are in the body.
func TestInject_ReportsRefusedRecipients(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success_count":1,"fail_count":1,"failed_recipients":["bob@example.net"],"errors":["bob@example.net: no dkim key"]}`))
	}))
	defer server.Close()

	result, err := kumomta.New(kumomta.Config{InjectionURL: server.URL}).Inject(context.Background(), injection())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"bob@example.net": "no dkim key"}, result.Failed)
}

func TestInject_AnUnavailableMTAIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := kumomta.New(kumomta.Config{InjectionURL: server.URL}).Inject(context.Background(), injection())
	assert.Error(t, err)
}
