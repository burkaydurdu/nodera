package sendingdomain

import (
	"net"
	"strings"

	"golang.org/x/net/idna"

	"github.com/piggystats/nodera/utils/str"
)

const (
	maxNameLength  = 253
	maxLabelLength = 63
	minLabels      = 2
)

// NormalizeName turns what a Team typed into the form records and DKIM use:
// lower case, IDNA in ASCII, no trailing dot. It refuses what cannot be a
// Sending domain: an IP, a single label, a malformed label, and any name in
// Nodera's own zone, whose records belong to Nodera.
func NormalizeName(raw, zone string) (string, error) {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if str.IsEmpty(name) || strings.HasPrefix(name, "[") || net.ParseIP(name) != nil {
		return "", ErrorInvalidName
	}

	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil {
		return "", ErrorInvalidName
	}
	if len(ascii) > maxNameLength {
		return "", ErrorInvalidName
	}

	labels := strings.Split(ascii, ".")
	if len(labels) < minLabels {
		return "", ErrorInvalidName
	}
	for _, label := range labels {
		if !validLabel(label) {
			return "", ErrorInvalidName
		}
	}

	zone = strings.ToLower(strings.TrimSuffix(zone, "."))
	if ascii == zone || strings.HasSuffix(ascii, "."+zone) {
		return "", ErrorInvalidName
	}

	return ascii, nil
}

func validLabel(label string) bool {
	if len(label) == 0 || len(label) > maxLabelLength {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return true
}
