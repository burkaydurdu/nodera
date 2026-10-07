package consumers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/utils/str"
)

// storagePath opens the Storage the Notification is about. It names the Team
// and the Project too: a reader may be in several Teams, and the web app
// selects what the link names, as it does for Mail's links.
const storagePath = "/dashboard/storage?team=%s&project=%s&storage=%s"

// storagePauseReasons are the codes a Pause's email can word. A code this
// version does not know has no words, and a redelivery would not grow any.
var storagePauseReasons = map[string]bool{"abuse_complaint": true, "malware": true, "phishing": true, "legal": true}

// StorageEvents makes a Notification of everything the storage context tells a
// Team about a Storage. Params are what the web app words it from. Storage's
// events carry no link, so the one the email opens is built here from the web
// app's origin, and like every link it rides in the email params only.
type StorageEvents struct {
	notifier INotifier
	frontURL string
}

func NewStorageEvents(notifier INotifier, frontURL string) *StorageEvents {
	return &StorageEvents{notifier: notifier, frontURL: frontURL}
}

func (c *StorageEvents) RoutingKeys() []string {
	return []string{constants.StorageNearlyFull, constants.StorageFull, constants.StoragePaused, constants.StorageDowngradeSkipped}
}

func (c *StorageEvents) Consume(message *rabbitmq.Message) error {
	event, err := c.storageEvent(message)
	if err != nil {
		return err
	}
	if event == nil {
		return nil
	}

	return c.notifier.NotifyTeam(context.Background(), *event)
}

// storageEvent answers nil for a routing key this consumer does not handle.
func (c *StorageEvents) storageEvent(message *rabbitmq.Message) (*handlers.TeamEvent, error) {
	switch message.RoutingKey {
	case constants.StorageNearlyFull:
		var e events.StorageNearlyFullEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return c.fillEvent(notification.TypeStorageNearlyFull, e.Payload)
	case constants.StorageFull:
		var e events.StorageFullEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return c.fillEvent(notification.TypeStorageFull, e.Payload)
	case constants.StoragePaused:
		var e events.StoragePausedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		if !storagePauseReasons[p.Reason] {
			return nil, notification.ErrorMalformedEvent.Copy().SetMessage("unknown reason for a pause")
		}

		// The reason travels as a code: the web app and the email word it in
		// the reader's language.
		return c.occasion(p.Storage, notification.TypeStoragePaused, p.PausedAt,
			map[string]any{"reason": p.Reason},
			map[string]any{"ReasonKey": "storage_pause_reason_" + p.Reason})
	case constants.StorageDowngradeSkipped:
		var e events.StorageDowngradeSkippedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		if str.IsBlank(p.PackageCode) || str.IsBlank(p.SkippedPackageCode) || p.SkippedSizeBytes <= 0 || p.UsedBytes < 0 {
			return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing a package or a size")
		}

		return c.occasion(p.Storage, notification.TypeStorageDowngradeSkipped, p.At,
			map[string]any{
				"package_code": p.PackageCode, "skipped_package_code": p.SkippedPackageCode,
				"skipped_size_bytes": p.SkippedSizeBytes, "used_bytes": p.UsedBytes,
			},
			map[string]any{
				"PackageCode": p.PackageCode, "SkippedPackageCode": p.SkippedPackageCode,
				"SkippedSizeBytes": p.SkippedSizeBytes, "UsedBytes": p.UsedBytes,
			})
	default:
		return nil, nil
	}
}

// fillEvent is a mark of the Package being crossed. The sizes stay bytes in
// both sets of params: the web app and the email each print them for their
// reader.
func (c *StorageEvents) fillEvent(t notification.Type, p events.StorageFill) (*handlers.TeamEvent, error) {
	if str.IsBlank(p.PackageCode) || p.SizeBytes <= 0 || p.UsedBytes < 0 {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the package or a size")
	}

	return c.occasion(p.Storage, t, p.At,
		map[string]any{"package_code": p.PackageCode, "size_bytes": p.SizeBytes, "used_bytes": p.UsedBytes},
		map[string]any{"PackageCode": p.PackageCode, "SizeBytes": p.SizeBytes, "UsedBytes": p.UsedBytes})
}

// occasion names what happened by its type, the Storage and the moment, so a
// redelivered event records and sends nothing twice while the same thing
// happening to the Storage again, a second filling or a second Pause, is told
// again.
func (c *StorageEvents) occasion(st events.Storage, t notification.Type, at time.Time, params, emailParams map[string]any) (*handlers.TeamEvent, error) {
	if st.TeamID == uuid.Nil || st.ProjectID == uuid.Nil {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the team or the project")
	}
	if st.StorageID == uuid.Nil || str.IsBlank(st.BucketName) {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the storage")
	}
	if err := requireMoment(at); err != nil {
		return nil, err
	}
	params["project_id"], params["project_name"] = st.ProjectID.String(), st.ProjectName
	params["storage_id"], params["bucket_name"] = st.StorageID.String(), st.BucketName
	emailParams["ProjectName"], emailParams["BucketName"] = st.ProjectName, st.BucketName
	emailParams["StorageURL"] = c.frontURL + fmt.Sprintf(storagePath, st.TeamID, st.ProjectID, st.StorageID)

	return &handlers.TeamEvent{
		SourceKey: string(t) + ":" + st.StorageID.String() + ":" + momentOf(at),
		Type:      t, TeamID: st.TeamID, Params: params, EmailParams: emailParams,
	}, nil
}
