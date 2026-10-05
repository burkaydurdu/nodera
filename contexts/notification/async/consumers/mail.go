package consumers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/utils/str"
)

// mailPath opens the Mail App on the Project the Notification is about. It
// names the Team too: a reader may be in several, and the web app selects the
// one the link names, as it does for billing's links.
const mailPath = "/dashboard/mail?team=%s&project=%s"

// periodLayout is how mail names a Period: its calendar month in UTC.
const periodLayout = "2006-01"

// pauseReasons are the codes a Pause's email can word. A code this version does
// not know has no words, and a redelivery would not grow any.
var pauseReasons = map[string]bool{"bounce_rate": true, "complaint_rate": true}

// MailEvents makes a Notification of everything the mail context tells a Team
// about a Project's Mail. Params are what the web app words it from. Mail's
// events carry no link, so the one the email opens is built here from the web
// app's origin, and like every link it rides in the email params only.
type MailEvents struct {
	notifier INotifier
	frontURL string
}

func NewMailEvents(notifier INotifier, frontURL string) *MailEvents {
	return &MailEvents{notifier: notifier, frontURL: frontURL}
}

func (c *MailEvents) RoutingKeys() []string {
	return []string{
		constants.MailDomainVerified, constants.MailDomainUnverified, constants.MailPackageNearLimit,
		constants.MailPackageExhausted, constants.MailRatesWarning, constants.MailSendingPaused,
	}
}

func (c *MailEvents) Consume(message *rabbitmq.Message) error {
	event, err := c.mailEvent(message)
	if err != nil {
		return err
	}
	if event == nil {
		return nil
	}

	return c.notifier.NotifyTeam(context.Background(), *event)
}

// mailEvent answers nil for a routing key this consumer does not handle.
func (c *MailEvents) mailEvent(message *rabbitmq.Message) (*handlers.TeamEvent, error) {
	switch message.RoutingKey {
	case constants.MailDomainVerified:
		var e events.MailDomainVerifiedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload

		return c.domainEvent(p.Mail, notification.TypeMailDomainVerified, p.SendingDomainID, p.Domain, p.VerifiedAt)
	case constants.MailDomainUnverified:
		var e events.MailDomainUnverifiedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload

		return c.domainEvent(p.Mail, notification.TypeMailDomainUnverified, p.SendingDomainID, p.Domain, p.UnverifiedAt)
	case constants.MailPackageNearLimit:
		var e events.MailPackageNearLimitEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return c.usageEvent(notification.TypeMailPackageNearLimit, e.Payload)
	case constants.MailPackageExhausted:
		var e events.MailPackageExhaustedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return c.usageEvent(notification.TypeMailPackageExhausted, e.Payload)
	case constants.MailRatesWarning:
		var e events.MailRatesWarningEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		if err := requireMoment(p.At); err != nil {
			return nil, err
		}

		return c.projectEvent(p.Mail, notification.TypeMailRatesWarning, p.ProjectID.String()+":"+momentOf(p.At),
			map[string]any{"bounce_rate": string(p.BounceRate), "complaint_rate": string(p.ComplaintRate), "window_days": p.WindowDays},
			map[string]any{"BounceRate": p.BounceRate, "ComplaintRate": p.ComplaintRate, "WindowDays": p.WindowDays})
	case constants.MailSendingPaused:
		var e events.MailSendingPausedEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}
		p := e.Payload
		if !pauseReasons[p.Reason] {
			return nil, notification.ErrorMalformedEvent.Copy().SetMessage("unknown reason for a pause")
		}
		if err := requireMoment(p.PausedAt); err != nil {
			return nil, err
		}

		// The reason travels as a code: the web app and the email word it in
		// the reader's language.
		return c.projectEvent(p.Mail, notification.TypeMailPaused, p.ProjectID.String()+":"+momentOf(p.PausedAt),
			map[string]any{"reason": p.Reason, "bounce_rate": string(p.BounceRate), "complaint_rate": string(p.ComplaintRate)},
			map[string]any{"ReasonKey": "mail_pause_reason_" + p.Reason, "BounceRate": p.BounceRate, "ComplaintRate": p.ComplaintRate})
	default:
		return nil, nil
	}
}

// domainEvent names a Sending domain's change by the domain and the moment it
// changed: a domain can be lost and verified again, and every time is news.
func (c *MailEvents) domainEvent(m events.Mail, t notification.Type, domainID uuid.UUID, domain string, at time.Time) (*handlers.TeamEvent, error) {
	if domainID == uuid.Nil || str.IsBlank(domain) {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the sending domain")
	}
	if err := requireMoment(at); err != nil {
		return nil, err
	}

	return c.projectEvent(m, t, domainID.String()+":"+momentOf(at),
		map[string]any{"sending_domain_id": domainID.String(), "domain": domain},
		map[string]any{"Domain": domain})
}

// usageEvent names a Package milestone by the Period and the Package: each is
// reached once per Period, and a larger Package chosen within the Period is a
// new allowance to be told about.
func (c *MailEvents) usageEvent(t notification.Type, p events.MailPackageUsage) (*handlers.TeamEvent, error) {
	if str.IsBlank(p.PackageCode) {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the package")
	}
	start, err := time.Parse(periodLayout, p.Period)
	if err != nil {
		return nil, notification.ErrorMalformedEvent.Copy().SetError(err)
	}

	return c.projectEvent(p.Mail, t, p.ProjectID.String()+":"+p.Period+":"+p.PackageCode,
		map[string]any{"package_code": p.PackageCode, "allowance": p.Allowance, "used": p.Used, "period": p.Period},
		map[string]any{
			"PackageCode": p.PackageCode, "Allowance": p.Allowance, "Used": p.Used,
			// The allowance renews when the next Period starts.
			"RenewsAt": start.AddDate(0, 1, 0),
		})
}

// projectEvent names the occasion by its type and what it is about, so a
// redelivered event records and sends nothing twice.
func (c *MailEvents) projectEvent(m events.Mail, t notification.Type, occasion string, params, emailParams map[string]any) (*handlers.TeamEvent, error) {
	if m.TeamID == uuid.Nil || m.ProjectID == uuid.Nil {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the team or the project")
	}
	params["project_id"], params["project_name"] = m.ProjectID.String(), m.ProjectName
	emailParams["ProjectName"], emailParams["MailURL"] = m.ProjectName, c.frontURL+fmt.Sprintf(mailPath, m.TeamID, m.ProjectID)

	return &handlers.TeamEvent{
		SourceKey: string(t) + ":" + occasion, Type: t, TeamID: m.TeamID, Params: params, EmailParams: emailParams,
	}, nil
}

// requireMoment refuses an event without the moment it happened: the moment
// names the occasion, and without it a later one could not be told from a
// redelivery.
func requireMoment(at time.Time) error {
	if at.IsZero() {
		return notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing its time")
	}

	return nil
}

// momentOf prints a moment the same way on every redelivery.
func momentOf(at time.Time) string {
	return at.UTC().Format(time.RFC3339Nano)
}
