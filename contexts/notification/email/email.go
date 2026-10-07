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
	"github.com/piggystats/nodera/utils/str"
)

type Kind string

const (
	KindEmailVerificationCode  Kind = "email_verification_code"
	KindEmailAlreadyRegistered Kind = "email_already_registered"
	KindMFAEmailCode           Kind = "mfa_email_code"
	KindPasswordReset          Kind = "password_reset"
	KindTeamInvitation         Kind = "team_invitation"

	KindInvoiceIssued               Kind = "invoice_issued"
	KindPaymentSucceeded            Kind = "payment_succeeded"
	KindPaymentFailed               Kind = "payment_failed"
	KindPaymentConfirmationRequired Kind = "payment_confirmation_required"
	KindTeamSuspended               Kind = "team_suspended"
	KindDropletsDeletionWarning     Kind = "droplets_deletion_warning"
	KindCardExpiring                Kind = "card_expiring"

	// A Notification's email shares its type's name, which the dispatcher
	// sends as the Kind.
	KindDropletFailed Kind = "droplet_failed"

	KindMailDomainVerified   Kind = "mail_domain_verified"
	KindMailDomainUnverified Kind = "mail_domain_unverified"
	KindMailPackageNearLimit Kind = "mail_package_near_limit"
	KindMailPackageExhausted Kind = "mail_package_exhausted"
	KindMailRatesWarning     Kind = "mail_rates_warning"
	KindMailPaused           Kind = "mail_paused"

	KindStorageNearlyFull       Kind = "storage_nearly_full"
	KindStorageFull             Kind = "storage_full"
	KindStoragePaused           Kind = "storage_paused"
	KindStorageDowngradeSkipped Kind = "storage_downgrade_skipped"
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
	if str.IsBlank(to) {
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
	// inject markup into their own or anyone else's email. "t" lets a body
	// name an enum (a Role) in the reader's language from the same locale file
	// the subject comes from. "size" prints a byte count for the reader, so the
	// event can carry bytes and the web app and the email each word them.
	funcs := template.FuncMap{
		"t":    func(key string) string { return s.locale.Translate(lang, key, params) },
		"size": func(bytes int64) string { return humanSize(bytes, lang) },
	}
	tpl, err := template.New(string(kind)).Funcs(funcs).ParseFiles(files...)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := tpl.ExecuteTemplate(&out, string(kind), params); err != nil {
		return "", err
	}

	return out.String(), nil
}
