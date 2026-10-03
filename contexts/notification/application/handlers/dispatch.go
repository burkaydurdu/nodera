package handlers

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/dto"
	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/utils/str"
)

// The realtime message types the web app reacts to.
const (
	LiveNotificationCreated = "notification.created"
	LiveNotificationRead    = "notification.read"
)

//go:generate mockgen -destination=../../mock/handlers/email_sender.go -package=mockhandlers github.com/piggystats/nodera/contexts/notification/application/handlers IEmailSender
type IEmailSender interface {
	Send(ctx context.Context, kind email.Kind, to, recipientLocale string, params map[string]any) error
}

//go:generate mockgen -destination=../../mock/handlers/realtime_publisher.go -package=mockhandlers github.com/piggystats/nodera/contexts/notification/application/handlers IRealtimePublisher
type IRealtimePublisher interface {
	Publish(ctx context.Context, message *realtime.Message) error
}

// TeamEvent is something that happened to a Team's thing: SourceKey names it
// once for every Recipient, so a redelivered event records nothing new.
type TeamEvent struct {
	SourceKey string
	Type      notification.Type
	TeamID    uuid.UUID
	// Params are recorded with the Notification and are what the web app
	// words it from; never a secret.
	Params map[string]any
	// EmailParams are what the email template sees besides who it is for.
	// They may carry a link or a code, so they are never recorded: the event
	// carries them again on every redelivery.
	EmailParams map[string]any
	// Also are told by email although they are not members, such as a billing
	// profile's invoice address; an address a member already has is told once.
	Also []notification.Recipient
}

// PersonEvent is something one person is told about.
type PersonEvent struct {
	SourceKey string
	Type      notification.Type
	Recipient notification.Recipient
	// TeamID is the Team the Notification is about in the Inbox, which hides
	// it once the person leaves; nil keeps it visible whatever their Teams,
	// as for an invitation or a removal.
	TeamID      *uuid.UUID
	Params      map[string]any
	EmailParams map[string]any
}

// Dispatcher records Notifications and sends them out. Recording and sending
// are separate steps: the record commits first, then every Channel not yet
// delivered goes out and is marked, so a failure part-way returns an error,
// the event goes to the error queue, and its redelivery finishes the job
// without repeating what already went out.
type Dispatcher struct {
	log           logger.ILog
	notifications notification.IService
	access        access.IService
	transactions  postgresql.ITransactionManager
	email         IEmailSender
	realtime      IRealtimePublisher
	now           func() time.Time
}

func NewDispatcher(
	log logger.ILog,
	notifications notification.IService,
	accessService access.IService,
	transactions postgresql.ITransactionManager,
	emailSender IEmailSender,
	publisher IRealtimePublisher,
	now func() time.Time,
) *Dispatcher {
	return &Dispatcher{
		log: log, notifications: notifications, access: accessService, transactions: transactions,
		email: emailSender, realtime: publisher, now: now,
	}
}

// NotifyTeam tells every member whose Role may see what the event is about
// (docs/adr/0008), and the addresses in Also by email.
func (d *Dispatcher) NotifyTeam(ctx context.Context, event TeamEvent) error {
	definition, err := definitionOf(event.Type)
	if err != nil {
		return err
	}
	members, err := d.access.MembersOf(ctx, event.TeamID)
	if err != nil {
		return err
	}
	team, err := d.access.FindTeam(ctx, event.TeamID)
	if err != nil {
		return err
	}

	recipients := make([]notification.Recipient, 0, len(members)+len(event.Also))
	told := make(map[string]bool, len(members))
	for i := range members {
		member := members[i]
		if !member.Allows(definition.Permission) {
			continue
		}
		recipients = append(recipients, notification.Recipient{UserID: &member.UserID, Email: member.Email, FullName: member.FullName, Locale: member.Locale})
		told[addressKey(member.Email)] = true
	}
	for _, also := range event.Also {
		if str.IsBlank(also.Email) || told[addressKey(also.Email)] {
			continue
		}
		told[addressKey(also.Email)] = true
		recipients = append(recipients, notification.Recipient{Email: also.Email, FullName: also.FullName, Locale: also.Locale})
	}

	teamID := event.TeamID
	if err := d.record(ctx, definition, event.SourceKey, event.Type, &teamID, event.Params, recipients...); err != nil {
		return err
	}

	teamName := ""
	if team != nil {
		teamName = team.Name
	}

	return d.deliver(ctx, event.SourceKey, emailParams(event.EmailParams, teamName))
}

