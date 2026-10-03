package integrationtest

import (
	"net/http"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
)

func (s *notificationSuite) TestInbox_PagesNewestFirst() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	for _, hostname := range []string{"web-1", "web-2", "web-3"} {
		s.becameActive(newDroplet(teamID, hostname), "")
	}

	first := s.request(http.MethodGet, "/v1/notifications?limit=2", ayse.Token)
	s.Require().Equal(http.StatusOK, first.Code, first.Raw)
	s.Require().Len(first.List, 2)
	s.Equal("web-3", first.List[0]["params"].(map[string]any)["hostname"])
	s.Equal("web-2", first.List[1]["params"].(map[string]any)["hostname"])

	last := s.request(http.MethodGet, "/v1/notifications?limit=2&before="+first.List[1]["id"].(string), ayse.Token)
	s.Require().Equal(http.StatusOK, last.Code, last.Raw)
	s.Require().Len(last.List, 1, "a page shorter than the limit is the last")
	s.Equal("web-1", last.List[0]["params"].(map[string]any)["hostname"])
}

func (s *notificationSuite) TestInbox_RefusesAPageLargerThanTheMaximum() {
	ayse := s.signIn("Ayse", "tr")

	s.Equal(http.StatusBadRequest, s.request(http.MethodGet, "/v1/notifications?limit=51", ayse.Token).Code)
	s.Equal(http.StatusBadRequest, s.request(http.MethodGet, "/v1/notifications?before=not-an-id", ayse.Token).Code)
}

func (s *notificationSuite) TestInbox_ReadingOneTellsTheOtherTabs() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	s.becameActive(newDroplet(teamID, "web-1"), "")
	s.becameActive(newDroplet(teamID, "web-2"), "")
	id := s.inbox(ayse)[0]["id"].(string)

	read := s.request(http.MethodPut, "/v1/notifications/"+id+"/read", ayse.Token)

	s.Require().Equal(http.StatusOK, read.Code, read.Raw)
	s.NotNil(read.Body["read_at"])
	s.Equal(1, s.unread(ayse))
	pushed := s.pushedTo(ayse, "notification.read")
	s.Require().Len(pushed, 1)
	s.JSONEq(`{"notification_ids":["`+id+`"]}`, string(pushed[0].Data))

	again := s.request(http.MethodPut, "/v1/notifications/"+id+"/read", ayse.Token)
	s.Require().Equal(http.StatusOK, again.Code)
	s.Equal(read.Body["read_at"], again.Body["read_at"], "the first read stands")
}

func (s *notificationSuite) TestInbox_ReadAll() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	s.becameActive(newDroplet(teamID, "web-1"), "")
	s.becameActive(newDroplet(teamID, "web-2"), "")

	res := s.request(http.MethodPut, "/v1/notifications/read-all", ayse.Token)

	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal(float64(2), res.Body["count"])
	s.Equal(0, s.unread(ayse))
	pushed := s.pushedTo(ayse, "notification.read")
	s.Require().Len(pushed, 1)
	s.JSONEq(`{"all":true}`, string(pushed[0].Data))

	s.Equal(float64(0), s.request(http.MethodPut, "/v1/notifications/read-all", ayse.Token).Body["count"])
	s.Len(s.pushedTo(ayse, "notification.read"), 1, "nothing was left to tell")
}

func (s *notificationSuite) TestInbox_AnotherUsersNotificationIsNotFound() {
	ayse, can := s.signIn("Ayse", "tr"), s.signIn("Can", "tr")
	teamID := s.team("Acme", ayse)
	s.becameActive(newDroplet(teamID, "web-1"), "")
	id := s.inbox(ayse)[0]["id"].(string)

	res := s.request(http.MethodPut, "/v1/notifications/"+id+"/read", can.Token)

	s.Equal(http.StatusNotFound, res.Code)
	domain, code := res.errorCode()
	s.Equal(int(notification.Domain), domain)
	s.Equal(notification.ErrorNotFound.Code, code)
	s.Equal(1, s.unread(ayse))
}

func (s *notificationSuite) TestInbox_ARemovedMemberNoLongerSeesTheTeamsNotifications() {
	ayse, mehmet := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")
	s.becameActive(newDroplet(teamID, "web-1"), "203.0.113.7")
	id := s.inbox(mehmet)[0]["id"].(string)

	s.removeMember(teamID, mehmet)

	s.Empty(s.inbox(mehmet), "the Droplet's address is not theirs to see any more")
	s.Equal(0, s.unread(mehmet))
	s.Equal(http.StatusNotFound, s.request(http.MethodPut, "/v1/notifications/"+id+"/read", mehmet.Token).Code)

	s.addMember(teamID, mehmet, "developer")
	s.Len(s.inbox(mehmet), 1, "hidden, never deleted: back in the Team, back in the Inbox")
}

func (s *notificationSuite) TestInbox_NeedsALiveSession() {
	ayse := s.signIn("Ayse", "tr")

	s.Equal(http.StatusUnauthorized, s.request(http.MethodGet, "/v1/notifications", "").Code)
	s.Equal(http.StatusUnauthorized, s.request(http.MethodGet, "/v1/notifications/unread-count", "not-a-token").Code)

	s.Require().NoError(s.rawRedis.FlushDB(s.T().Context()).Err())
	s.Equal(http.StatusUnauthorized, s.request(http.MethodGet, "/v1/notifications", ayse.Token).Code, "signed out")
}
