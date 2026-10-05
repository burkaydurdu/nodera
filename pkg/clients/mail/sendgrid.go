package mail

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sendgrid/sendgrid-go"
	sendgridmail "github.com/sendgrid/sendgrid-go/helpers/mail"
)

const sendgridSendEndpoint = "/v3/mail/send"

type SendgridConfig struct {
	APIKey   string
	FromName string
	// Host is SendGrid's API by default; a test points it at a stub.
	Host string
}

type SendgridClient struct {
	client   *sendgrid.Client
	fromName string
}

func NewSendgridClient(cfg *SendgridConfig) IClient {
	request := sendgrid.GetRequest(cfg.APIKey, sendgridSendEndpoint, cfg.Host)
	request.Method = http.MethodPost

	return &SendgridClient{client: &sendgrid.Client{Request: request}, fromName: cfg.FromName}
}

// Send fails on any answer but 2xx: sendgrid-go reports a rejection (a revoked
// key, an unverified sender) as a response, not an error, and taking it for a
// delivery would lose the email silently.
func (s *SendgridClient) Send(_ context.Context, message *Message) error {
	from := sendgridmail.NewEmail(s.fromName, message.From)
	to := sendgridmail.NewEmail("", message.To[0])

	response, err := s.client.Send(sendgridmail.NewSingleEmail(from, message.Subject, to, "", message.Body))
	if err != nil {
		return ErrFailedToSendEmail.Copy().SetError(err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ErrFailedToSendEmail.Copy().SetError(fmt.Errorf("sendgrid answered %d: %s", response.StatusCode, response.Body))
	}

	return nil
}
