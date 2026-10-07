package consumers_test

import (
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	mockconsumers "github.com/piggystats/nodera/contexts/notification/mock/consumers"
)

var storageAt = time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC)

func storagePayload(teamID, projectID, storageID uuid.UUID, fields map[string]any) map[string]any {
	payload := map[string]any{
		"team_id": teamID, "project_id": projectID, "project_name": "Shop", "storage_id": storageID, "bucket_name": "shop-assets",
	}
	maps.Copy(payload, fields)

	return payload
}

func storageFill(used int64) map[string]any {
	return map[string]any{"package_code": "10gb", "size_bytes": 10_000_000_000, "used_bytes": used, "at": storageAt}
}

func storageLink(teamID, projectID, storageID uuid.UUID) string {
	return frontURL + "/dashboard/storage?team=" + teamID.String() + "&project=" + projectID.String() + "&storage=" + storageID.String()
}

func TestStorageNearlyFull_IsNamedByTheStorageAndTheMomentItCrossedTheMark(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID, storageID := uuid.New(), uuid.New(), uuid.New()

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageNearlyFull,
		storagePayload(teamID, projectID, storageID, storageFill(8_100_000_000))))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeStorageNearlyFull, event.Type)
	assert.Equal(t, "storage_nearly_full:"+storageID.String()+":2026-10-06T09:30:00Z", event.SourceKey)
	assert.Equal(t, teamID, event.TeamID)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "storage_id": storageID.String(), "bucket_name": "shop-assets",
		"package_code": "10gb", "size_bytes": int64(10_000_000_000), "used_bytes": int64(8_100_000_000),
	}, event.Params)
	assert.Equal(t, map[string]any{
		"ProjectName": "Shop", "BucketName": "shop-assets", "StorageURL": storageLink(teamID, projectID, storageID),
		"PackageCode": "10gb", "SizeBytes": int64(10_000_000_000), "UsedBytes": int64(8_100_000_000),
	}, event.EmailParams, "the sizes stay bytes: the email prints them for its reader")
	assert.Empty(t, event.Also, "storage names nobody outside the Team")
}

func TestStorageFull_MayHoldMoreThanItsPackage(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID, storageID := uuid.New(), uuid.New(), uuid.New()

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageFull,
		storagePayload(teamID, projectID, storageID, storageFill(10_400_000_000))))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeStorageFull, event.Type)
	assert.Equal(t, "storage_full:"+storageID.String()+":2026-10-06T09:30:00Z", event.SourceKey)
	assert.Equal(t, int64(10_400_000_000), event.Params["used_bytes"])
	assert.Equal(t, int64(10_400_000_000), event.EmailParams["UsedBytes"])
	assert.Equal(t, storageLink(teamID, projectID, storageID), event.EmailParams["StorageURL"])
}

// A Storage that fell back under a mark and crossed it again is a new
// occasion, where a redelivery of the first crossing is not.
func TestStorageFull_ASecondFillingIsAnotherOccasion(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	first, again := toldTeam(t, notifier), toldTeam(t, notifier)
	teamID, projectID, storageID := uuid.New(), uuid.New(), uuid.New()
	later := storageFill(10_000_000_000)
	later["at"] = storageAt.Add(30 * 24 * time.Hour)
	consumer := consumers.NewStorageEvents(notifier, frontURL)

	require.NoError(t, consumer.Consume(message(t, constants.StorageFull, storagePayload(teamID, projectID, storageID, storageFill(10_000_000_000)))))
	require.NoError(t, consumer.Consume(message(t, constants.StorageFull, storagePayload(teamID, projectID, storageID, later))))

	assert.NotEqual(t, first.SourceKey, again.SourceKey)
}

