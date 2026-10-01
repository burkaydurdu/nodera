package echo_middleware

import "testing"

// The password rule is a published contract of the sign-up, reset and change
// forms; the web app mirrors it in its live checklist.
func TestIsValidPassword(t *testing.T) {
	cases := map[string]bool{
		"Str0ng!pass":             true,
		"a1!aaaaa":                true,
		"short1!":                 false, // 7 characters
		"NoDigits!!":              false,
		"nosymbols123":            false,
		"12345678!":               false, // no letter
		"Türkçe1!":                true,
		string(make([]byte, 129)): false,
	}

	for password, want := range cases {
		t.Run(password, func(t *testing.T) {
			if got := IsValidPassword(password); got != want {
				t.Fatalf("IsValidPassword(%q) = %v, want %v", password, got, want)
			}
		})
	}
}
