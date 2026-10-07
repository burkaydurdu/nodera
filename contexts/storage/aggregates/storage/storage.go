// Package storage is the Storage: one S3 bucket a Project keeps files in,
// under a Bucket name, in a Region and on a Package, with the Pause Nodera
// may put on it and what it was last measured to hold.
package storage

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PriceScale is the two decimals a monthly price carries on the wire.
const PriceScale = 2

// NearlyFullPercent is where a Team is told a Storage is filling up.
const NearlyFullPercent = 80

// MaxPerProject bounds how many Storages a Project holds at once.
const MaxPerProject = 100

type Status string

const (
	// StatusOpening holds the Bucket name while the engine makes the bucket;
	// nobody is shown a Storage in it.
	StatusOpening  Status = "opening"
	StatusActive   Status = "active"
	StatusDeleting Status = "deleting"
	StatusDeleted  Status = "deleted"
)

// State is what a member sees a Storage as: the first thing that stops it,
// in the order the engine would refuse.
type State string

const (
	StateDeleting  State = "deleting"
	StateSuspended State = "suspended"
	StatePaused    State = "paused"
	StateFull      State = "full"
	StateActive    State = "active"
)

// Reason is why an operator Paused a Storage; the words for it are the
// reader's language.
type Reason string

const (
	ReasonAbuseComplaint Reason = "abuse_complaint"
	ReasonMalware        Reason = "malware"
	ReasonPhishing       Reason = "phishing"
	ReasonLegal          Reason = "legal"
)

func (r Reason) IsValid() bool {
	switch r {
	case ReasonAbuseComplaint, ReasonMalware, ReasonPhishing, ReasonLegal:
		return true
	default:
		return false
	}
}

// Package is the Package a Storage is on, its size and price copied when it
// was chosen like a Droplet's Locked price. StartedAt is zero until the
// Storage is open.
type Package struct {
	Code         string
	SizeBytes    int64
	MonthlyPrice decimal.Decimal
	StartedAt    time.Time
}

// Scheduled is a smaller Package the next Period starts with, its terms
// copied when it was scheduled. At is when it was scheduled: the change is due
// at the first Period boundary after it.
type Scheduled struct {
	Code         string
	SizeBytes    int64
	MonthlyPrice decimal.Decimal
	At           time.Time
}

// Due is the instant the scheduled change applies.
func (s *Scheduled) Due() time.Time {
	return NextPeriodStart(s.At)
}

// HoldFrom starts holding the scheduled Package at the Period's first instant.
func (s *Scheduled) HoldFrom(at time.Time) Package {
	return Package{Code: s.Code, SizeBytes: s.SizeBytes, MonthlyPrice: s.MonthlyPrice, StartedAt: at}
}

type Pause struct {
	Reason Reason
	At     time.Time
}

type Storage struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	TeamID     uuid.UUID
	Region     string
	BucketName string
	Status     Status
	Package    Package
	Scheduled  *Scheduled
	Pause      *Pause
	// TeamSuspended is the Team's Billing status as it concerns this Storage,
	// kept on the row so that what the engine must hold is read from one place.
	TeamSuspended bool

	UsedBytes            int64
	ObjectCount          int64
	MeasuredAt           *time.Time
	NearlyFullNotifiedAt *time.Time
	FullNotifiedAt       *time.Time

	// EngineSizeBytes and EngineStopped are what the engine was last made to
	// hold; they trail the fields above until the engine agrees.
	EngineSizeBytes  int64
	EngineStopped    bool
	TrafficReadUntil *time.Time

	CreatedBy uuid.UUID
	CreatedAt time.Time
	DeletedAt *time.Time
}

// Stopped is whether the Storage must refuse every read and write: Paused, or
// its Team suspended. One Storage can be both; it comes back when neither
// holds.
func (s *Storage) Stopped() bool {
	return s.Pause != nil || s.TeamSuspended
}

// InSync is whether the engine holds the size limit and the stop the Storage
// should have.
func (s *Storage) InSync() bool {
	return s.EngineSizeBytes == s.Package.SizeBytes && s.EngineStopped == s.Stopped()
}

func (s *Storage) NearlyFull() bool {
	return nearlyFull(s.UsedBytes, s.Package.SizeBytes)
}

func (s *Storage) Full() bool {
	return s.UsedBytes >= s.Package.SizeBytes
}

func (s *Storage) State() State {
	switch {
	case s.Status == StatusDeleting:
		return StateDeleting
	case s.TeamSuspended:
		return StateSuspended
	case s.Pause != nil:
		return StatePaused
	case s.Full():
		return StateFull
	default:
		return StateActive
	}
}

// Fits is whether what the Storage holds would fit a Package of sizeBytes.
func (s *Storage) Fits(sizeBytes int64) bool {
	return s.UsedBytes <= sizeBytes
}

// Measure records what the engine counted and answers which marks were just
// reached. A mark is armed again once the Storage is back under it, so a
// second filling is told again and one filling only once.
func (s *Storage) Measure(usedBytes, objects int64, at time.Time) (nearly, full bool) {
	s.UsedBytes, s.ObjectCount, s.MeasuredAt = usedBytes, objects, &at

	return s.remark(at)
}

// Hold puts the Storage on a Package and arms the marks the new size puts the
// Storage back under.
func (s *Storage) Hold(p Package) {
	s.Package = p
	s.Scheduled = nil
	if !s.NearlyFull() {
		s.NearlyFullNotifiedAt = nil
	}
	if !s.Full() {
		s.FullNotifiedAt = nil
	}
}

func (s *Storage) remark(at time.Time) (nearly, full bool) {
	switch {
	case !s.NearlyFull():
		s.NearlyFullNotifiedAt = nil
	case s.NearlyFullNotifiedAt == nil:
		s.NearlyFullNotifiedAt, nearly = &at, true
	}
	switch {
	case !s.Full():
		s.FullNotifiedAt = nil
	case s.FullNotifiedAt == nil:
		s.FullNotifiedAt, full = &at, true
	}

	// A Storage that jumps straight to full is told once, that it is full.
	return nearly && !full, full
}

func nearlyFull(usedBytes, sizeBytes int64) bool {
	return usedBytes*100 >= sizeBytes*NearlyFullPercent
}

type ChangeKind int

const (
	ChangeNothing ChangeKind = iota
	// ChangeUpgrade is a larger Package: at once.
	ChangeUpgrade
	// ChangeScheduled is a smaller Package: from the next Period, because
	// this Period's price is already owed.
	ChangeScheduled
	// ChangeUnscheduled is the current Package chosen again: the scheduled
	// change is undone.
	ChangeUnscheduled
)

// Decide is what choosing a Package of the code and size does to a Storage.
func Decide(current Package, scheduled *Scheduled, code string, sizeBytes int64) ChangeKind {
	switch {
	case code == current.Code && scheduled != nil:
		return ChangeUnscheduled
	case code == current.Code:
		return ChangeNothing
	case sizeBytes > current.SizeBytes:
		return ChangeUpgrade
	default:
		return ChangeScheduled
	}
}

// PeriodStart is the first instant of the calendar month in UTC that holds t.
func PeriodStart(t time.Time) time.Time {
	u := t.UTC()

	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func NextPeriodStart(t time.Time) time.Time {
	return PeriodStart(t).AddDate(0, 1, 0)
}

// Account is a Project's account in the engine with the root key the storage
// service acts with, its secret sealed.
type Account struct {
	ProjectID    uuid.UUID
	AccountID    string
	AccessKey    string
	SealedSecret []byte
}
