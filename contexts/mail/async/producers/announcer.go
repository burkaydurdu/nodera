package producers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/async/constants"
	"github.com/piggystats/nodera/contexts/mail/async/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

// Announcer publishes everything this context tells others. Timestamps go out
// in UTC so every consumer reads the same instant whatever zone the database
// handed back.
type Announcer struct {
	client OutboxClient
}

func NewAnnouncer(client OutboxClient) *Announcer {
	return &Announcer{client: client}
}

func (a *Announcer) PackageStarted(ctx context.Context, project *access.ProjectAccess, h *packages.HeldPackage) error {
	return publish(ctx, a.client, constants.MailPackageStarted, &events.MailPackageStartedEvent{Payload: events.MailPackageStartedBody{
		ProjectID:     project.ProjectID,
		TeamID:        project.TeamID,
		ProjectName:   project.Name,
		PackageCode:   h.Code,
		MonthlyEmails: h.MonthlyEmails,
		MonthlyPrice:  h.MonthlyPrice.StringFixed(packages.PriceScale),
		StartedAt:     h.StartedAt.UTC(),
	}})
}

func (a *Announcer) PackageStopped(ctx context.Context, teamID, projectID uuid.UUID, startedAt, stoppedAt time.Time) error {
	return publish(ctx, a.client, constants.MailPackageStopped, &events.MailPackageStoppedEvent{Payload: events.MailPackageStoppedBody{
		ProjectID: projectID, TeamID: teamID, StartedAt: startedAt.UTC(), StoppedAt: stoppedAt.UTC(),
	}})
}

func (a *Announcer) DomainVerified(ctx context.Context, project *access.ProjectAccess, d *sendingdomain.SendingDomain, at time.Time) error {
	return publish(ctx, a.client, constants.MailDomainVerified, &events.MailDomainVerifiedEvent{Payload: events.MailDomainVerifiedBody{
		TeamID: project.TeamID, ProjectID: project.ProjectID, ProjectName: project.Name, SendingDomainID: d.ID, Domain: d.Name, VerifiedAt: at.UTC(),
	}})
}

func (a *Announcer) DomainUnverified(ctx context.Context, project *access.ProjectAccess, d *sendingdomain.SendingDomain, at time.Time) error {
	return publish(ctx, a.client, constants.MailDomainUnverified, &events.MailDomainUnverifiedEvent{Payload: events.MailDomainUnverifiedBody{
		TeamID: project.TeamID, ProjectID: project.ProjectID, ProjectName: project.Name, SendingDomainID: d.ID, Domain: d.Name, UnverifiedAt: at.UTC(),
	}})
}

func (a *Announcer) PackageNearLimit(ctx context.Context, project *access.ProjectAccess, code string, period *packages.Period, at time.Time) error {
	return publish(ctx, a.client, constants.MailPackageNearLimit, &events.MailPackageNearLimitEvent{Payload: usage(project, code, period, at)})
}

func (a *Announcer) PackageExhausted(ctx context.Context, project *access.ProjectAccess, code string, period *packages.Period, at time.Time) error {
	return publish(ctx, a.client, constants.MailPackageExhausted, &events.MailPackageExhaustedEvent{Payload: usage(project, code, period, at)})
}

func (a *Announcer) RatesWarning(ctx context.Context, project *access.ProjectAccess, w limits.Window, at time.Time) error {
	return publish(ctx, a.client, constants.MailRatesWarning, &events.MailRatesWarningEvent{Payload: events.MailRatesWarningBody{
		TeamID: project.TeamID, ProjectID: project.ProjectID, ProjectName: project.Name,
		BounceRate: w.BounceRate(), ComplaintRate: w.ComplaintRate(), WindowDays: limits.WindowDays, At: at.UTC(),
	}})
}

func (a *Announcer) SendingPaused(ctx context.Context, project *access.ProjectAccess, reason limits.Reason, w limits.Window, at time.Time) error {
	return publish(ctx, a.client, constants.MailSendingPaused, &events.MailSendingPausedEvent{Payload: events.MailSendingPausedBody{
		TeamID: project.TeamID, ProjectID: project.ProjectID, ProjectName: project.Name, Reason: string(reason),
		BounceRate: w.BounceRate(), ComplaintRate: w.ComplaintRate(), PausedAt: at.UTC(),
	}})
}

// liveSendingDomain is what the web app refetches a Sending domain by.
type liveSendingDomain struct {
	ProjectID       uuid.UUID            `json:"project_id"`
	SendingDomainID uuid.UUID            `json:"sending_domain_id"`
	Status          sendingdomain.Status `json:"status"`
}

// liveSending is what the web app refetches a Project's Mail overview by.
type liveSending struct {
	ProjectID uuid.UUID `json:"project_id"`
}

// DomainChanged and SendingChanged reach the open web apps of the Team's
// members who may see Mail (docs/adr/0007); the web app refetches.
func (a *Announcer) DomainChanged(ctx context.Context, teamID, projectID, domainID uuid.UUID, status sendingdomain.Status, at time.Time) error {
	return a.live(ctx, teamID, constants.RealtimeDomainChanged, liveSendingDomain{ProjectID: projectID, SendingDomainID: domainID, Status: status}, at)
}

func (a *Announcer) SendingChanged(ctx context.Context, teamID, projectID uuid.UUID, at time.Time) error {
	return a.live(ctx, teamID, constants.RealtimeSendingChanged, liveSending{ProjectID: projectID}, at)
}

func (a *Announcer) live(ctx context.Context, teamID uuid.UUID, messageType string, data any, at time.Time) error {
	message, err := realtime.New(realtime.ToTeam(teamID, authz.MailRead), messageType, data, at)
	if err != nil {
		return err
	}

	return publish(ctx, a.client, realtime.RoutingKey, message)
}

func usage(project *access.ProjectAccess, code string, period *packages.Period, at time.Time) events.MailPackageUsageBody {
	return events.MailPackageUsageBody{
		TeamID: project.TeamID, ProjectID: project.ProjectID, ProjectName: project.Name, PackageCode: code,
		Allowance: period.Allowance, Used: period.Used, Period: packages.PeriodLabel(period.Start), At: at.UTC(),
	}
}
