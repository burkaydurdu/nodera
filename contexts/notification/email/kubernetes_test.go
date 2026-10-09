package email

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSender_KubernetesClusterFailedReadsTheNotificationParams(t *testing.T) {
	sender, client := newSender(t)
	params := map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "TeamName": "Acme",
		"Name": "prod", "ProjectName": "Shop", "FailureKey": "kubernetes_failure_control_plane_failed",
	}

	for locale, want := range map[string][2]string{
		"en": {"Cluster prod could not be set up", "the control plane was not ready in time"},
		"tr": {"prod cluster'ı kurulamadı", "control plane zamanında hazır olmadı"},
	} {
		t.Run(locale, func(t *testing.T) {
			require.NoError(t, sender.Send(context.Background(), KindKubernetesClusterFailed, "ada@example.com", locale, params))
			assert.Equal(t, want[0], client.message.Subject)
			assert.Contains(t, client.message.Body, "prod")
			assert.Contains(t, client.message.Body, "Shop")
			assert.Contains(t, client.message.Body, want[1], "the reason in the reader's language")
		})
	}
}
