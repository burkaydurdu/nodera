package integrationtest

import (
	"net/http"
	"regexp"
	"time"

	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

var record = regexp.MustCompile(`^n[a-z0-9]{8}(\.example\.com|[ab]\._domainkey\.example\.com)$`)

func (s *mailSuite) TestDomain_AddAnswersTheThreeRecords() {
	p := s.grantProject()

	added := s.addDomain(p, " Example.COM. ")
	s.Equal("example.com", added.str("name"))
	s.Equal("pending", added.str("status"))

	records := added.Body["records"].([]any)
	s.Require().Len(records, 3)
	returnPath := records[0].(map[string]any)
	s.Equal("return_path", returnPath["purpose"])
	s.Equal("CNAME", returnPath["type"])
	s.Equal("rp."+testZone, returnPath["value"])
	for _, raw := range records {
		r := raw.(map[string]any)
		s.Regexp(record, r["host"])
		s.Equal(false, r["valid"])
	}
	dkim := records[1].(map[string]any)
	s.Regexp(`^a\.[a-z0-9]{8}\.dkim\.`+regexp.QuoteMeta(testZone)+`$`, dkim["value"])

	listed := s.manage(http.MethodGet, projectPath(p, "/domains"), "", p.Owner)
	s.Require().Equal(http.StatusOK, listed.Code, listed.Raw)
	s.Len(listed.List, 1)
}

func (s *mailSuite) TestDomain_VerifiesOnlyWhenAllThreeRecordsPointRight() {
	p := s.grantProject()
	added := s.addDomain(p, "example.com")
	id := added.str("id")
	records := added.Body["records"].([]any)

	first := records[0].(map[string]any)
	s.resolver.setCNAME(first["host"].(string), first["value"].(string))
	checked := s.manage(http.MethodPost, projectPath(p, "/domains/"+id+"/verify"), "", p.Owner)
	s.Require().Equal(http.StatusOK, checked.Code, checked.Raw)
	s.Equal("pending", checked.str("status"))
	s.Equal(true, checked.Body["records"].([]any)[0].(map[string]any)["valid"])
	s.Equal(false, checked.Body["records"].([]any)[1].(map[string]any)["valid"])

	s.clock.Advance(time.Minute)
	s.publish(added.Body)
	s.resolver.setTXT("_dmarc.example.com", "v=DMARC1; p=none")
	checked = s.manage(http.MethodPost, projectPath(p, "/domains/"+id+"/verify"), "", p.Owner)
	s.Require().Equal(http.StatusOK, checked.Code, checked.Raw)
	s.Equal("verified", checked.str("status"))
	s.Equal(true, checked.Body["dmarc_found"])

	verified := s.outboxPayloads(constants.MailDomainVerified)
	s.Require().Len(verified, 1)
	s.Equal(id, verified[0]["sending_domain_id"])
	s.Equal("example.com", verified[0]["domain"])
	s.Equal("Shop", verified[0]["project_name"])
	s.Equal(p.TeamID.String(), verified[0]["team_id"])
	s.Contains(s.realtimeTypes(), constants.RealtimeDomainChanged)
}

func (s *mailSuite) TestDomain_VerifyNowWaitsAMinute() {
	p := s.grantProject()
	id := s.addDomain(p, "example.com").str("id")

	s.Require().Equal(http.StatusOK, s.manage(http.MethodPost, projectPath(p, "/domains/"+id+"/verify"), "", p.Owner).Code)
	again := s.manage(http.MethodPost, projectPath(p, "/domains/"+id+"/verify"), "", p.Owner)
	s.expectError(again, http.StatusTooManyRequests, 52, 10005)
	s.NotEmpty(again.Hdr.Get("Retry-After"))
}

// A domain whose records disappear stops sending, is announced, and comes
// back by itself when the records do.
func (s *mailSuite) TestDomain_DailyCheckLosesAndRegainsVerification() {
	p := s.grantProject()
	id := s.verifiedDomain(p, "example.com")
	host := s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner).Body["records"].([]any)[1].(map[string]any)["host"].(string)
	target := s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner).Body["records"].([]any)[1].(map[string]any)["value"].(string)

	s.resolver.removeCNAME(host)
	s.clock.Advance(25 * time.Hour)
	s.Require().NoError(s.jobs.CheckDomains(s.T().Context()))
	s.Equal("unverified", s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner).str("status"))
	lost := s.outboxPayloads(constants.MailDomainUnverified)
	s.Require().Len(lost, 1)
	s.Equal("example.com", lost[0]["domain"])

	s.resolver.setCNAME(host, target)
	s.clock.Advance(25 * time.Hour)
	s.Require().NoError(s.jobs.CheckDomains(s.T().Context()))
	s.Equal("verified", s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner).str("status"))
	s.Len(s.outboxPayloads(constants.MailDomainVerified), 2)
}

