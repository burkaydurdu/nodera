package integrationtest

import (
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/email"
)

// mailProject is a Project of a Team as the mail context names it.
type mailProject struct {
	ID     uuid.UUID
	TeamID uuid.UUID
	Name   string
}

// mailCase is one of mail's events and the Notification it makes, its params
// as the Inbox shows them.
type mailCase struct {
	routingKey string
	kind       email.Kind
	fields     map[string]any
	params     map[string]any
}

func mailCases(p mailProject) []mailCase {
	domainID := uuid.New()
	at := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	project := func(more map[string]any) map[string]any {
		out := map[string]any{"project_id": p.ID.String(), "project_name": p.Name}
		for k, v := range more {
			out[k] = v
		}

		return out
	}

	return []mailCase{
		{
			constants.MailDomainVerified, email.KindMailDomainVerified,
			map[string]any{"sending_domain_id": domainID, "domain": "mail.shop.example", "verified_at": at},
			project(map[string]any{"sending_domain_id": domainID.String(), "domain": "mail.shop.example"}),
		},
		{
			constants.MailDomainUnverified, email.KindMailDomainUnverified,
			map[string]any{"sending_domain_id": domainID, "domain": "mail.shop.example", "unverified_at": at.Add(24 * time.Hour)},
			project(map[string]any{"sending_domain_id": domainID.String(), "domain": "mail.shop.example"}),
		},
		{
			constants.MailPackageNearLimit, email.KindMailPackageNearLimit,
			map[string]any{"package_code": "10k", "allowance": 10000, "used": 8000, "period": "2026-10", "at": at},
			project(map[string]any{"package_code": "10k", "allowance": float64(10000), "used": float64(8000), "period": "2026-10"}),
		},
		{
			constants.MailPackageExhausted, email.KindMailPackageExhausted,
			map[string]any{"package_code": "10k", "allowance": 10000, "used": 10000, "period": "2026-10", "at": at},
			project(map[string]any{"package_code": "10k", "allowance": float64(10000), "used": float64(10000), "period": "2026-10"}),
		},
		{
			constants.MailRatesWarning, email.KindMailRatesWarning,
			map[string]any{"bounce_rate": "2.40", "complaint_rate": "0.06", "window_days": 7, "at": at},
			project(map[string]any{"bounce_rate": "2.40", "complaint_rate": "0.06", "window_days": float64(7)}),
		},
		{
			constants.MailSendingPaused, email.KindMailPaused,
			map[string]any{"reason": "complaint_rate", "bounce_rate": "1.10", "complaint_rate": "0.12", "paused_at": at},
			project(map[string]any{"reason": "complaint_rate", "bounce_rate": "1.10", "complaint_rate": "0.12"}),
		},
	}
}

// mailMessage is the mail context publishing one of its events about the
// Project, through its outbox.
func (s *notificationSuite) mailMessage(routingKey string, p mailProject, fields map[string]any) *rabbitmq.Message {
	payload := map[string]any{"team_id": p.TeamID, "project_id": p.ID, "project_name": p.Name}
	for k, v := range fields {
		payload[k] = v
	}

	return &rabbitmq.Message{
		RoutingKey: routingKey, Payload: []byte(s.json(map[string]any{"payload": payload})),
		MessageID: uuid.NewString(), CorrelationID: uuid.NewString(),
	}
}

func (s *notificationSuite) TestMailEvents_ReachEveryMailReaderOfTheTeamAndNobodyElse() {
	ayse, mehmet, zeynep := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr"), s.signIn("Zeynep", "en")
	former, stranger := s.signIn("Deniz", "tr"), s.signIn("Can", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")
	s.addMember(teamID, zeynep, "finance")
	s.addMember(teamID, former, "developer")
	s.removeMember(teamID, former)
	s.team("Other", stranger)
	shop := mailProject{ID: uuid.New(), TeamID: teamID, Name: "Shop"}
	cases := mailCases(shop)

	for _, c := range cases {
		s.Require().NoError(s.deliver(s.mailMessage(c.routingKey, shop, c.fields)), c.routingKey)
	}

	for _, member := range []user{ayse, mehmet, zeynep} {
		items := s.inbox(member)
		s.Require().Len(items, len(cases), member.FullName)
		params := map[string]any{}
		for _, item := range items {
			s.Equal(teamID.String(), item["team_id"])
			params[item["type"].(string)] = item["params"]
		}
		for _, c := range cases {
			s.Equal(c.params, params[string(c.kind)], "%s for %s: ids, names, numbers and codes, never a sentence", c.kind, member.FullName)
		}
	}
	for _, c := range cases {
		s.Equal(3, s.recorded(string(c.kind)), "%s: the owner, the developer and finance, each holding mail.read", c.kind)
	}
	for _, outsider := range []user{former, stranger} {
		s.Empty(s.emailsTo(outsider), outsider.FullName)
		s.Empty(s.pushedTo(outsider, "notification.created"), outsider.FullName)
		s.Zero(s.recordedFor(outsider), "%s is outside the Team", outsider.FullName)
	}
}

func (s *notificationSuite) TestMailEvents_GoOutInTheAppAndByEmailOnceEachInTheReadersLanguage() {
	ayse, zeynep := s.signIn("Ayse", "tr"), s.signIn("Zeynep", "en")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, zeynep, "finance")
	shop := mailProject{ID: uuid.New(), TeamID: teamID, Name: "Shop"}
	cases := mailCases(shop)
	kinds := make([]email.Kind, 0, len(cases))

	for _, c := range cases {
		s.Require().NoError(s.deliver(s.mailMessage(c.routingKey, shop, c.fields)), c.routingKey)
		kinds = append(kinds, c.kind)
	}

	for _, member := range []user{ayse, zeynep} {
		s.Len(s.pushedTo(member, "notification.created"), len(cases), member.FullName)
		sent := s.emailsTo(member)
		s.Require().Len(sent, len(cases), member.FullName)
		sentKinds := make([]email.Kind, 0, len(sent))
		for _, e := range sent {
			sentKinds = append(sentKinds, e.Kind)
			s.Equal(member.Locale, e.Locale, "each member in their own language")
			s.Equal(member.FullName, e.Params["FullName"])
			s.Equal("Acme", e.Params["TeamName"])
			s.Equal("Shop", e.Params["ProjectName"])
			s.Equal("http://localhost:3002/dashboard/mail?team="+teamID.String()+"&project="+shop.ID.String(), e.Params["MailURL"])
		}
		s.ElementsMatch(kinds, sentKinds)
	}
	s.Equal(2*len(cases)*2, s.deliveries(), "an in-app and an email delivery per member and event")
	s.Zero(s.undelivered())
	s.Zero(s.recordedContaining("/dashboard/mail"), "the link rides in the email only")
}

