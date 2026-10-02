package echo_middleware

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/country"
	"github.com/piggystats/nodera/utils/str"
)

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type CustomValidator struct {
	validator *validator.Validate
}

// Normalizable lets a request own its defaults; Normalize runs before the
// field rules so the `validate` tags see the defaulted values.
type Normalizable interface {
	Normalize()
}

func (cv *CustomValidator) Validate(i any) error {
	if n, ok := i.(Normalizable); ok {
		n.Normalize()
	}

	return cv.validator.Struct(i)
}

const (
	PasswordMinLength = 8
	PasswordMaxLength = 128
)

// IsValidPassword is the one password rule of the platform: 8–128 characters
// with at least one letter, one digit and one symbol. Sign-up, reset and
// change all go through it.
func IsValidPassword(password string) bool {
	if len(password) < PasswordMinLength || len(password) > PasswordMaxLength {
		return false
	}

	var hasLetter, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			hasSymbol = true
		}
	}

	return hasLetter && hasDigit && hasSymbol
}

var customValidations = map[string]validator.Func{
	"regex": func(fl validator.FieldLevel) bool {
		re, err := regexp.Compile(fl.Param())
		if err != nil {
			return false
		}

		return re.MatchString(fl.Field().String())
	},
	"password": func(fl validator.FieldLevel) bool {
		return IsValidPassword(fl.Field().String())
	},
	"country": func(fl validator.FieldLevel) bool {
		return country.IsValid(fl.Field().String())
	},
	"locale": func(fl validator.FieldLevel) bool {
		return fl.Field().String() == "tr" || fl.Field().String() == "en"
	},
	"role": func(fl validator.FieldLevel) bool {
		return authz.IsRole(fl.Field().String())
	},
}

func NewCustomValidator() *CustomValidator {
	v := validator.New()
	v.RegisterTagNameFunc(clientFieldName)
	for tag, fn := range customValidations {
		if err := v.RegisterValidation(tag, fn); err != nil {
			panic(err)
		}
	}

	return &CustomValidator{validator: v}
}

// clientFieldName reports a field under the name the client used for it: the
// JSON key for a body field, the path or query name for a bound parameter, so
// a form can show the message under the input it belongs to.
func clientFieldName(field reflect.StructField) string {
	for _, tag := range []string{"json", "param", "query"} {
		name, _, _ := strings.Cut(field.Tag.Get(tag), ",")
		if !str.IsEmpty(name) && name != "-" {
			return name
		}
	}

	return field.Name
}

func GetValidationError(validationErrors validator.ValidationErrors) []*ValidationError {
	errs := make([]*ValidationError, 0, len(validationErrors))
	for _, fieldError := range validationErrors {
		errs = append(errs, &ValidationError{
			Field:   fieldError.Field(),
			Message: fmt.Sprintf("Validation failed on the '%s' tag", fieldError.Tag()),
		})
	}

	return errs
}
