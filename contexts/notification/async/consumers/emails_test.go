package consumers_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	mockconsumers "github.com/piggystats/nodera/contexts/notification/mock/consumers"
)

func message(t *testing.T, routingKey string, payload map[string]any) *rabbitmq.Message {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"payload": payload})
	require.NoError(t, err)

	return &rabbitmq.Message{RoutingKey: routingKey, Payload: raw, MessageID: "message-1"}
}

// told captures the one PersonEvent the consumer hands on.
func told(t *testing.T, notifier *mockconsumers.MockINotifier) *handlers.PersonEvent {
	t.Helper()
	var event handlers.PersonEvent
	notifier.EXPECT().NotifyPerson(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, e handlers.PersonEvent) error {
		event = e

		return nil
	})

	return &event
}

func TestEmailVerificationCode_CarriesItsCodeOnlyInTheEmail(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := told(t, notifier)
	userID := uuid.New()

	err := consumers.NewCustomerEmails(notifier).Consume(message(t, constants.EmailVerificationCode, map[string]any{
		"user_id": userID, "email": "ada@example.com", "full_name": "Ada", "locale": "tr", "code": "482913",
	}))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeEmailVerificationCode, event.Type)
	assert.Equal(t, "email_verification_code:message-1", event.SourceKey, "a redelivery keeps its message's id")
	assert.Equal(t, &userID, event.Recipient.UserID)
	assert.Equal(t, "ada@example.com", event.Recipient.Email)
	assert.Equal(t, "tr", event.Recipient.Locale)
	assert.Equal(t, "482913", event.EmailParams["Code"])
	assert.Empty(t, event.Params, "the code is never recorded")
}

func TestPasswordReset_KeepsTheLinkOutOfTheRecordedParams(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := told(t, notifier)

	err := consumers.NewCustomerEmails(notifier).Consume(message(t, constants.PasswordReset, map[string]any{
		"user_id": uuid.New(), "email": "ada@example.com", "locale": "en", "reset_url": "https://app/reset?token=secret",
	}))

	require.NoError(t, err)
	assert.Equal(t, "https://app/reset?token=secret", event.EmailParams["ResetURL"])
	assert.Empty(t, event.Params)
}

func TestInvitation_ToAnAddressWithoutAnAccountNamesNoUser(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := told(t, notifier)
	invitationID, teamID := uuid.New(), uuid.New()
	sentAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	err := consumers.NewCustomerEmails(notifier).Consume(message(t, constants.TeamInvitation, map[string]any{
		"invitation_id": invitationID, "team_id": teamID, "sent_at": sentAt,
		"recipient": map[string]any{"user_id": uuid.Nil, "email": "new@example.com", "locale": "en"},
		"team_name": "Acme", "role": "developer", "inviter_name": "Ayse",
		"accept_url": "https://app/invitations/accept?token=secret", "expires_at": sentAt.Add(7 * 24 * time.Hour),
	}))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeTeamInvitation, event.Type)
	assert.Equal(t, "team_invitation:"+invitationID.String()+":2026-10-02T09:00:00Z", event.SourceKey)
	assert.Nil(t, event.Recipient.UserID)
	assert.Nil(t, event.TeamID, "not a member yet")
	assert.Equal(t, "Acme", event.Params["team_name"])
	assert.NotContains(t, event.Params, "accept_url")
	assert.Equal(t, "https://app/invitations/accept?token=secret", event.EmailParams["AcceptURL"])
}

func TestInvitation_FromAProducerWithoutTheInvitationIsNamedByItsMessage(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := told(t, notifier)

	err := consumers.NewCustomerEmails(notifier).Consume(message(t, constants.TeamInvitation, map[string]any{
		"recipient": map[string]any{"email": "new@example.com", "locale": "en"}, "team_name": "Acme", "role": "developer",
	}))

	require.NoError(t, err)
	assert.Equal(t, "team_invitation:message-1", event.SourceKey)
}

func TestInvitation_WithARoleTheMatrixDoesNotKnowIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewCustomerEmails(notifier).Consume(message(t, constants.TeamInvitation, map[string]any{
		"recipient": map[string]any{"email": "new@example.com", "locale": "en"}, "role": "admin",
	}))

	require.Error(t, err)
}

func TestCustomerEmail_WithoutAnAddressIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewCustomerEmails(notifier).Consume(message(t, constants.MFAEmailCode, map[string]any{"user_id": uuid.New(), "code": "1"}))

	require.Error(t, err)
}

func TestCustomerEmail_AMalformedPayloadIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewCustomerEmails(notifier).Consume(&rabbitmq.Message{RoutingKey: constants.EmailAlreadyRegistered, Payload: []byte("{")})

	require.Error(t, err)
}
