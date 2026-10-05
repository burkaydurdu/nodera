package consumers_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	mockconsumers "github.com/piggystats/nodera/contexts/notification/mock/consumers"
)

const frontURL = "http://localhost:3002"

var mailAt = time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)

func mailPayload(teamID, projectID uuid.UUID, fields map[string]any) map[string]any {
	payload := map[string]any{"team_id": teamID, "project_id": projectID, "project_name": "Shop"}
	for k, v := range fields {
		payload[k] = v
	}

	return payload
}

func mailLink(teamID, projectID uuid.UUID) string {
	return frontURL + "/dashboard/mail?team=" + teamID.String() + "&project=" + projectID.String()
}

func TestMailDomainVerified_IsNamedByTheDomainAndTheMomentItWasVerified(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID, domainID := uuid.New(), uuid.New(), uuid.New()

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailDomainVerified, mailPayload(teamID, projectID, map[string]any{
		"sending_domain_id": domainID, "domain": "mail.shop.example", "verified_at": mailAt,
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeMailDomainVerified, event.Type)
	assert.Equal(t, "mail_domain_verified:"+domainID.String()+":2026-10-05T09:30:00Z", event.SourceKey)
	assert.Equal(t, teamID, event.TeamID)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "sending_domain_id": domainID.String(), "domain": "mail.shop.example",
	}, event.Params)
	assert.Equal(t, "Shop", event.EmailParams["ProjectName"])
	assert.Equal(t, "mail.shop.example", event.EmailParams["Domain"])
	assert.Equal(t, mailLink(teamID, projectID), event.EmailParams["MailURL"], "the button opens the Project's Mail in its Team")
	assert.Empty(t, event.Also, "mail names nobody outside the Team")
}

func TestMailDomainUnverified_IsNamedByTheMomentItWasLost(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID, domainID := uuid.New(), uuid.New(), uuid.New()

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailDomainUnverified, mailPayload(teamID, projectID, map[string]any{
		"sending_domain_id": domainID, "domain": "mail.shop.example", "unverified_at": mailAt,
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeMailDomainUnverified, event.Type)
	assert.Equal(t, "mail_domain_unverified:"+domainID.String()+":2026-10-05T09:30:00Z", event.SourceKey)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "sending_domain_id": domainID.String(), "domain": "mail.shop.example",
	}, event.Params)
	assert.Equal(t, mailLink(teamID, projectID), event.EmailParams["MailURL"])
}

func TestMailPackageNearLimit_IsNamedByItsPeriodAndPackage(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID := uuid.New(), uuid.New()

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailPackageNearLimit, mailPayload(teamID, projectID, map[string]any{
		"package_code": "10k", "allowance": 10000, "used": 8000, "period": "2026-10", "at": mailAt,
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeMailPackageNearLimit, event.Type)
	assert.Equal(t, "mail_package_near_limit:"+projectID.String()+":2026-10:10k", event.SourceKey)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "package_code": "10k", "allowance": 10000, "used": 8000, "period": "2026-10",
	}, event.Params)
	assert.Equal(t, 8000, event.EmailParams["Used"])
	assert.Equal(t, 10000, event.EmailParams["Allowance"])
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), event.EmailParams["RenewsAt"], "the allowance renews with the next Period")
	assert.Equal(t, mailLink(teamID, projectID), event.EmailParams["MailURL"])
}

func TestMailPackageExhausted_RenewsWhenTheNextPeriodStarts(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	projectID := uuid.New()

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailPackageExhausted, mailPayload(uuid.New(), projectID, map[string]any{
		"package_code": "25k", "allowance": 25000, "used": 25000, "period": "2026-12", "at": mailAt,
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeMailPackageExhausted, event.Type)
	assert.Equal(t, "mail_package_exhausted:"+projectID.String()+":2026-12:25k", event.SourceKey)
	assert.Equal(t, "25k", event.Params["package_code"])
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), event.EmailParams["RenewsAt"])
}

