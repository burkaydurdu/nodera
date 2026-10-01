package mail

import (
	"context"
	"crypto/tls"

	"gopkg.in/gomail.v2"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// InsecureSkipVerify is for a dev mail catcher with no certificate. Never
	// set it where real codes travel.
	InsecureSkipVerify bool
}

type SMTPClient struct {
	dialer *gomail.Dialer
}

func NewSMTPClient(cfg *SMTPConfig) IClient {
	dialer := gomail.NewDialer(cfg.Host, cfg.Port, cfg.Username, cfg.Password)
	if cfg.InsecureSkipVerify {
		dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	return &SMTPClient{dialer: dialer}
}

func (s *SMTPClient) Send(_ context.Context, message *Message) error {
	m := gomail.NewMessage()
	m.SetHeader("From", message.From)
	m.SetHeader("To", message.To...)
	m.SetHeader("Subject", message.Subject)
	m.SetBody("text/html", message.Body)

	if err := s.dialer.DialAndSend(m); err != nil {
		return ErrFailedToSendEmail.Copy().SetError(err)
	}

	return nil
}