func (s *notificationSuite) TestMailEvents_ARedeliveredEventIsToldOnce() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	shop := mailProject{ID: uuid.New(), TeamID: teamID, Name: "Shop"}
	cases := mailCases(shop)

	for _, c := range cases {
		message := s.mailMessage(c.routingKey, shop, c.fields)
		s.Require().NoError(s.deliver(message), c.routingKey)
		s.Require().NoError(s.deliver(message), "the shovel redelivers %s", c.routingKey)
	}

	s.Len(s.inbox(ayse), len(cases))
	s.Len(s.pushedTo(ayse, "notification.created"), len(cases))
	s.Len(s.emailsTo(ayse), len(cases))

	verifiedAgain := maps.Clone(cases[0].fields)
	verifiedAgain["verified_at"] = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	s.Require().NoError(s.deliver(s.mailMessage(constants.MailDomainVerified, shop, verifiedAgain)))
	s.Len(s.inbox(ayse), len(cases)+1, "the domain verified again once its records are back is news")
}

func (s *notificationSuite) TestMailEvents_AMalformedPayloadIsRefused() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	shop := mailProject{ID: uuid.New(), TeamID: teamID, Name: "Shop"}
	at := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)

	for _, message := range []*rabbitmq.Message{
		{RoutingKey: constants.MailDomainVerified, Payload: []byte("not json"), MessageID: uuid.NewString()},
		s.mailMessage(constants.MailDomainVerified, mailProject{ID: shop.ID, Name: "Shop"},
			map[string]any{"sending_domain_id": uuid.New(), "domain": "mail.shop.example", "verified_at": at}),
		s.mailMessage(constants.MailPackageExhausted, mailProject{TeamID: teamID, Name: "Shop"},
			map[string]any{"package_code": "10k", "allowance": 10000, "used": 10000, "period": "2026-10", "at": at}),
		s.mailMessage(constants.MailPackageNearLimit, shop,
			map[string]any{"package_code": "10k", "allowance": 10000, "used": 8000, "period": "October", "at": at}),
		s.mailMessage(constants.MailRatesWarning, shop,
			map[string]any{"bounce_rate": "high", "complaint_rate": "0.06", "window_days": 7, "at": at}),
		s.mailMessage(constants.MailSendingPaused, shop,
			map[string]any{"reason": "spam_traps", "bounce_rate": "1.10", "complaint_rate": "0.12", "paused_at": at}),
	} {
		err := s.deliver(message)
		s.Require().ErrorIs(err, notification.ErrorMalformedEvent, string(message.Payload))
		var refused rerror.Error
		s.Require().ErrorAs(err, &refused)
		s.Equal(notification.ErrorMalformedEvent.HttpStatus, refused.HttpStatus, "terminal: the same bytes would fail again")
	}

	s.Zero(s.deliveries(), "nothing is recorded")
	s.Empty(s.email.sent)
	s.Empty(s.realtime.messages)
}

// recordedFor counts the Notifications recorded for the User.
func (s *notificationSuite) recordedFor(u user) int {
	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM notifications WHERE user_id = $1`, u.ID).Scan(&count))

	return count
}

// deliveries counts the Channels Notifications were recorded to go out on.
func (s *notificationSuite) deliveries() int {
	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM notification_deliveries`).Scan(&count))

	return count
}

func (s *notificationSuite) undelivered() int {
	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM notification_deliveries WHERE delivered_at IS NULL`).Scan(&count))

	return count
}