// A resolver's bad minute is no answer about the records: the domain keeps
// sending and is checked again soon rather than a day later.
func (s *mailSuite) TestDomain_ACheckDNSDoesNotAnswerChangesNothing() {
	p := s.grantProject()
	id := s.verifiedDomain(p, "example.com")
	checkedAt := s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner).str("last_checked_at")

	s.resolver.fail(true)
	s.clock.Advance(25 * time.Hour)
	s.Require().NoError(s.jobs.CheckDomains(s.T().Context()))

	d := s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner)
	s.Equal("verified", d.str("status"))
	s.Equal(checkedAt, d.str("last_checked_at"))
	s.Empty(s.outboxPayloads(constants.MailDomainUnverified))

	s.resolver.fail(false)
	s.clock.Advance(11 * time.Minute)
	s.Require().NoError(s.jobs.CheckDomains(s.T().Context()))
	s.NotEqual(checkedAt, s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner).str("last_checked_at"), "checked again within minutes")
}

// The token makes every record Project-specific: one Project's records never
// verify another Project's copy of the same domain.
func (s *mailSuite) TestDomain_TwoProjectsShareADomainWithTheirOwnRecords() {
	p := s.grantProject()
	other := s.grantProject()

	s.verifiedDomain(p, "example.com")
	second := s.addDomain(other, "example.com")
	s.NotEqual(s.manage(http.MethodGet, projectPath(p, "/domains"), "", p.Owner).List[0]["records"], second.Body["records"])

	checked := s.manage(http.MethodPost, projectPath(other, "/domains/"+second.str("id")+"/verify"), "", other.Owner)
	s.Require().Equal(http.StatusOK, checked.Code, checked.Raw)
	s.Equal("pending", checked.str("status"), "the first Project's records must not verify the second")
}

func (s *mailSuite) TestDomain_Refusals() {
	p := s.grantProject()

	for _, name := range []string{"localhost", "192.168.1.1", "bad_name.example.com", "x." + testZone} {
		s.expectError(s.manage(http.MethodPost, projectPath(p, "/domains"), `{"name":"`+name+`"}`, p.Owner), http.StatusBadRequest, 52, 10002)
	}

	s.addDomain(p, "example.com")
	s.expectError(s.manage(http.MethodPost, projectPath(p, "/domains"), `{"name":"EXAMPLE.com"}`, p.Owner), http.StatusConflict, 52, 10003)

	for i := range 9 {
		s.addDomain(p, "d"+string(rune('a'+i))+".example.net")
	}
	s.expectError(s.manage(http.MethodPost, projectPath(p, "/domains"), `{"name":"eleventh.example.net"}`, p.Owner), http.StatusConflict, 52, 10004)
}

func (s *mailSuite) TestDomain_DeleteStopsSendingFromIt() {
	p, key := s.ready()
	id := s.manage(http.MethodGet, projectPath(p, "/domains"), "", p.Owner).List[0]["id"].(string)

	s.Require().Equal(http.StatusNoContent, s.manage(http.MethodDelete, projectPath(p, "/domains/"+id), "", p.Owner).Code)
	s.expectError(s.manage(http.MethodGet, projectPath(p, "/domains/"+id), "", p.Owner), http.StatusNotFound, 52, 10001)
	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusUnprocessableEntity, 55, 10009)
}

func (s *mailSuite) TestDomain_SigningKeyIsTheActiveSelectorsInPEM() {
	p := s.grantProject()
	id := s.verifiedDomain(p, "example.com")

	key := s.mtaHook(http.MethodGet, "/internal/v1/dkim-keys/"+id, "", testInternalToken)
	s.Require().Equal(http.StatusOK, key.Code, key.Raw)
	s.Equal("example.com", key.str("domain"))
	s.Regexp(`^n[a-z0-9]{8}a$`, key.str("selector"))
	s.Contains(key.str("private_key_pem"), "-----BEGIN PRIVATE KEY-----")

	s.Equal(http.StatusUnauthorized, s.mtaHook(http.MethodGet, "/internal/v1/dkim-keys/"+id, "", "wrong").Code)
}

// A rotation publishes the idle selector's fresh key at once and signs with
// it only after the wait.
func (s *mailSuite) TestDomain_KeysRotateWithoutTheTeam() {
	p := s.grantProject()
	id := s.verifiedDomain(p, "example.com")
	before := s.mtaHook(http.MethodGet, "/internal/v1/dkim-keys/"+id, "", testInternalToken).str("selector")

	s.clock.Advance(181 * 24 * time.Hour)
	s.Require().NoError(s.jobs.RotateKeys(s.T().Context()))
	s.Equal(before, s.mtaHook(http.MethodGet, "/internal/v1/dkim-keys/"+id, "", testInternalToken).str("selector"), "the fresh key does not sign yet")

	s.clock.Advance(49 * time.Hour)
	s.Require().NoError(s.jobs.RotateKeys(s.T().Context()))
	after := s.mtaHook(http.MethodGet, "/internal/v1/dkim-keys/"+id, "", testInternalToken).str("selector")
	s.NotEqual(before, after)
	s.Regexp(`^n[a-z0-9]{8}b$`, after)
}
