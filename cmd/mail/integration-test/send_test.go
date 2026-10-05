package integrationtest

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

func addresses(n int, prefix string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d@example.net", prefix, i)
	}

	return out
}

func (s *mailSuite) TestSend_AcceptsCountsAndHandsOver() {
	p, key := s.ready()

	sent := s.sendTo(key, "No-Reply@Example.com", "Ana@Example.net", "bob@example.net")
	s.Require().Equal(http.StatusAccepted, sent.Code, sent.Raw)
	s.Equal(float64(2), sent.Body["accepted"])
	s.Equal([]any{}, sent.Body["suppressed"])
	id := sent.str("id")

	injections := s.mta.Injections()
	s.Require().Len(injections, 1)
	in := injections[0]
	s.Equal(id, in.MessageID.String())
	s.Equal("example.com", in.Envelope.DKIMDomain)
	s.Require().Len(in.Envelope.Recipients, 2)
	s.Equal("ana@example.net", in.Envelope.Recipients[0].Email)
	for position, r := range in.Envelope.Recipients {
		s.Equal(position, r.Position)
		s.Regexp(`^b\+`+id+`\.`+strconv.Itoa(position)+`\.[0-9a-f]{12}@n[a-z0-9]{8}\.example\.com$`, r.ReturnPath, "each copy has its own return path")
	}
	content := string(in.Content)
	s.Contains(content, "Message-Id: <"+id+"@example.com>")
	s.Contains(content, "Feedback-Id: "+p.ProjectID.String()+":"+p.TeamID.String()+":mail:nodera")
	s.Contains(content, "Subject: Your code")

	o := s.overview(p)
	s.Equal(float64(2), o.obj("period")["used"])
	s.Equal(float64(2), o.obj("daily_limit")["used"])

	logs := s.manage(http.MethodGet, projectPath(p, "/messages"), "", p.Owner)
	s.Require().Equal(http.StatusOK, logs.Code, logs.Raw)
	s.Require().Len(logs.items(), 1)
	entry := logs.items()[0]
	s.Equal(id, entry["id"])
	s.Equal("no-reply@example.com", entry["from"])
	s.Equal("api", entry["source"])
	s.Equal([]any{"auth"}, entry["tags"])
	s.Equal([]any{
		map[string]any{"address": "ana@example.net", "status": "accepted"},
		map[string]any{"address": "bob@example.net", "status": "accepted"},
	}, entry["recipients"])

	var spooled int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM message_spool`).Scan(&spooled))
	s.Zero(spooled, "the content is deleted once the MTA holds it")
}

func (s *mailSuite) TestSend_IdempotencyKeyAnswersTheFirstMessage() {
	p, key := s.ready()
	body := `{"from":{"email":"no-reply@example.com"},"to":[{"email":"ana@example.net"}],"subject":"Hi","text":"x"}`

	first := s.send(key, body, "Idempotency-Key", "order-42")
	second := s.send(key, body, "Idempotency-Key", "order-42")
	s.Require().Equal(http.StatusAccepted, first.Code, first.Raw)
	s.Require().Equal(http.StatusAccepted, second.Code, second.Raw)
	s.Equal(first.str("id"), second.str("id"))
	s.Len(s.mta.Injections(), 1)
	s.Equal(float64(1), s.overview(p).obj("period")["used"])

	s.clock.Advance(25 * time.Hour)
	third := s.send(key, body, "Idempotency-Key", "order-42")
	s.NotEqual(first.str("id"), third.str("id"), "a key repeats for 24 hours only")
}

func (s *mailSuite) TestSend_ValidationNamesTheProblem() {
	_, key := s.ready()
	base := `"from":{"email":"no-reply@example.com"},"to":[{"email":"ana@example.net"}],"subject":"Hi"`

	s.expectError(s.send(key, `{`+base+`}`), http.StatusBadRequest, 55, 10006)
	s.expectError(s.send(key, `{`+base+`,"text":"x","headers":{"From":"evil@example.org"}}`), http.StatusBadRequest, 55, 10007)
	s.expectError(s.send(key, `{`+base+`,"text":"x","headers":{"X-Order":"1\r\nBcc: evil@example.org"}}`), http.StatusBadRequest, 55, 10013)
	s.expectError(s.send(key, `{`+base+`,"text":"x","attachments":[{"filename":"a.pdf","content_type":"application/pdf","content":"%%%"}]}`), http.StatusBadRequest, 55, 10008)
	s.expectError(s.send(key, `{`+base+`,"text":"x","tags":["`+strings.Repeat("t", 65)+`"]}`), http.StatusBadRequest, 55, 10012)
	s.expectError(s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[{"email":"not an address"}],"subject":"Hi","text":"x"}`), http.StatusBadRequest, 55, 10004)

	list := func(n int, prefix string) string {
		out := make([]string, n)
		for i, a := range addresses(n, prefix) {
			out[i] = fmt.Sprintf(`{"email":%q}`, a)
		}

		return strings.Join(out, ",")
	}
	oneList := s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[`+list(51, "r")+`],"subject":"Hi","text":"x"}`)
	s.Require().Equal(http.StatusBadRequest, oneList.Code, oneList.Raw)
	s.Equal("to", oneList.List[0]["field"])
	s.expectError(s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[`+list(30, "t")+`],"cc":[`+list(30, "c")+`],"subject":"Hi","text":"x"}`), http.StatusBadRequest, 55, 10003)

	big := base64.StdEncoding.EncodeToString(make([]byte, 10<<20+1))
	s.expectError(s.send(key, `{`+base+`,"text":"x","attachments":[{"filename":"a.bin","content_type":"application/octet-stream","content":"`+big+`"}]}`), http.StatusBadRequest, 55, 10005)
	s.Empty(s.mta.Injections())
}