func TestMailRatesWarning_KeepsTheRatesAsMailRoundedThem(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID := uuid.New(), uuid.New()

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailRatesWarning, mailPayload(teamID, projectID, map[string]any{
		"bounce_rate": "2.40", "complaint_rate": "0.00", "window_days": 7, "at": mailAt,
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeMailRatesWarning, event.Type)
	assert.Equal(t, "mail_rates_warning:"+projectID.String()+":2026-10-05T09:30:00Z", event.SourceKey)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "bounce_rate": "2.40", "complaint_rate": "0.00", "window_days": 7,
	}, event.Params)
	assert.EqualValues(t, "2.40", event.EmailParams["BounceRate"])
	assert.EqualValues(t, "0.00", event.EmailParams["ComplaintRate"])
	assert.Equal(t, 7, event.EmailParams["WindowDays"])
	assert.Equal(t, mailLink(teamID, projectID), event.EmailParams["MailURL"])
}

func TestMailSendingPaused_CarriesTheReasonAsACodeAndWordsItInTheEmail(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))
	event := toldTeam(t, notifier)
	teamID, projectID := uuid.New(), uuid.New()

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailSendingPaused, mailPayload(teamID, projectID, map[string]any{
		"reason": "complaint_rate", "bounce_rate": "1.10", "complaint_rate": "0.12", "paused_at": mailAt,
	})))

	require.NoError(t, err)
	assert.Equal(t, notification.TypeMailPaused, event.Type)
	assert.Equal(t, "mail_paused:"+projectID.String()+":2026-10-05T09:30:00Z", event.SourceKey)
	assert.Equal(t, map[string]any{
		"project_id": projectID.String(), "project_name": "Shop", "reason": "complaint_rate", "bounce_rate": "1.10", "complaint_rate": "0.12",
	}, event.Params, "the web app words the reason from its code; no English sentence is recorded")
	assert.Equal(t, "mail_pause_reason_complaint_rate", event.EmailParams["ReasonKey"])
	assert.EqualValues(t, "1.10", event.EmailParams["BounceRate"])
	assert.EqualValues(t, "0.12", event.EmailParams["ComplaintRate"])
}

func TestMailSendingPaused_WithAReasonThisVersionCannotWordIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailSendingPaused, mailPayload(uuid.New(), uuid.New(), map[string]any{
		"reason": "spam_traps", "bounce_rate": "1.10", "complaint_rate": "0.12", "paused_at": mailAt,
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

// The moment names the occasion: without it a later Pause could not be told
// from a redelivery of this one.
func TestMailSendingPaused_WithoutItsTimeIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailSendingPaused, mailPayload(uuid.New(), uuid.New(), map[string]any{
		"reason": "bounce_rate", "bounce_rate": "5.40", "complaint_rate": "0.02",
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestMailEvent_WithoutItsTeamIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailDomainVerified, mailPayload(uuid.Nil, uuid.New(), map[string]any{
		"sending_domain_id": uuid.New(), "domain": "mail.shop.example", "verified_at": mailAt,
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestMailEvent_WithoutItsProjectIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailRatesWarning, mailPayload(uuid.New(), uuid.Nil, map[string]any{
		"bounce_rate": "2.40", "complaint_rate": "0.00", "window_days": 7, "at": mailAt,
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestMailDomainEvent_WithoutItsSendingDomainIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailDomainUnverified, mailPayload(uuid.New(), uuid.New(), map[string]any{
		"domain": "mail.shop.example", "unverified_at": mailAt,
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestMailPackageEvent_WithAPeriodThatIsNoMonthIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailPackageNearLimit, mailPayload(uuid.New(), uuid.New(), map[string]any{
		"package_code": "10k", "allowance": 10000, "used": 8000, "period": "October", "at": mailAt,
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestMailRatesWarning_WithARateThatIsNoNumberIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(message(t, constants.MailRatesWarning, mailPayload(uuid.New(), uuid.New(), map[string]any{
		"bounce_rate": "high", "complaint_rate": "0.00", "window_days": 7, "at": mailAt,
	})))

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}

func TestMailEvent_AMalformedPayloadIsRefused(t *testing.T) {
	notifier := mockconsumers.NewMockINotifier(gomock.NewController(t))

	err := consumers.NewMailEvents(notifier, frontURL).Consume(&rabbitmq.Message{RoutingKey: constants.MailPackageExhausted, Payload: []byte("{")})

	require.ErrorIs(t, err, notification.ErrorMalformedEvent)
}
