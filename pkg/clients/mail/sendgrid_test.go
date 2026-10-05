package mail_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/pkg/clients/mail"
)

func sendgridAnswering(t *testing.T, status int) mail.IClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"errors":[{"message":"the from address does not match a verified Sender Identity"}]}`))
	}))
	t.Cleanup(server.Close)

	return mail.NewSendgridClient(&mail.SendgridConfig{APIKey: "SG.test", FromName: "Nodera", Host: server.URL})
}

func message() *mail.Message {
	return &mail.Message{From: "no-reply@nodera.io", To: []string{"person@example.com"}, Subject: "Code", Body: "<p>123456</p>"}
}

func TestSendgridSend_AcceptedIsDelivered(t *testing.T) {
	require.NoError(t, sendgridAnswering(t, http.StatusAccepted).Send(context.Background(), message()))
}

// sendgrid-go returns no error for a 4xx or 5xx answer; a rejected email
// must still fail the delivery instead of being recorded as sent.
func TestSendgridSend_RejectionIsAFailure(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		err := sendgridAnswering(t, status).Send(context.Background(), message())
		assert.ErrorIsf(t, err, mail.ErrFailedToSendEmail, "status %d", status)
	}
}
