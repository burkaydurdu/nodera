package echo_middleware

import (
	"errors"
	"testing"

	"github.com/go-playground/validator/v10"
)

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

type fieldNameProbe struct {
	TeamID       string `json:"-" param:"teamID" validate:"required"`
	Region       string `json:"-" query:"region" validate:"required"`
	InvoiceEmail string `json:"invoice_email" validate:"required,email"`
	Untagged     string `validate:"required"`
}

// A validation error names the field the way the client sent it, so a form can
// put the message under the right input.
func TestValidationError_NamesFieldsAsTheClientSendsThem(t *testing.T) {
	err := NewCustomValidator().Validate(&fieldNameProbe{InvoiceEmail: "nope"})

	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected validation errors, got %v", err)
	}
	got := map[string]bool{}
	for _, e := range GetValidationError(verrs) {
		got[e.Field] = true
	}
	for _, want := range []string{"teamID", "region", "invoice_email", "Untagged"} {
		if !got[want] {
			t.Errorf("missing field %q in %v", want, got)
		}
	}
}
