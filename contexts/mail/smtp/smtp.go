// Package smtp is the SMTP relay: ready-made applications send with the user
// "apikey" and an API key as the password, through the same send path as the
// HTTP API.
package smtp

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/apikey"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
)

const (
	username       = "apikey"
	sessionTimeout = 2 * time.Minute
)

type Sender interface {
	SendSMTP(ctx context.Context, key *apikey.Key, envelopeRecipients []string, raw []byte) (*message.Message, error)
}

type KeyVerifier interface {
	Verify(ctx context.Context, raw string, permission authz.Permission) (*apikey.Key, error)
}

type Backend struct {
	log      logger.ILog
	sender   Sender
	verifier KeyVerifier
}

func NewBackend(log logger.ILog, sender Sender, verifier KeyVerifier) *Backend {
	return &Backend{log: log, sender: sender, verifier: verifier}
}

func (b *Backend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{backend: b}, nil
}

// Configure applies the relay's limits to a server: 50 recipients, the
// Message size, and no AUTH before TLS.
func Configure(s *gosmtp.Server, hostname string, maxMessageBytes int64) {
	s.Domain = hostname
	s.MaxRecipients = message.MaxRecipients
	s.MaxMessageBytes = maxMessageBytes
	s.ReadTimeout = sessionTimeout
	s.WriteTimeout = sessionTimeout
	s.AllowInsecureAuth = false
}

type session struct {
	backend    *Backend
	key        *apikey.Key
	recipients []string
}

func (s *session) AuthMechanisms() []string {
	return []string{sasl.Plain, sasl.Login}
}

func (s *session) Auth(mechanism string) (sasl.Server, error) {
	switch mechanism {
	case sasl.Plain:
		return sasl.NewPlainServer(func(_, user, password string) error { return s.authenticate(user, password) }), nil
	case sasl.Login:
		return newLoginServer(s.authenticate), nil
	default:
		return nil, gosmtp.ErrAuthUnknownMechanism
	}
}

// authenticate never says which half was wrong; an unreadable key store is a
// temporary failure so the client tries again with the same good key.
func (s *session) authenticate(user, password string) error {
	if !strings.EqualFold(user, username) {
		return gosmtp.ErrAuthFailed
	}

	key, err := s.backend.verifier.Verify(context.Background(), password, authz.MailSend)
	switch {
	case errors.Is(err, apikey.ErrorUnavailable):
		reply, _ := Reply(err)

		return reply
	case err != nil:
		return gosmtp.ErrAuthFailed
	}
	s.key = key

	return nil
}

// Mail takes the client's envelope sender and drops it: the envelope belongs
// to Nodera's return path.
func (s *session) Mail(string, *gosmtp.MailOptions) error {
	if s.key == nil {
		return gosmtp.ErrAuthRequired
	}

	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if s.key == nil {
		return gosmtp.ErrAuthRequired
	}
	if _, err := message.NormalizeAddress(to); err != nil {
		reply, _ := Reply(err)

		return reply
	}
	s.recipients = append(s.recipients, to)

	return nil
}

func (s *session) Data(r io.Reader) error {
	if s.key == nil {
		return gosmtp.ErrAuthRequired
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if _, err := s.backend.sender.SendSMTP(context.Background(), s.key, s.recipients, raw); err != nil {
		reply, refused := Reply(err)
		if !refused {
			s.backend.log.Error(err)
		}

		return reply
	}

	return nil
}

func (s *session) Reset() {
	s.recipients = nil
}

func (s *session) Logout() error {
	return nil
}
