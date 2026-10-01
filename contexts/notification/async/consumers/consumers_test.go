package consumers

import (
	"context"
	"errors"
	"testing"

	"github.com/piggystats/rabbitmq"
	"github.com/piggystats/rerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/pkg/clients/mail"
)

// recordingSender captures what a consumer asked to send.
type recordingSender struct {
	kind   email.Kind
	to     string
	locale string
	params map[string]any
	err    error
}

func (r *recordingSender) Send(_ context.Context, kind email.Kind, to, locale string, params map[string]any) error {
	r.kind, r.to, r.locale, r.params = kind, to, locale, params

	return r.err
}

func message(payload string) *rabbitmq.Message {
	return &rabbitmq.Message{Payload: []byte(payload), CorrelationID: "corr-1"}
}

func TestEmailVerificationCode_SendsCodeInRecipientLocale(t *testing.T) {
	sender := &recordingSender{}

	err := NewEmailVerificationCode(sender).Consume(message(
		`{"payload":{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada","locale":"tr","code":"123456"}}`))

	require.NoError(t, err)
	assert.Equal(t, email.KindEmailVerificationCode, sender.kind)
	assert.Equal(t, "ada@example.com", sender.to)
	assert.Equal(t, "tr", sender.locale)
	assert.Equal(t, "123456", sender.params["Code"])
	assert.Equal(t, "Ada", sender.params["FullName"])
}

func TestMFAEmailCode_SendsCode(t *testing.T) {
	sender := &recordingSender{}

	err := NewMFAEmailCode(sender).Consume(message(
		`{"payload":{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada","locale":"en","code":"654321"}}`))

	require.NoError(t, err)
	assert.Equal(t, email.KindMFAEmailCode, sender.kind)
	assert.Equal(t, "654321", sender.params["Code"])
}

func TestPasswordReset_SendsTheLink(t *testing.T) {
	sender := &recordingSender{}

	err := NewPasswordReset(sender).Consume(message(
		`{"payload":{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada","locale":"en","reset_url":"http://localhost:3002/reset-password?token=abc"}}`))

	require.NoError(t, err)
	assert.Equal(t, email.KindPasswordReset, sender.kind)
	assert.Equal(t, "http://localhost:3002/reset-password?token=abc", sender.params["ResetURL"])
}

func TestEmailAlreadyRegistered_SendsToTheOwner(t *testing.T) {
	sender := &recordingSender{}

	err := NewEmailAlreadyRegistered(sender).Consume(message(
		`{"payload":{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada","locale":"tr"}}`))

	require.NoError(t, err)
	assert.Equal(t, email.KindEmailAlreadyRegistered, sender.kind)
	assert.Equal(t, "tr", sender.locale)
}

// A payload that cannot be decoded would fail identically on every redelivery,
// so it must come back as a terminal domain error, never a plain one.
func TestConsume_MalformedPayload_IsTerminal(t *testing.T) {
	err := NewMFAEmailCode(&recordingSender{}).Consume(message(`{not json`))

	var rerr rerror.Error
	require.True(t, errors.As(err, &rerr))
	assert.False(t, rerr.IsRetryable)
}

// A transport failure must propagate unchanged so the broker retries it.
func TestConsume_TransportFailure_IsRetryable(t *testing.T) {
	sender := &recordingSender{err: mail.ErrFailedToSendEmail.Copy().SetError(errors.New("smtp down"))}

	err := NewMFAEmailCode(sender).Consume(message(
		`{"payload":{"user_id":"019a0000-0000-7000-8000-000000000001","email":"ada@example.com","full_name":"Ada","locale":"en","code":"654321"}}`))

	assert.True(t, rerror.IsRetryable(err))
}
