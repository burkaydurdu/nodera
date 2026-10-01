package email

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	params := map[string]any{"FullName": "Ada", "Code": "123456", "ResetURL": "http://localhost:3002/reset-password?token=abc"}

	for _, kind := range []Kind{KindEmailVerificationCode, KindEmailAlreadyRegistered, KindMFAEmailCode, KindPasswordReset} {
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