func (s *mailSuite) TestSend_AttachmentsReachTheMTA() {
	_, key := s.ready()
	content := base64.StdEncoding.EncodeToString([]byte("%PDF-1.7 invoice"))

	sent := s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[{"email":"ana@example.net"}],"cc":[{"email":"cc@example.net"}],
		"bcc":[{"email":"hidden@example.net"}],"reply_to":{"email":"help@example.com"},"subject":"Invoice","html":"<p>Attached</p>",
		"attachments":[{"filename":"invoice.pdf","content_type":"application/pdf","content":"`+content+`"}],"headers":{"X-Order":"42"}}`)
	s.Require().Equal(http.StatusAccepted, sent.Code, sent.Raw)
	s.Equal(float64(3), sent.Body["accepted"])

	raw := string(s.mta.Injections()[0].Content)
	s.Contains(raw, "Cc: <cc@example.net>")
	s.Contains(raw, "Reply-To: <help@example.com>")
	s.Contains(raw, "X-Order: 42")
	s.Contains(raw, `filename=invoice.pdf`)
	s.NotContains(raw, "hidden@example.net", "a Bcc is only on the envelope")
	s.Len(s.mta.Injections()[0].Envelope.Recipients, 3)
}

func (s *mailSuite) TestSend_SenderMustBeOnAVerifiedSendingDomain() {
	p, key := s.ready()

	s.expectError(s.sendTo(key, "no-reply@other.org", "ana@example.net"), http.StatusUnprocessableEntity, 55, 10009)

	s.addDomain(p, "pending.example.org")
	s.expectError(s.sendTo(key, "no-reply@pending.example.org", "ana@example.net"), http.StatusUnprocessableEntity, 55, 10009)

	other := s.grantProject()
	s.verifiedDomain(other, "theirs.example.org")
	s.expectError(s.sendTo(key, "no-reply@theirs.example.org", "ana@example.net"), http.StatusUnprocessableEntity, 55, 10009)
}

func (s *mailSuite) TestSend_RefusalsInTheSpecsOrder() {
	p := s.grantProject()
	s.verifiedDomain(p, "example.com")
	key := s.apiKey(p)

	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusPaymentRequired, 53, 10002)

	s.Require().Equal(http.StatusOK, s.choose(p, "10k").Code)
	s.exec(`UPDATE project_limits SET paused_at = now(), pause_reason = 'bounce_rate' WHERE project_id = $1`, p.ProjectID)
	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusLocked, 54, 10001)

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusLocked, 51, 10004)
	s.Empty(s.mta.Injections())
}

func (s *mailSuite) TestSend_DailyLimitHoldsUntilMidnight() {
	p, key := s.ready()
	for i := range 4 {
		s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(50, fmt.Sprintf("d%d-", i))...).Code)
	}

	refused := s.sendTo(key, "no-reply@example.com", "one-more@example.net")
	s.expectError(refused, http.StatusTooManyRequests, 54, 10002)
	s.Equal("50401", refused.Hdr.Get("Retry-After"), "fourteen hours to the next UTC midnight")
	s.Equal("daily_limit", s.overview(p).str("sending_state"))
	s.Equal(float64(200), s.overview(p).obj("period")["used"], "a refused Message costs nothing")

	s.clock.Set(time.Date(2026, 10, 13, 0, 0, 1, 0, time.UTC))
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "one-more@example.net").Code)
}

func (s *mailSuite) TestSend_AllowanceIsAnnouncedAndThenRefused() {
	p, key := s.ready()
	s.exec(`UPDATE project_limits SET daily_limit = 100000 WHERE project_id = $1`, p.ProjectID)
	s.exec(`UPDATE mail_periods SET used = 7999 WHERE project_id = $1`, p.ProjectID)

	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "a@example.net").Code)
	near := s.outboxPayloads(constants.MailPackageNearLimit)
	s.Require().Len(near, 1)
	s.Equal(map[string]any{
		"team_id": p.TeamID.String(), "project_id": p.ProjectID.String(), "project_name": "Shop", "package_code": "10k",
		"allowance": float64(10000), "used": float64(8000), "period": "2026-10", "at": near[0]["at"],
	}, near[0])

	s.exec(`UPDATE mail_periods SET used = 9998 WHERE project_id = $1`, p.ProjectID)
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "b@example.net", "c@example.net").Code)
	s.Len(s.outboxPayloads(constants.MailPackageExhausted), 1)
	s.Len(s.outboxPayloads(constants.MailPackageNearLimit), 1, "each threshold once per Period")

	refused := s.sendTo(key, "no-reply@example.com", "d@example.net")
	s.expectError(refused, http.StatusTooManyRequests, 53, 10003)
	s.NotEmpty(refused.Hdr.Get("Retry-After"))
	s.Equal("exhausted", s.overview(p).str("sending_state"))

	s.Require().Equal(http.StatusOK, s.choose(p, "25k").Code)
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "d@example.net").Code, "an upgrade resumes sending at once")
}

func (s *mailSuite) TestSend_SuppressedRecipientsAreDroppedAndNamed() {
	p, key := s.ready()
	s.exec(`INSERT INTO suppressions (id, project_id, address, reason, created_at) VALUES (gen_random_uuid(), $1, 'gone@example.net', 'bounce', now())`, p.ProjectID)

	sent := s.sendTo(key, "no-reply@example.com", "gone@example.net", "ana@example.net")
	s.Require().Equal(http.StatusAccepted, sent.Code, sent.Raw)
	s.Equal(float64(1), sent.Body["accepted"])
	s.Equal([]any{"gone@example.net"}, sent.Body["suppressed"])
	s.Equal(float64(1), s.overview(p).obj("period")["used"])
	s.Len(s.mta.Injections()[0].Envelope.Recipients, 1)

	only := s.sendTo(key, "no-reply@example.com", "gone@example.net")
	s.Require().Equal(http.StatusAccepted, only.Code, only.Raw)
	s.Equal(float64(0), only.Body["accepted"])
	s.Len(s.mta.Injections(), 1, "nothing is handed over for a Message with nobody to send to")
}

func (s *mailSuite) TestSend_KeysAreCheckedBehindTheGateway() {
	p, key := s.ready()

	s.expectError(s.send("", `{}`), http.StatusUnauthorized, 58, 10001)
	s.expectError(s.send("nd_short.secret", `{}`), http.StatusUnauthorized, 58, 10001)
	s.expectError(s.sendTo(s.apiKey(p, "droplet.read"), "no-reply@example.com", "ana@example.net"), http.StatusForbidden, 58, 10002)

	s.revokeKey(key)
	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusUnauthorized, 58, 10001)
}

func (s *mailSuite) TestSend_KeepsTheMessageIDTheClientChose() {
	p, key := s.ready()

	sent := s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[{"email":"ana@example.net"}],"subject":"Order","text":"x",
		"headers":{"message-id":"<order-42@shop.example.com>"}}`)
	s.Require().Equal(http.StatusAccepted, sent.Code, sent.Raw)

	s.Contains(string(s.mta.Injections()[0].Content), "Message-Id: <order-42@shop.example.com>")
	detail := s.manage(http.MethodGet, projectPath(p, "/messages/"+sent.str("id")), "", p.Owner)
	s.Equal("order-42@shop.example.com", detail.str("message_id_header"))
}

