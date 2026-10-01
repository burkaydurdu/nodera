// Package email renders and sends the platform's emails. One Kind per email;
// the template and subject key share the Kind's name.
package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"path/filepath"

	"github.com/piggystats/nodera/pkg/clients/mail"
	"github.com/piggystats/nodera/pkg/locale"
)

type Kind string

const (
	KindEmailVerificationCode  Kind = "email_verification_code"
	KindEmailAlreadyRegistered Kind = "email_already_registered"
	KindMFAEmailCode           Kind = "mfa_email_code"
	KindPasswordReset          Kind = "password_reset"
)

// shared templates carry no copy; per-locale ones do.
var shared = []string{"common/head.html", "common/navbar.html"}

var perLocale = []string{"%s/footer.html"}

type Config struct {
	From string
	// TemplatesDir is the notification templates root, holding email/ and
	// locales/.
	TemplatesDir string
}

// ErrorInvalidRecipient is terminal: the same payload would fail again.
var ErrorInvalidRecipient = mail.ErrFailedToBuildMessage.Copy().SetMessage("recipient email is empty")

type Sender struct {
	config Config
	client mail.IClient
	locale locale.ILocale
}

func NewSender(config Config, client mail.IClient) *Sender {
	return &Sender{
		config: config,
		client: client,
		locale: locale.New(filepath.Join(config.TemplatesDir, "locales")),
	}
}

// Send renders kind for the recipient's locale and delivers it. params is
// what the template sees; the subject comes from the locale file under
// "<kind>_subject".
func (s *Sender) Send(ctx context.Context, kind Kind, to, recipientLocale string, params map[string]any) error {
	if to == "" {
		return ErrorInvalidRecipient
	}

	lang := locale.Resolve(recipientLocale)
	body, err := s.render(kind, lang, params)
	if err != nil {
		return mail.ErrFailedToBuildMessage.Copy().SetError(err)
	}

	return s.client.Send(ctx, &mail.Message{
		From:    s.config.From,
		To:      []string{to},
		Subject: s.locale.Translate(lang, string(kind)+"_subject", params),
		Body:    body,
	})
}

func (s *Sender) render(kind Kind, lang string, params map[string]any) (string, error) {
	files := make([]string, 0, len(shared)+len(perLocale)+1)
	for _, f := range shared {
		files = append(files, filepath.Join(s.config.TemplatesDir, "email", f))
	}
	for _, f := range perLocale {
		files = append(files, filepath.Join(s.config.TemplatesDir, "email", fmt.Sprintf(f, lang)))
	}
	files = append(files, filepath.Join(s.config.TemplatesDir, "email", lang, string(kind)+".html"))

	// html/template context-escapes the params, so a person's name cannot
	// inject markup into their own or anyone else's email.
	tpl, err := template.New(string(kind)).ParseFiles(files...)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := tpl.ExecuteTemplate(&out, string(kind), params); err != nil {
		return "", err
	}

	return out.String(), nil
}
