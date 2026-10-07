package producers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/async/constants"
	"github.com/piggystats/nodera/contexts/storage/async/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

// Announcer publishes everything this context tells others. Timestamps go out
// in UTC so every consumer reads the same instant whatever zone the database
// handed back.
type Announcer struct {
	client OutboxClient
}

func NewAnnouncer(client OutboxClient) *Announcer {
	return &Announcer{client: client}
}

func (a *Announcer) PackageStarted(ctx context.Context, project *access.ProjectAccess, s *storage.Storage) error {
	return publish(ctx, a.client, constants.StoragePackageStarted, &events.StoragePackageStartedEvent{Payload: events.StoragePackageStartedBody{
		StorageID: s.ID, TeamID: s.TeamID, ProjectID: s.ProjectID, ProjectName: project.Name, BucketName: s.BucketName,
		Region: s.Region, PackageCode: s.Package.Code, SizeBytes: s.Package.SizeBytes,
		MonthlyPrice: s.Package.MonthlyPrice.StringFixed(storage.PriceScale), StartedAt: s.Package.StartedAt.UTC(),
	}})
}

func (a *Announcer) PackageStopped(ctx context.Context, teamID, storageID uuid.UUID, startedAt, stoppedAt time.Time) error {
	return publish(ctx, a.client, constants.StoragePackageStopped, &events.StoragePackageStoppedEvent{Payload: events.StoragePackageStoppedBody{
		StorageID: storageID, TeamID: teamID, StartedAt: startedAt.UTC(), StoppedAt: stoppedAt.UTC(),
	}})
}

func (a *Announcer) NearlyFull(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, at time.Time) error {
	return publish(ctx, a.client, constants.StorageNearlyFull, &events.StorageNearlyFullEvent{Payload: fill(project, s, at)})
}

func (a *Announcer) Full(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, at time.Time) error {
	return publish(ctx, a.client, constants.StorageFull, &events.StorageFullEvent{Payload: fill(project, s, at)})
}

func (a *Announcer) Paused(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, p storage.Pause) error {
	return publish(ctx, a.client, constants.StoragePaused, &events.StoragePausedEvent{Payload: events.StoragePausedBody{
		TeamID: s.TeamID, ProjectID: s.ProjectID, ProjectName: project.Name, StorageID: s.ID, BucketName: s.BucketName,
		Reason: string(p.Reason), PausedAt: p.At.UTC(),
	}})
}

func (a *Announcer) DowngradeSkipped(ctx context.Context, project *access.ProjectAccess, s *storage.Storage, skipped *storage.Scheduled, at time.Time) error {
	return publish(ctx, a.client, constants.StorageDowngradeSkipped, &events.StorageDowngradeSkippedEvent{Payload: events.StorageDowngradeSkippedBody{
		TeamID: s.TeamID, ProjectID: s.ProjectID, ProjectName: project.Name, StorageID: s.ID, BucketName: s.BucketName,
		PackageCode: s.Package.Code, SkippedPackageCode: skipped.Code, SkippedSizeBytes: skipped.SizeBytes,
		UsedBytes: s.UsedBytes, At: at.UTC(),
	}})
}

// liveStorage is what the web app refetches by.
type liveStorage struct {
	ProjectID uuid.UUID `json:"project_id"`
	StorageID uuid.UUID `json:"storage_id"`
}

// StorageChanged reaches the open web apps of the Team's members who may see
// Storage (docs/adr/0007); the web app refetches.
func (a *Announcer) StorageChanged(ctx context.Context, teamID, projectID, storageID uuid.UUID, at time.Time) error {
	message, err := realtime.New(realtime.ToTeam(teamID, authz.StorageRead), constants.RealtimeStorageChanged, liveStorage{ProjectID: projectID, StorageID: storageID}, at)
	if err != nil {
		return err
	}

	return publish(ctx, a.client, realtime.RoutingKey, message)
}

func fill(project *access.ProjectAccess, s *storage.Storage, at time.Time) events.StorageFillBody {
	return events.StorageFillBody{
		TeamID: s.TeamID, ProjectID: s.ProjectID, ProjectName: project.Name, StorageID: s.ID, BucketName: s.BucketName,
		PackageCode: s.Package.Code, SizeBytes: s.Package.SizeBytes, UsedBytes: s.UsedBytes, At: at.UTC(),
	}
}
