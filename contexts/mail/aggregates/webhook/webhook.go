// Package webhook is the Webhook: an address on a Team's own server that
// receives a Project's Message events in signed batches, and the deliveries
// that carry them there.
package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/str"
)

const (
	MaxPerProject = 3
	BatchSize     = 100
	Timeout       = 10 * time.Second
	// GiveUpAfter bounds how long a failing delivery is retried.
	GiveUpAfter     = 24 * time.Hour
	secretBytes     = 32
	secretPrefix    = "whsec_"
	SignatureHeader = "Nodera-Signature"
	WebhookIDHeader = "Nodera-Webhook-Id"
	DeliveryHeader  = "Nodera-Delivery-Id"
)

// Schedule is the wait after each failed attempt; the last step repeats until
// GiveUpAfter.
var Schedule = []time.Duration{
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour,
}

type Webhook struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	URL       string
	Events    []string
	Active    bool
	// Secret is sealed; it signs every delivery.
	Secret    []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Delivery is one batch on its way to a Webhook; Body is frozen when the
// batch is made, so every retry sends and signs the same bytes.
type Delivery struct {
	ID             uuid.UUID
	WebhookID      uuid.UUID
	Status         Status
	Attempts       int
	EventCount     int
	Body           []byte
	NextAttemptAt  *time.Time
	LastAttemptAt  *time.Time
	LastStatusCode *int
	LastError      *string
	CreatedAt      time.Time
}

// DueDelivery is a Delivery to attempt now, with what its Webhook needs to sign and
// reach it.
type DueDelivery struct {
	Delivery *Delivery
	URL      string
	Secret   []byte
	Active   bool
}

// Sign is the Nodera-Signature value: an HMAC-SHA256 over the timestamp and
// the body, so a receiver can check both and refuse a replay.
func Sign(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)

	return "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// NextAttempt answers when to try again after attempts failures of a
// delivery first tried at first, and false once GiveUpAfter has passed.
func NextAttempt(first time.Time, attempts int, now time.Time) (time.Time, bool) {
	deadline := first.Add(GiveUpAfter)
	if !now.Before(deadline) {
		return time.Time{}, false
	}

	next := now.Add(Schedule[min(max(attempts, 1), len(Schedule))-1])
	if next.After(deadline) {
		next = deadline
	}

	return next, true
}

// NewSecret is a Webhook's signing secret, shown to the Team once.
func NewSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return secretPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidateURL refuses what a Webhook may not point at: anything but HTTPS,
// credentials in the URL, and a private address. allowLocal lifts the last
// two rules for development and test, where receivers run on loopback.
func ValidateURL(raw string, allowLocal bool) error {
	u, err := url.Parse(raw)
	if err != nil || str.IsEmpty(u.Host) || u.User != nil {
		return ErrorInvalidURL
	}

	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && allowLocal:
	default:
		return ErrorInvalidURL
	}

	if ip := net.ParseIP(u.Hostname()); ip != nil && !allowLocal && !PublicIP(ip) {
		return ErrorInvalidURL
	}

	return nil
}

// nonPublic is the space net.IP's predicates leave out that never holds a
// Team's server: shared, reserved, documentation and benchmarking ranges, and
// the IPv6 prefixes that carry an IPv4 address to a gateway, which could
// reach a private one through it.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// PublicIP is whether a delivery may connect to ip: never to loopback,
// private, link-local (cloud metadata lives there), multicast, unspecified
// or any other non-public space.
func PublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}

	return !slices.ContainsFunc(nonPublic, func(p netip.Prefix) bool { return p.Contains(addr) })
}
