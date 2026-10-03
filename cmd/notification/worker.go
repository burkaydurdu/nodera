package main

import (
	"context"

	"github.com/piggystats/rabbitmq"
	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/cmd/notification/app"
	"github.com/piggystats/nodera/cmd/notification/config"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	"github.com/piggystats/nodera/contexts/notification/async/producers"
	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/clients/mail"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
	"github.com/piggystats/nodera/utils/convert"
)

const prefetchCount = 32

// RunWorker consumes the email events and the domain events Notifications are
// made from. Consumers never retry: a failed event goes to the error queue
// and the shovel service decides when it comes back; the delivery state per
// Channel makes that redelivery harmless.
func RunWorker(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-notification-worker"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

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

	rabbitMQClient := newRabbitMQClient(ctx, conf, log)
	publisher := producers.NewRealtime(rabbitMQClient)
	publisher.AddPublisher(ctx)

	sources := app.BuildConsumers(app.Dependencies{Log: log, Conf: conf, Pool: pool, Email: sender, Realtime: publisher}).Sources()
	consumers.NewWorker(rabbitMQClient, log, sources...).Consume(ctx)
	cobra.CheckErr(rabbitMQClient.RunConsumers(ctx))
}

func newRabbitMQClient(ctx context.Context, conf *config.Config, log logger.ILog) rabbitmq.IClient {
	return rabbitmq.NewRabbitMqClient(
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
}