func (s *mailSuite) TestSend_RefusesAMessageIDThatIsNotOne() {
	_, key := s.ready()

	s.expectError(s.send(key, `{"from":{"email":"no-reply@example.com"},"to":[{"email":"ana@example.net"}],"subject":"Order","text":"x",
		"headers":{"Message-ID":"no host here"}}`), http.StatusBadRequest, 55, 10013)
}

func (s *mailSuite) TestSend_AnUnreachableMTAIsRetriedFromTheSpool() {
	p, key := s.ready()
	s.mta.FailWith(errors.New("connection refused"))

	sent := s.sendTo(key, "no-reply@example.com", "ana@example.net")
	s.Require().Equal(http.StatusAccepted, sent.Code, sent.Raw)
	s.Empty(s.mta.Injections())

	s.mta.FailWith(nil)
	s.Require().NoError(s.jobs.DispatchSpool(s.T().Context()))
	s.Empty(s.mta.Injections(), "the retry waits its backoff")

	s.clock.Advance(2 * time.Minute)
	s.Require().NoError(s.jobs.DispatchSpool(s.T().Context()))
	s.Require().Len(s.mta.Injections(), 1)
	s.Require().NoError(s.jobs.DispatchSpool(s.T().Context()))
	s.Len(s.mta.Injections(), 1, "handed over once")
	s.Equal("accepted", s.manage(http.MethodGet, projectPath(p, "/messages/"+sent.str("id")), "", p.Owner).Body["recipients"].([]any)[0].(map[string]any)["status"])
}

