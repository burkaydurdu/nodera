package mail

import (
	"context"

	"github.com/sendgrid/sendgrid-go"
	sendgridmail "github.com/sendgrid/sendgrid-go/helpers/mail"
)

type SendgridConfig struct {
	APIKey   string
	FromName string
}

type SendgridClient struct {
	client   *sendgrid.Client
	fromName string
}

func NewSendgridClient(cfg *SendgridConfig) IClient {
	return &SendgridClient{client: sendgrid.NewSendClient(cfg.APIKey), fromName: cfg.FromName}
}

func (s *SendgridClient) Send(_ context.Context, message *Message) error {
	from := sendgridmail.NewEmail(s.fromName, message.From)
	to := sendgridmail.NewEmail("", message.To[0])

	if _, err := s.client.Send(sendgridmail.NewSingleEmail(from, message.Subject, to, "", message.Body)); err != nil {
		return ErrFailedToSendEmail.Copy().SetError(err)
	}

	return nil
}
