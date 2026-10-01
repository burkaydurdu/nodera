package email

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/pkg/clients/mail"
)

type capturingClient struct {
	message *mail.Message
}

func (c *capturingClient) Send(_ context.Context, message *mail.Message) error {
	c.message = message

	return nil
}

func templatesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(file), "..", "..", "..", "templates", "notification")
}

func newSender(t *testing.T) (*Sender, *capturingClient) {
	t.Helper()
	client := &capturingClient{}

	return NewSender(Config{From: "no-reply@nodera.test", TemplatesDir: templatesDir(t)}, client), client
}

func TestSender_RendersEveryKindInBothLocales(t *testing.T) {
	sender, client := newSender(t)
	params := map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "Code": "123456", "ResetURL": "http://localhost:3002/reset-password?token=abc",
		"TeamName": "Platform", "Role": "developer", "InviterName": "Grace", "AcceptURL": "http://localhost:3002/invitations/accept?token=abc",
		"ExpiresAt": time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}

	for _, kind := range []Kind{KindEmailVerificationCode, KindEmailAlreadyRegistered, KindMFAEmailCode, KindPasswordReset, KindTeamInvitation} {
		for _, locale := range []string{"tr", "en"} {
			t.Run(string(kind)+"/"+locale, func(t *testing.T) {
				require.NoError(t, sender.Send(context.Background(), kind, "ada@example.com", locale, params))
				assert.Equal(t, []string{"ada@example.com"}, client.message.To)
				assert.Equal(t, "no-reply@nodera.test", client.message.From)
				assert.NotEmpty(t, client.message.Subject)
				assert.Contains(t, client.message.Body, "Ada")
			})
		}
	}
}

func TestSender_TurkishLocale_UsesTurkishSubjectAndBody(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMFAEmailCode, "ada@example.com", "tr", map[string]any{"FullName": "Ada", "Code": "123456"}))

	assert.Equal(t, "Nodera giriş kodun", client.message.Subject)
	assert.Contains(t, client.message.Body, "Merhaba Ada")
	assert.Contains(t, client.message.Body, "123456")
}

func TestSender_UnknownLocale_FallsBackToEnglish(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindPasswordReset, "ada@example.com", "de", map[string]any{"FullName": "Ada", "ResetURL": "http://x/reset?token=1"}))

	assert.Equal(t, "Reset your Nodera password", client.message.Subject)
	assert.Contains(t, client.message.Body, `href="http://x/reset?token=1"`)
}

func TestSender_EscapesTheRecipientName(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMFAEmailCode, "ada@example.com", "en", map[string]any{"FullName": "<script>x</script>", "Code": "123456"}))

	assert.False(t, strings.Contains(client.message.Body, "<script>"))
}

func TestSender_EmptyRecipient_IsTerminal(t *testing.T) {
	sender, _ := newSender(t)

	err := sender.Send(context.Background(), KindMFAEmailCode, "", "en", nil)

	assert.ErrorIs(t, err, ErrorInvalidRecipient)
}

func teamInvitationParams() map[string]any {
	return map[string]any{
		"FullName": "", "Email": "grace@example.com", "TeamName": "Platform", "Role": "developer", "InviterName": "Ada Lovelace",
		"AcceptURL": "http://localhost:3002/invitations/accept?token=abc", "ExpiresAt": time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}
}

func TestSender_TeamInvitation_English_TranslatesTheRoleAndNamesTheInviter(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindTeamInvitation, "grace@example.com", "en", teamInvitationParams()))

	assert.Equal(t, "Ada Lovelace invited you to join Platform on Nodera", client.message.Subject)
	assert.Contains(t, client.message.Body, "Ada Lovelace")
	assert.Contains(t, client.message.Body, "Platform")
	assert.Contains(t, client.message.Body, "Developer")
	assert.NotContains(t, client.message.Body, "developer", "the raw Role never reaches the reader")
	assert.Contains(t, client.message.Body, `href="http://localhost:3002/invitations/accept?token=abc"`)
	assert.Contains(t, client.message.Body, "October 8, 2026")
	assert.Contains(t, client.message.Body, "grace@example.com")
}

func TestSender_TeamInvitation_Turkish_TranslatesTheRole(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindTeamInvitation, "grace@example.com", "tr", teamInvitationParams()))

	assert.Equal(t, "Ada Lovelace seni Nodera'da Platform takımına davet etti", client.message.Subject)
	assert.Contains(t, client.message.Body, "Geliştirici")
	assert.NotContains(t, client.message.Body, "developer")
	assert.Contains(t, client.message.Body, "08.10.2026")
	assert.Contains(t, client.message.Body, "grace@example.com")
}

func TestSender_TeamInvitation_FinanceRole_InBothLocales(t *testing.T) {
	sender, client := newSender(t)
	params := teamInvitationParams()
	params["Role"] = "finance"

	require.NoError(t, sender.Send(context.Background(), KindTeamInvitation, "grace@example.com", "en", params))
	assert.Contains(t, client.message.Body, "Finance")

	require.NoError(t, sender.Send(context.Background(), KindTeamInvitation, "grace@example.com", "tr", params))
	assert.Contains(t, client.message.Body, "Finans")
}
