package email

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const storageURL = "http://localhost:3002/dashboard/storage?team=t1&project=p1&storage=s1"

func storageParams() map[string]any {
	return map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "TeamName": "Acme", "ProjectName": "Shop", "BucketName": "shop-assets", "StorageURL": storageURL,
		"PackageCode": "10gb", "SizeBytes": int64(10_000_000_000), "UsedBytes": int64(8_100_000_000),
		"SkippedPackageCode": "10gb", "SkippedSizeBytes": int64(10_000_000_000),
		"ReasonKey": "storage_pause_reason_abuse_complaint",
	}
}

func TestSender_RendersEveryStorageKindInBothLocales(t *testing.T) {
	sender, client := newSender(t)
	kinds := []Kind{KindStorageNearlyFull, KindStorageFull, KindStoragePaused, KindStorageDowngradeSkipped}

	for _, kind := range kinds {
		for _, locale := range []string{"tr", "en"} {
			t.Run(string(kind)+"/"+locale, func(t *testing.T) {
				require.NoError(t, sender.Send(context.Background(), kind, "ada@example.com", locale, storageParams()))
				assert.Equal(t, []string{"ada@example.com"}, client.message.To)
				assert.Contains(t, client.message.Subject, "shop-assets", "the subject names the Storage by its Bucket name")
				assert.Contains(t, client.message.Body, "Ada")
				assert.Contains(t, client.message.Body, "Shop")
				assert.Contains(t, client.message.Body, "shop-assets")
				assert.Contains(t, client.message.Body, "Acme")
				assert.Contains(t, client.message.Body, "ada@example.com")
				assert.Contains(t, client.message.Body, `href="http://localhost:3002/dashboard/storage?team=t1&amp;project=p1&amp;storage=s1"`, "the button opens the Storage")
			})
		}
	}
}

func TestSender_StorageNearlyFull_ShowsHowFullInHumanSizes(t *testing.T) {
	sender, client := newSender(t)

	require.NoError(t, sender.Send(context.Background(), KindStorageNearlyFull, "ada@example.com", "en", storageParams()))
	assert.Equal(t, "Storage shop-assets is nearly full", client.message.Subject)
	assert.Contains(t, client.message.Body, "8.1 GB")
	assert.Contains(t, client.message.Body, "10 GB")
	assert.Contains(t, client.message.Body, "10gb")
	assert.NotContains(t, client.message.Body, "000000", "bytes are never printed raw")

	require.NoError(t, sender.Send(context.Background(), KindStorageNearlyFull, "ada@example.com", "tr", storageParams()))
	assert.Equal(t, "shop-assets adlı Storage dolmak üzere", client.message.Subject)
	assert.Contains(t, client.message.Body, "8,1 GB", "Turkish writes the decimals with a comma")
	assert.Contains(t, client.message.Body, "10 GB")
	assert.NotContains(t, client.message.Body, "000000")
}

func TestSender_StorageFull_SaysWritesStopWhileReadingAndDeletingGoOn(t *testing.T) {
	sender, client := newSender(t)
	params := storageParams()
	params["UsedBytes"] = int64(10_400_000_000)

	require.NoError(t, sender.Send(context.Background(), KindStorageFull, "ada@example.com", "en", params))
	assert.Equal(t, "Storage shop-assets is full", client.message.Subject)
	assert.Contains(t, client.message.Body, "10.4 GB")
	assert.Contains(t, client.message.Body, "takes no more writes")
	assert.Contains(t, client.message.Body, "Reading and deleting")

	require.NoError(t, sender.Send(context.Background(), KindStorageFull, "ada@example.com", "tr", params))
	assert.Equal(t, "shop-assets adlı Storage doldu", client.message.Subject)
	assert.Contains(t, client.message.Body, "10,4 GB")
	assert.Contains(t, client.message.Body, "yazma kabul etmez")
}

func TestSender_StoragePaused_WordsTheReason(t *testing.T) {
	sender, client := newSender(t)

	for _, c := range []struct{ locale, reason, subject, words string }{
		{"en", "abuse_complaint", "Storage shop-assets is paused", "a complaint was made about what it holds or publishes"},
		{"en", "malware", "Storage shop-assets is paused", "it was found to hold or spread malware"},
		{"en", "phishing", "Storage shop-assets is paused", "it was found to host phishing content"},
		{"en", "legal", "Storage shop-assets is paused", "a legal request requires it"},
		{"tr", "abuse_complaint", "shop-assets adlı Storage duraklatıldı", "barındırdığı ya da yayımladığı içerik hakkında bir şikâyet alındı"},
		{"tr", "malware", "shop-assets adlı Storage duraklatıldı", "zararlı yazılım barındırdığı ya da yaydığı tespit edildi"},
		{"tr", "phishing", "shop-assets adlı Storage duraklatıldı", "oltalama içeriği barındırdığı tespit edildi"},
		{"tr", "legal", "shop-assets adlı Storage duraklatıldı", "yasal bir talep bunu gerektiriyor"},
	} {
		params := storageParams()
		params["ReasonKey"] = "storage_pause_reason_" + c.reason

		require.NoError(t, sender.Send(context.Background(), KindStoragePaused, "ada@example.com", c.locale, params))
		assert.Equal(t, c.subject, client.message.Subject)
		assert.Contains(t, client.message.Body, c.words, "the reason in the reader's language")
		assert.NotContains(t, client.message.Body, "storage_pause_reason", "the raw key never reaches the reader")
		assert.NotContains(t, client.message.Body, "abuse_complaint")
	}
}

func TestSender_StorageDowngradeSkipped_NamesBothPackagesAndTheSizes(t *testing.T) {
	sender, client := newSender(t)
	params := storageParams()
	params["PackageCode"], params["UsedBytes"] = "50gb", int64(12_300_000_000)

	require.NoError(t, sender.Send(context.Background(), KindStorageDowngradeSkipped, "ada@example.com", "en", params))
	assert.Equal(t, "Storage shop-assets stays on its 50gb package", client.message.Subject)
	assert.Contains(t, client.message.Body, "12.3 GB")
	assert.Contains(t, client.message.Body, "10 GB")
	assert.Contains(t, client.message.Body, "10gb")
	assert.Contains(t, client.message.Body, "50gb")

	require.NoError(t, sender.Send(context.Background(), KindStorageDowngradeSkipped, "ada@example.com", "tr", params))
	assert.Equal(t, "shop-assets adlı Storage 50gb paketinde kalıyor", client.message.Subject)
	assert.Contains(t, client.message.Body, "12,3 GB")
	assert.Contains(t, client.message.Body, "10 GB")
}

func TestHumanSize_PrintsTheLargestUnitWithAtMostOneDecimal(t *testing.T) {
	for _, c := range []struct {
		bytes  int64
		en, tr string
	}{
		{0, "0 B", "0 B"},
		{512, "512 B", "512 B"},
		{1_000, "1 KB", "1 KB"},
		{1_500, "1.5 KB", "1,5 KB"},
		{999_999_999, "999.9 MB", "999,9 MB"},
		{1_000_000_000, "1 GB", "1 GB"},
		{8_100_000_000, "8.1 GB", "8,1 GB"},
		// Truncated, never rounded up: a nearly full Storage is never shown as
		// full.
		{9_999_999_999, "9.9 GB", "9,9 GB"},
		{250_000_000_000, "250 GB", "250 GB"},
		{1_250_000_000_000, "1.2 TB", "1,2 TB"},
	} {
		assert.Equal(t, c.en, humanSize(c.bytes, "en"), c.bytes)
		assert.Equal(t, c.tr, humanSize(c.bytes, "tr"), c.bytes)
	}
}
