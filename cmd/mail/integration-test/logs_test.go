package integrationtest

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (s *mailSuite) TestLogs_FiltersAndPages() {
	p, key := s.ready()
	first := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")
	s.clock.Advance(time.Minute)
	second := s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[{"email":"bob@example.net"}],"subject":"Receipt","text":"x","tags":["billing"]}`).str("id")
	s.clock.Advance(time.Minute)
	third := s.sendTo(key, "no-reply@example.com", "carl@example.net").str("id")
	s.post(s.delivery(third, "carl@example.net", "Delivery", "", 250, `null`))

	list := func(query string) []map[string]any {
		r := s.manage(http.MethodGet, projectPath(p, "/messages")+"?"+query, "", p.Owner)
		s.Require().Equal(http.StatusOK, r.Code, r.Raw)

		return r.items()
	}

	all := list("")
	s.Require().Len(all, 3)
	s.Equal(third, all[0]["id"], "newest first")
	s.Equal([]string{second}, ids(list("tag=billing")))
	s.Equal([]string{first}, ids(list("recipient=ANA@example.net")))
	s.Equal([]string{third}, ids(list("status=delivered")))
	s.Equal([]string{second, first}, ids(list("status=accepted&from=no-reply@example.com")), "a delivered Message is no longer accepted")
	s.Equal([]string{second, first}, ids(list("until="+url.QueryEscape(start.Add(90*time.Second).Format(time.RFC3339)))))

	page := s.manage(http.MethodGet, projectPath(p, "/messages")+"?limit=2", "", p.Owner)
	s.Require().Len(page.items(), 2)
	cursor := page.str("next_cursor")
	s.NotEmpty(cursor)
	next := s.manage(http.MethodGet, projectPath(p, "/messages")+"?limit=2&cursor="+cursor, "", p.Owner)
	s.Equal([]string{first}, ids(next.items()))
	s.Nil(next.Body["next_cursor"])

	s.Equal(http.StatusBadRequest, s.manage(http.MethodGet, projectPath(p, "/messages")+"?status=opened", "", p.Owner).Code)
	s.expectError(s.manage(http.MethodGet, projectPath(p, "/messages")+"?cursor=broken", "", p.Owner), http.StatusBadRequest, 55, 10010)
}

func ids(items []map[string]any) []string {
	out := []string{}
	for _, item := range items {
		out = append(out, fmt.Sprint(item["id"]))
	}

	return out
}

func (s *mailSuite) TestLogs_ExpireAfterThirtyDays() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")

	s.clock.Advance(29 * 24 * time.Hour)
	s.Require().NoError(s.jobs.Expire(s.T().Context()))
	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, "/messages/"+id), "", p.Owner).Code)

	s.clock.Advance(2 * 24 * time.Hour)
	s.Require().NoError(s.jobs.Expire(s.T().Context()))
	s.expectError(s.manage(http.MethodGet, projectPath(p, "/messages/"+id), "", p.Owner), http.StatusNotFound, 55, 10001)
	s.Empty(s.manage(http.MethodGet, projectPath(p, "/messages"), "", p.Owner).items())
}
