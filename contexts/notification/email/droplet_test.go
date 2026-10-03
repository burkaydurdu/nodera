package email

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSender_DropletFailedReadsTheNotificationParams(t *testing.T) {
	sender, client := newSender(t)
	params := map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "TeamName": "Acme",
		"Hostname": "web-1", "FailureKey": "droplet_failure_rejected",
	}

	for locale, want := range map[string][2]string{
		"en": {"web-1 could not be created", "the order was refused"},
		"tr": {"web-1 oluşturulamadı", "sipariş reddedildi"},
	} {
		t.Run(locale, func(t *testing.T) {
			require.NoError(t, sender.Send(context.Background(), KindDropletFailed, "ada@example.com", locale, params))
			assert.Equal(t, want[0], client.message.Subject)
			assert.Contains(t, client.message.Body, "web-1")
			assert.Contains(t, client.message.Body, "Acme")
			assert.Contains(t, client.message.Body, want[1], "the reason in the reader's language")
		})
	}
}