// The MTA refusing a copy is never the receiving server's verdict: the copy
// waits in the spool while the others go.
func (s *mailSuite) TestSend_CopiesTheMTARefusesAreHandedOverLater() {
	p, key := s.ready()
	s.mta.Refuse("bob@example.net", "signing key unavailable")

	sent := s.sendTo(key, "no-reply@example.com", "ana@example.net", "bob@example.net")
	s.Require().Equal(http.StatusAccepted, sent.Code, sent.Raw)
	s.Require().Len(s.mta.Injections(), 1)
	s.Equal("accepted", s.recipient(p, sent.str("id"), 1)["status"])

	s.mta.Admit("bob@example.net")
	s.clock.Advance(2 * time.Minute)
	s.Require().NoError(s.jobs.DispatchSpool(s.T().Context()))

	injections := s.mta.Injections()
	s.Require().Len(injections, 2)
	s.Require().Len(injections[1].Envelope.Recipients, 1, "only the refused copy is handed over again")
	s.Equal("bob@example.net", injections[1].Envelope.Recipients[0].Email)
	var spooled int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM message_spool`).Scan(&spooled))
	s.Zero(spooled)
}

func (s *mailSuite) TestSend_ACopyTheMTAKeepsRefusingBouncesAfterADay() {
	p, key := s.ready()
	s.mta.Refuse("bob@example.net", "signing key unavailable")
	sent := s.sendTo(key, "no-reply@example.com", "ana@example.net", "bob@example.net")

	s.clock.Advance(25 * time.Hour)
	s.Require().NoError(s.jobs.DispatchSpool(s.T().Context()))

	s.Equal("accepted", s.recipient(p, sent.str("id"), 0)["status"])
	bob := s.recipient(p, sent.str("id"), 1)
	s.Equal("bounced", bob["status"])
	events := bob["events"].([]any)
	s.Equal("the message could not be handed to Nodera's delivery servers", events[len(events)-1].(map[string]any)["response"].(map[string]any)["text"])
	s.Empty(s.suppressions(p), "Nodera's own refusal says nothing about the address")
}

func (s *mailSuite) TestSend_AMessageTheMTANeverTookBouncesAfterADay() {
	p, key := s.ready()
	s.mta.FailWith(errors.New("connection refused"))
	sent := s.sendTo(key, "no-reply@example.com", "ana@example.net")

	s.clock.Advance(25 * time.Hour)
	s.Require().NoError(s.jobs.DispatchSpool(s.T().Context()))

	detail := s.manage(http.MethodGet, projectPath(p, "/messages/"+sent.str("id")), "", p.Owner)
	s.Equal("bounced", detail.Body["recipients"].([]any)[0].(map[string]any)["status"])
	var spooled int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM message_spool`).Scan(&spooled))
	s.Zero(spooled)
}