func TestStoragePaused_CarriesTheReasonAsACodeAndWordsItInTheEmail(t *testing.T) {
	for _, reason := range []string{"abuse_complaint", "malware", "phishing", "legal"} {
		t.Run(reason, func(t *testing.T) {
			notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
			event := toldTeam(t, notifier)
			teamID, projectID, storageID := uuid.New(), uuid.New(), uuid.New()

			err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StoragePaused,
				storagePayload(teamID, projectID, storageID, map[string]any{"reason": reason, "paused_at": storageAt})))

			require.NoError(t, err)
			assert.Equal(t, notification.TypeStoragePaused, event.Type)
			assert.Equal(t, "storage_paused:"+storageID.String()+":2026-10-06T09:30:00Z", event.SourceKey)
			assert.Equal(t, map[string]any{
				"project_id": projectID.String(), "project_name": "Shop", "storage_id": storageID.String(), "bucket_name": "shop-assets",
				"reason": reason,
			}, event.Params, "the web app words the reason from its code; no English sentence is recorded")
			assert.Equal(t, "storage_pause_reason_"+reason, event.EmailParams["ReasonKey"])
			assert.Equal(t, storageLink(teamID, projectID, storageID), event.EmailParams["StorageURL"])
		})
	}
}

func TestStorageDowngradeSkipped_NamesThePackageItStaysOnAndTheOneItDidNotFit(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID, storageID := uuid.New(), uuid.New(), uuid.New()

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageDowngradeSkipped,
		storagePayload(teamID, projectID, storageID, map[string]any{
			"package_code": "50gb", "skipped_package_code": "10gb", "skipped_size_bytes": 10_000_000_000, "used_bytes": 12_300_000_000, "at": storageAt,
		})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeStorageDowngradeSkipped, event.Type)
	assert.Equal(t, "storage_downgrade_skipped:"+storageID.String()+":2026-10-06T09:30:00Z", event.SourceKey)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "storage_id": storageID.String(), "bucket_name": "shop-assets",
		"package_code": "50gb", "skipped_package_code": "10gb", "skipped_size_bytes": int64(10_000_000_000), "used_bytes": int64(12_300_000_000),
	}, event.Params)
	assert.Equal(t, map[string]any{
		"ProjectName": "Shop", "BucketName": "shop-assets", "StorageURL": storageLink(teamID, projectID, storageID),
		"PackageCode": "50gb", "SkippedPackageCode": "10gb", "SkippedSizeBytes": int64(10_000_000_000), "UsedBytes": int64(12_300_000_000),
	}, event.EmailParams)
}

func TestStoragePaused_WithAReasonThisVersionCannotWordIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StoragePaused,
		storagePayload(uuid.New(), uuid.New(), uuid.New(), map[string]any{"reason": "bad_taste", "paused_at": storageAt})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

// The moment names the occasion: without it a second filling could not be told
// from a redelivery of the first.
func TestStorageFull_WithoutItsTimeIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	fill := storageFill(10_000_000_000)
	delete(fill, "at")

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageFull,
		storagePayload(uuid.New(), uuid.New(), uuid.New(), fill)))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageEvent_WithoutItsTeamIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageNearlyFull,
		storagePayload(uuid.Nil, uuid.New(), uuid.New(), storageFill(8_100_000_000))))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageEvent_WithoutItsProjectIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StoragePaused,
		storagePayload(uuid.New(), uuid.Nil, uuid.New(), map[string]any{"reason": "malware", "paused_at": storageAt})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageEvent_WithoutItsStorageIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageFull,
		storagePayload(uuid.New(), uuid.New(), uuid.Nil, storageFill(10_000_000_000))))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageEvent_WithoutItsBucketNameIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	payload := storagePayload(uuid.New(), uuid.New(), uuid.New(), storageFill(8_100_000_000))
	payload["bucket_name"] = " "

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageNearlyFull, payload))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageFill_WithoutThePackagesSizeIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	fill := storageFill(8_100_000_000)
	delete(fill, "size_bytes")

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageNearlyFull,
		storagePayload(uuid.New(), uuid.New(), uuid.New(), fill)))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageDowngradeSkipped_WithoutThePackageItDidNotFitIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(message(t, constants.StorageDowngradeSkipped,
		storagePayload(uuid.New(), uuid.New(), uuid.New(), map[string]any{
			"package_code": "50gb", "skipped_size_bytes": 10_000_000_000, "used_bytes": 12_300_000_000, "at": storageAt,
		})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestStorageEvent_AMalformedPayloadIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewStorageEvents(notifier, frontURL).Consume(&rabbitmq.Message{RoutingKey: constants.StorageFull, Payload: []byte("{")})

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}