// NotifyPerson tells one person, on the Channels of the type they can be
// reached on.
func (d *Dispatcher) NotifyPerson(ctx context.Context, event PersonEvent) error {
	definition, err := definitionOf(event.Type)
	if err != nil {
		return err
	}
	if err := d.record(ctx, definition, event.SourceKey, event.Type, event.TeamID, event.Params, event.Recipient); err != nil {
		return err
	}

	return d.deliver(ctx, event.SourceKey, emailParams(event.EmailParams, ""))
}

func definitionOf(t notification.Type) (notification.Definition, error) {
	definition, ok := notification.DefinitionOf(t)
	if !ok {
		return definition, notification.ErrorUnknownType.Copy().SetMessage("unknown notification type " + string(t))
	}

	return definition, nil
}

// record writes one Notification per Recipient in one transaction.
func (d *Dispatcher) record(
	ctx context.Context, definition notification.Definition, sourceKey string, t notification.Type,
	teamID *uuid.UUID, params map[string]any, recipients ...notification.Recipient,
) error {
	txCtx, err := d.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		channels := channelsFor(definition, recipient)
		if len(channels) == 0 {
			continue
		}
		n := &notification.Notification{
			SourceKey: sourceKey,
			Type:      t,
			TeamID:    teamID,
			Recipient: recipient,
			Params:    params,
			InInbox:   slices.Contains(channels, notification.ChannelInApp),
		}
		if _, err := d.notifications.Record(txCtx, n, channels); err != nil {
			_ = d.transactions.Rollback(txCtx)

			return err
		}
	}

	return d.transactions.Commit(txCtx)
}

// channelsFor drops a Channel the Recipient cannot be reached on: a member
// whose email never reached this context still gets the rest, an address
// with no User behind it gets no Inbox.
func channelsFor(definition notification.Definition, recipient notification.Recipient) []notification.Channel {
	channels := make([]notification.Channel, 0, len(definition.Channels))
	for _, channel := range definition.Channels {
		switch {
		case channel == notification.ChannelEmail && str.IsBlank(recipient.Email):
		case channel == notification.ChannelInApp && recipient.UserID == nil:
		default:
			channels = append(channels, channel)
		}
	}

	return channels
}

func (d *Dispatcher) deliver(ctx context.Context, sourceKey string, shared map[string]any) error {
	pending, err := d.notifications.FindPending(ctx, sourceKey)
	if err != nil {
		return err
	}

	var firstErr error
	for _, p := range pending {
		for _, channel := range p.Channels {
			if err := d.send(ctx, p.Notification, channel, shared); err != nil {
				d.log.ErrorWithFields(err, logger.NewFields().Add("notification", p.Notification.ID.String()).Add("channel", string(channel)))
				if markErr := d.notifications.MarkFailed(ctx, p.Notification.ID, channel, err.Error()); markErr != nil {
					d.log.Error(markErr)
				}
				if firstErr == nil {
					firstErr = err
				}

				continue
			}
			if err := d.notifications.MarkDelivered(ctx, p.Notification.ID, channel, d.now()); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (d *Dispatcher) send(ctx context.Context, n *notification.Notification, channel notification.Channel, shared map[string]any) error {
	switch channel {
	case notification.ChannelInApp:
		message, err := realtime.New(realtime.ToUser(*n.Recipient.UserID), LiveNotificationCreated, dto.ToNotification(n), n.CreatedAt)
		if err != nil {
			return apperror.ErrorCommon.Copy().SetError(err)
		}

		return d.realtime.Publish(ctx, message)
	case notification.ChannelEmail:
		params := map[string]any{"FullName": n.Recipient.FullName, "Email": n.Recipient.Email}
		maps.Copy(params, shared)

		return d.email.Send(ctx, email.Kind(n.Type), n.Recipient.Email, n.Recipient.Locale, params)
	default:
		return notification.ErrorNoSender.Copy().SetMessage("no sender for channel " + string(channel))
	}
}

// emailParams is what every email of the event shares: the Team by name, when
// there is one, under what the event itself brings.
func emailParams(eventParams map[string]any, teamName string) map[string]any {
	params := map[string]any{}
	if str.IsNotEmpty(teamName) {
		params["TeamName"] = teamName
	}
	maps.Copy(params, eventParams)

	return params
}

func addressKey(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}
