package integrationtest

import (
	"github.com/piggystats/nodera/contexts/notification/email"
)

func (s *notificationSuite) TestDropletActive_ReachesTheInboxOfEveryMemberOfTheTeam() {
	ayse, mehmet, zeynep, stranger := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr"), s.signIn("Zeynep", "en"), s.signIn("Can", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")
	s.addMember(teamID, zeynep, "finance")
	s.team("Other", stranger)
	web := newDroplet(teamID, "web-1")

	s.becameActive(web, "203.0.113.7")

	for _, member := range []user{ayse, mehmet, zeynep} {
		items := s.inbox(member)
		s.Require().Len(items, 1, member.FullName)
		s.Equal("droplet_active", items[0]["type"])
		s.Equal(teamID.String(), items[0]["team_id"])
		s.Nil(items[0]["read_at"])
		s.Equal(map[string]any{
			"droplet_id": web.ID.String(), "project_id": web.ProjectID.String(), "hostname": "web-1", "ipv4": "203.0.113.7",
		}, items[0]["params"])
		s.Equal(1, s.unread(member))
	}
	s.Empty(s.inbox(stranger), "another Team's Droplet is none of their business")
	s.Empty(s.email.sent, "a Droplet becoming ready is told in the app only")
}

func (s *notificationSuite) TestDropletActive_IsPushedLiveToEachRecipient() {
	ayse, mehmet := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")

	s.becameActive(newDroplet(teamID, "web-1"), "203.0.113.7")

	for _, member := range []user{ayse, mehmet} {
		pushed := s.pushedTo(member, "notification.created")
		s.Require().Len(pushed, 1, member.FullName)
		s.JSONEq(s.json(s.inbox(member)[0]), string(pushed[0].Data), "the push carries the Inbox item itself")
	}
}

func (s *notificationSuite) TestDropletActive_ARedeliveredEventRecordsAndPushesNothingTwice() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	web := newDroplet(teamID, "web-1")

	s.becameActive(web, "203.0.113.7")
	s.becameActive(web, "203.0.113.7")

	s.Len(s.inbox(ayse), 1)
	s.Len(s.pushedTo(ayse, "notification.created"), 1)
}

func (s *notificationSuite) TestDropletStatus_OtherMovesTellNobody() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	web := newDroplet(teamID, "web-1")
	active, off := "active", "off"

	s.Require().NoError(s.changed(web, nil, "provisioning", nil))
	s.Require().NoError(s.changed(web, &active, "off", nil))
	s.Require().NoError(s.changed(web, &off, "active", nil))

	s.Empty(s.inbox(ayse), "powering a Droplet off and on again is no news")
	s.Empty(s.realtime.messages)
}

func (s *notificationSuite) TestDropletFailed_GoesByEmailToo() {
	ayse, zeynep := s.signIn("Ayse", "tr"), s.signIn("Zeynep", "en")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, zeynep, "finance")
	web := newDroplet(teamID, "web-1")

	s.Require().NoError(s.failed(web, "rejected"))

	items := s.inbox(ayse)
	s.Require().Len(items, 1)
	s.Equal("droplet_failed", items[0]["type"])
	s.Equal(map[string]any{
		"droplet_id": web.ID.String(), "project_id": web.ProjectID.String(), "hostname": "web-1", "failure_code": "rejected",
	}, items[0]["params"], "the web app words the reason from its code; no English sentence is recorded")

	sent := s.emailsTo(zeynep)
	s.Require().Len(sent, 1)
	s.Equal(email.KindDropletFailed, sent[0].Kind)
	s.Equal("en", sent[0].Locale, "each member in their own language")
	s.Equal("Acme", sent[0].Params["TeamName"])
	s.Equal("Zeynep", sent[0].Params["FullName"])
	s.Equal("web-1", sent[0].Params["Hostname"])
	s.Equal("droplet_failure_rejected", sent[0].Params["FailureKey"])
	s.Len(s.emailsTo(ayse), 1)
}

func (s *notificationSuite) TestDelivery_ARedeliveryRetriesOnlyTheChannelThatFailed() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	web := newDroplet(teamID, "web-1")

	s.email.failNext = true
	s.Require().Error(s.failed(web, "rejected"), "the event goes to the error queue")
	s.Len(s.inbox(ayse), 1, "the Notification is recorded even though the email did not go")
	s.Len(s.pushedTo(ayse, "notification.created"), 1)
	s.Empty(s.emailsTo(ayse))

	s.Require().NoError(s.failed(web, "rejected"), "the shovel redelivers it")
	s.Len(s.emailsTo(ayse), 1)
	s.Len(s.pushedTo(ayse, "notification.created"), 1, "the push already went out")
	s.Len(s.inbox(ayse), 1)
}

func (s *notificationSuite) TestDropletStatus_AMalformedEventIsRefused() {
	s.Error(s.consumeErr("DropletStatusChanged", `{"payload":{"status":"active"}}`))
	s.Error(s.consumeErr("DropletStatusChanged", `not json`))
}
