package storage

import "strings"

const (
	minBucketName = 3
	maxBucketName = 63
)

// reservedNames could pass for Nodera's own addresses under the Storage
// domain, or for a service a visitor would trust.
var reservedNames = map[string]struct{}{
	"admin": {}, "api": {}, "app": {}, "auth": {}, "abuse": {}, "assets": {}, "billing": {}, "cdn": {}, "console": {},
	"dashboard": {}, "ftp": {}, "login": {}, "mail": {}, "ns1": {}, "ns2": {}, "postmaster": {}, "root": {}, "s3": {},
	"security": {}, "smtp": {}, "static": {}, "status": {}, "storage": {}, "support": {}, "www": {},
}

// reservedPrefixes are Nodera's own name and the prefix of an encoded
// international name, which would show as something else in a browser.
var reservedPrefixes = []string{"nodera", "xn--"}

// ValidateBucketName holds a name to what one DNS label under the wildcard
// certificate can carry: 3 to 63 lower-case letters, digits and hyphens,
// starting and ending with a letter or a digit, and no dots.
func ValidateBucketName(name string) error {
	if len(name) < minBucketName || len(name) > maxBucketName {
		return ErrorInvalidBucketName
	}
	for i := range len(name) {
		c := name[i]
		alphanumeric := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		edge := i == 0 || i == len(name)-1
		if !alphanumeric && (c != '-' || edge) {
			return ErrorInvalidBucketName
		}
	}

	if _, reserved := reservedNames[name]; reserved {
		return ErrorReservedBucketName
	}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return ErrorReservedBucketName
		}
	}

	return nil
}
