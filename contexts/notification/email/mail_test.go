package email

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mailURL = "http://localhost:3002/dashboard/mail?team=t1&project=p1"

func mailParams() map[string]any {
	return map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "TeamName": "Acme", "ProjectName": "Shop", "MailURL": mailURL,
		"Domain": "mail.shop.example", "PackageCode": "10k", "Allowance": 10000, "Used": 8000,
		"RenewsAt":   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		"BounceRate": json.Number("5.40"), "ComplaintRate": json.Number("0.02"), "WindowDays": 7,
		"ReasonKey": "mail_pause_reason_bounce_rate",
	}
}

func TestSender_RendersEveryMailKindInBothLocales(t *testing.T) {
	sender, client := newSender(t)
	kinds := []Kind{
		KindMailDomainVerified, KindMailDomainUnverified, KindMailPackageNearLimit, KindMailPackageExhausted,
		KindMailRatesWarning, KindMailPaused,
	}

	for _, kind := range kinds {
		for _, locale := range []string{"tr", "en"} {
			t.Run(string(kind)+"/"+locale, func(t *testing.T) {
				require.NoError(t, sender.Send(context.Background(), kind, "ada@example.com", locale, mailParams()))
				assert.Equal(t, []string{"ada@example.com"}, client.message.To)
				assert.Contains(t, client.message.Subject, "Shop", "the subject names the Project")
				assert.Contains(t, client.message.Body, "Ada")
				assert.Contains(t, client.message.Body, "Shop")
				assert.Contains(t, client.message.Body, "Acme")
				assert.Contains(t, client.message.Body, "ada@example.com")
				assert.Contains(t, client.message.Body, `href="http://localhost:3002/dashboard/mail?team=t1&amp;project=p1"`, "the button opens the Mail App")
			})
		}
	}
}

func TestSender_MailDomainVerified_NamesTheDomain(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMailDomainVerified, "ada@example.com", "en", mailParams()))
	assert.Equal(t, "mail.shop.example is verified for Shop", client.message.Subject)
	assert.Contains(t, client.message.Body, "mail.shop.example")

	require.NoError(t, sender.Send(context.Background(), KindMailDomainVerified, "ada@example.com", "tr", mailParams()))
	assert.Equal(t, "mail.shop.example, Shop projesi için doğrulandı", client.message.Subject)
	assert.Contains(t, client.message.Body, "mail.shop.example")
}

func TestSender_MailDomainUnverified_SaysSendingFromItStopped(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMailDomainUnverified, "ada@example.com", "en", mailParams()))
	assert.Equal(t, "mail.shop.example is no longer verified for Shop", client.message.Subject)
	assert.Contains(t, client.message.Body, "Sending from mail.shop.example has stopped")

	require.NoError(t, sender.Send(context.Background(), KindMailDomainUnverified, "ada@example.com", "tr", mailParams()))
	assert.Equal(t, "mail.shop.example, Shop projesi için artık doğrulanmış değil", client.message.Subject)
	assert.Contains(t, client.message.Body, "mail.shop.example üzerinden gönderim durdu")
}

func TestSender_MailPackageNearLimit_ShowsTheUsageAndWhenItRenews(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMailPackageNearLimit, "ada@example.com", "en", mailParams()))
	assert.Equal(t, "Shop is close to the limit of its 10k Mail package", client.message.Subject)
	assert.Contains(t, client.message.Body, "8000")
	assert.Contains(t, client.message.Body, "10000")
	assert.Contains(t, client.message.Body, "November 1, 2026")

	require.NoError(t, sender.Send(context.Background(), KindMailPackageNearLimit, "ada@example.com", "tr", mailParams()))
	assert.Equal(t, "Shop projesi 10k Mail paketinin sınırına yaklaştı", client.message.Subject)
	assert.Contains(t, client.message.Body, "8000")
	assert.Contains(t, client.message.Body, "10000")
	assert.Contains(t, client.message.Body, "01.11.2026")
}

func TestSender_MailPackageExhausted_SaysSendingStopsUntilItRenews(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMailPackageExhausted, "ada@example.com", "en", mailParams()))
	assert.Equal(t, "Shop has used up its 10k Mail package", client.message.Subject)
	assert.Contains(t, client.message.Body, "10000")
	assert.Contains(t, client.message.Body, "November 1, 2026")

	require.NoError(t, sender.Send(context.Background(), KindMailPackageExhausted, "ada@example.com", "tr", mailParams()))
	assert.Equal(t, "Shop projesi 10k Mail paketini tüketti", client.message.Subject)
	assert.Contains(t, client.message.Body, "01.11.2026")
}

func TestSender_MailRatesWarning_ShowsBothRatesOverTheirWindow(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindMailRatesWarning, "ada@example.com", "en", mailParams()))
	assert.Equal(t, "High bounce or complaint rate in Shop", client.message.Subject)
	assert.Contains(t, client.message.Body, "5.40%")
	assert.Contains(t, client.message.Body, "0.02%")
	assert.Contains(t, client.message.Body, "7 days")

	require.NoError(t, sender.Send(context.Background(), KindMailRatesWarning, "ada@example.com", "tr", mailParams()))
	assert.Equal(t, "Shop projesinde geri dönme ya da şikâyet oranı yüksek", client.message.Subject)
	assert.Contains(t, client.message.Body, "%5.40")
	assert.Contains(t, client.message.Body, "%0.02")
	assert.Contains(t, client.message.Body, "7 günde")
}

func TestSender_MailPaused_WordsTheReasonAndShowsBothRates(t *testing.T) {
	sender, client := newSender(t)
	complaints := mailParams()
	complaints["ReasonKey"] = "mail_pause_reason_complaint_rate"

	for _, c := range []struct {
		locale, subject, reason string
		params                  map[string]any
		rates                   [2]string
	}{
		{"en", "Mail sending is paused for Shop", "too much of its mail bounced", mailParams(), [2]string{"5.40%", "0.02%"}},
		{"en", "Mail sending is paused for Shop", "too many recipients reported its mail as spam", complaints, [2]string{"5.40%", "0.02%"}},
		{"tr", "Shop projesinin Mail gönderimi duraklatıldı", "e-postalarının çok fazlası geri döndü", mailParams(), [2]string{"%5.40", "%0.02"}},
		{"tr", "Shop projesinin Mail gönderimi duraklatıldı", "çok fazla alıcı e-postalarını spam olarak bildirdi", complaints, [2]string{"%5.40", "%0.02"}},
	} {
		require.NoError(t, sender.Send(context.Background(), KindMailPaused, "ada@example.com", c.locale, c.params))
		assert.Equal(t, c.subject, client.message.Subject)
		assert.Contains(t, client.message.Body, c.reason, "the reason in the reader's language")
		assert.Contains(t, client.message.Body, c.rates[0])
		assert.Contains(t, client.message.Body, c.rates[1])
		assert.NotContains(t, client.message.Body, "bounce_rate", "the raw code never reaches the reader")
		assert.NotContains(t, client.message.Body, "complaint_rate")
	}
}
