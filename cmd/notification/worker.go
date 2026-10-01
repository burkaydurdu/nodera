package main

import (
	"context"

	"github.com/piggystats/rabbitmq"
	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/cmd/notification/config"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/clients/mail"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
	"github.com/piggystats/nodera/utils/convert"
)

const prefetchCount = 32

func RunWorker(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	var mailClient mail.IClient
	switch conf.Mail.Provider {
	case "sendgrid":
		mailClient = mail.NewSendgridClient(&mail.SendgridConfig{APIKey: conf.SendGrid.APIKey, FromName: conf.Mail.FromName})
	default:
		mailClient = mail.NewSMTPClient(&mail.SMTPConfig{
			Host: conf.SMTP.Host, Port: conf.SMTP.Port, Username: conf.SMTP.Username, Password: conf.SMTP.Password,
			InsecureSkipVerify: conf.SMTP.InsecureSkipVerify,
		})
	}

	sender := email.NewSender(email.Config{From: conf.Mail.From, TemplatesDir: conf.Mail.TemplatesDir}, mailClient)

	rabbitMQClient := rabbitmq.NewRabbitMqClient(
		ctx,
		[]string{conf.RabbitMQ.Host},
		conf.RabbitMQ.Username,
		conf.RabbitMQ.Password,
		conf.RabbitMQ.VirtualHost,
		log,
		rabbitmq.RetryCount(0),
		rabbitmq.PrefetchCount(prefetchCount),
		rabbitmq.WithMessageAuthentication(convert.StringToBytes(conf.RabbitMQ.AuthSecret), rabbitmq.AuthModeStrict),
	)

	consumers.NewWorker(rabbitMQClient, log, sender).Consume(ctx)
	cobra.CheckErr(rabbitMQClient.RunConsumers(ctx))
}
