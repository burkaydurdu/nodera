// Package locale renders the short strings (subjects) of the emails in the
// recipient's language.
package locale

import (
	"path/filepath"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	Default = "en"
)

var supported = []string{"en", "tr"}

// Resolve answers a supported locale for any input, falling back to English.
func Resolve(locale string) string {
	for _, s := range supported {
		if s == locale {
			return s
		}
	}

	return Default
}

type ILocale interface {
	Translate(locale, key string, params map[string]any) string
}

type Locale struct {
	bundle *i18n.Bundle
}

// New loads <dir>/<locale>.json for every supported locale.
func New(dir string) ILocale {
	bundle := i18n.NewBundle(language.English)
	for _, l := range supported {
		bundle.MustLoadMessageFile(filepath.Join(dir, l+".json"))
	}

	return &Locale{bundle: bundle}
}

func (l *Locale) Translate(locale, key string, params map[string]any) string {
	return i18n.NewLocalizer(l.bundle, Resolve(locale)).MustLocalize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: params,
	})
}
