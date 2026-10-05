// Package domain is the Sending domain: a domain a Project has proven through
// DNS it controls, and the two DKIM keys Nodera signs its mail with
// (docs/adr/0011).
package sendingdomain

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusVerified   Status = "verified"
	StatusUnverified Status = "unverified"
)

// Selector names one of a domain's two DKIM keys; a rotation replaces the
// idle one and later signs with it.
type Selector string

const (
	SelectorA Selector = "a"
	SelectorB Selector = "b"
)

func (s Selector) IsValid() bool {
	return s == SelectorA || s == SelectorB
}

func (s Selector) Other() Selector {
	if s == SelectorA {
		return SelectorB
	}

	return SelectorA
}

type Purpose string

const (
	PurposeReturnPath Purpose = "return_path"
	PurposeDKIM       Purpose = "dkim"
)

// Check cadence: a new domain is looked at often while its Team is setting
// DNS up, then daily like a verified one.
const (
	PendingInterval   = 10 * time.Minute
	PendingFastWindow = 72 * time.Hour
	DailyInterval     = 24 * time.Hour
	// VerifyCooldown bounds "verify now" so a click storm cannot turn Nodera
	// into a DNS load generator.
	VerifyCooldown = time.Minute
	// RotationEvery and RotationWait are the key rotation of ADR-0011: a fresh
	// key is published under the idle selector and signs only after resolvers
	// had time to drop a cached copy of the old record.
	RotationEvery = 180 * 24 * time.Hour
	RotationWait  = 48 * time.Hour
)

type SendingDomain struct {
	ID               uuid.UUID
	ProjectID        uuid.UUID
	TeamID           uuid.UUID
	Name             string
	Token            string
	Status           Status
	ActiveSelector   Selector
	ReturnPathValid  bool
	DKIMAValid       bool
	DKIMBValid       bool
	DMARCFound       bool
	LastCheckedAt    *time.Time
	NextCheckAt      time.Time
	VerifiedAt       *time.Time
	RotationSelector *Selector
	RotationReadyAt  *time.Time
	KeysRotatedAt    time.Time
	CreatedAt        time.Time
}

type SendingDomains []*SendingDomain

// Record is one CNAME the Team adds, with what the last check found.
type Record struct {
	Purpose   Purpose
	Type      string
	Host      string
	Value     string
	Valid     bool
	CheckedAt *time.Time
}

// ReturnPathDomain is the envelope sender's domain: bounces and feedback
// reports addressed to it reach Nodera through the Team's CNAME.
func (d *SendingDomain) ReturnPathDomain() string {
	return "n" + d.Token + "." + d.Name
}

// SelectorName is the selector a DKIM-Signature names; the token keeps it
// unique when two Projects share the domain.
func (d *SendingDomain) SelectorName(s Selector) string {
	return "n" + d.Token + string(s)
}

func (d *SendingDomain) SigningSelector() string {
	return d.SelectorName(d.ActiveSelector)
}

// DKIMHost is the name of a selector's CNAME in the Team's zone.
func (d *SendingDomain) DKIMHost(s Selector) string {
	return d.SelectorName(s) + "._domainkey." + d.Name
}

// KeyHost is where Nodera's zone publishes a selector's key.
func (d *SendingDomain) KeyHost(s Selector, zone string) string {
	return string(s) + "." + d.Token + ".dkim." + zone
}

// ReturnPathTarget is the one name every return path points at.
func ReturnPathTarget(zone string) string {
	return "rp." + zone
}

// Records are the three CNAMEs, in the order the Team is shown them.
func (d *SendingDomain) Records(zone string) []Record {
	return []Record{
		{Purpose: PurposeReturnPath, Type: "CNAME", Host: d.ReturnPathDomain(), Value: ReturnPathTarget(zone), Valid: d.ReturnPathValid, CheckedAt: d.LastCheckedAt},
		{Purpose: PurposeDKIM, Type: "CNAME", Host: d.DKIMHost(SelectorA), Value: d.KeyHost(SelectorA, zone), Valid: d.DKIMAValid, CheckedAt: d.LastCheckedAt},
		{Purpose: PurposeDKIM, Type: "CNAME", Host: d.DKIMHost(SelectorB), Value: d.KeyHost(SelectorB, zone), Valid: d.DKIMBValid, CheckedAt: d.LastCheckedAt},
	}
}

// CheckResult is what DNS answered for the three records and DMARC.
// Unanswered is a record DNS gave no answer about, a timeout or a SERVFAIL,
// rather than one it said is missing.
type CheckResult struct {
	ReturnPath bool
	DKIMA      bool
	DKIMB      bool
	DMARC      bool
	Unanswered bool
}

func (r CheckResult) AllValid() bool {
	return r.ReturnPath && r.DKIMA && r.DKIMB
}

// Change is what a check did to the domain's status, for the announcements.
type Change int

const (
	ChangeNone Change = iota
	ChangeVerified
	ChangeUnverified
)

// ApplyCheck records a check and moves the status: all three records verify
// a domain, a verified one missing any of them is unverified, and the records
// coming back verify it again. A check DNS did not answer changes nothing
// and is tried again soon, so a resolver's bad minute never unverifies a
// domain for a day.
func (d *SendingDomain) ApplyCheck(result CheckResult, now time.Time) Change {
	if result.Unanswered {
		d.NextCheckAt = now.Add(PendingInterval)

		return ChangeNone
	}

	d.ReturnPathValid = result.ReturnPath
	d.DKIMAValid = result.DKIMA
	d.DKIMBValid = result.DKIMB
	d.DMARCFound = result.DMARC
	checked := now
	d.LastCheckedAt = &checked

	change := ChangeNone
	switch {
	case result.AllValid() && d.Status != StatusVerified:
		d.Status = StatusVerified
		d.VerifiedAt = &checked
		change = ChangeVerified
	case !result.AllValid() && d.Status == StatusVerified:
		d.Status = StatusUnverified
		change = ChangeUnverified
	}

	d.NextCheckAt = d.nextCheck(now)

	return change
}

func (d *SendingDomain) nextCheck(now time.Time) time.Time {
	if d.Status == StatusPending && now.Before(d.CreatedAt.Add(PendingFastWindow)) {
		return now.Add(PendingInterval)
	}

	return now.Add(DailyInterval)
}

// CanVerifyNow is the "verify now" cooldown.
func (d *SendingDomain) CanVerifyNow(now time.Time) bool {
	return d.LastCheckedAt == nil || !now.Before(d.LastCheckedAt.Add(VerifyCooldown))
}

// Key is one selector's key pair; PrivateKey is sealed.
type Key struct {
	SendingDomainID uuid.UUID
	Selector        Selector
	PrivateKey      []byte
	PublicKey       string
}

// PublishedKey is what the DNS zone serves for one selector.
type PublishedKey struct {
	Token     string
	Selector  Selector
	PublicKey string
}
