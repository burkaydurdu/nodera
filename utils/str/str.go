package str

import "strings"

// IsEmpty reports whether s is unset. It is the check to reach for by default:
// it says what the comparison means, and it is the one place to widen "unset"
// if that ever changes.
func IsEmpty(s string) bool {
	return s == ""
}

func IsNotEmpty(s string) bool {
	return !IsEmpty(s)
}

// IsBlank reports whether s is unset or whitespace only.
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}
