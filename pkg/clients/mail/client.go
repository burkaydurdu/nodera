package mail

import "context"

type Message struct {
	From    string
	To      []string
	Subject string
	Body    string
}

//go:generate mockgen -destination=../../../mock/clients/mail/client.go -package=mockmail github.com/piggystats/nodera/pkg/clients/mail IClient
type IClient interface {
	Send(ctx context.Context, message *Message) error
}
