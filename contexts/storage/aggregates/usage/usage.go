// Package usage is what a Storage held and moved over time: its size at the
// end of each UTC day and its traffic hour by hour, as the engine reports
// them. Nothing is billed from it (docs/adr/0012).
package usage

import (
	"time"

	"github.com/google/uuid"
)

// Days is how far back the web app is shown.
const Days = 30

type Size struct {
	StorageID   uuid.UUID
	Day         time.Time
	UsedBytes   int64
	ObjectCount int64
}

type Traffic struct {
	StorageID     uuid.UUID
	Hour          time.Time
	BytesSent     int64
	BytesReceived int64
	Requests      int64
}

// Day is one UTC day of a Storage as the web app sees it.
type Day struct {
	Day           time.Time
	UsedBytes     int64
	ObjectCount   int64
	BytesSent     int64
	BytesReceived int64
	Requests      int64
}

// DayOf is the UTC day holding t.
func DayOf(t time.Time) time.Time {
	u := t.UTC()

	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// HourOf is the UTC hour holding t.
func HourOf(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}
