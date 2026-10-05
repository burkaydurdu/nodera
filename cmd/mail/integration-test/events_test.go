package integrationtest

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
)

// delivery is a KumoMTA log record about a recipient's copy, at the suite's
// clock; like KumoMTA's own, it names the copy by its position.
func (s *mailSuite) delivery(messageID, recipient, recordType, classification string, code int, enhanced string) string {
	at := s.clock.Now()
	position := 0
	if r, ok := s.injected(messageID, recipient); ok {
		position = r.Position
	}

	return fmt.Sprintf(`{"type":%q,"recipient":%q,"event_time":%q,"timestamp":%d,
		"response":{"code":%d,"enhanced_code":%s,"content":"server says"},"bounce_classification":%q,
		"meta":{"x_message_id":%q,"x_position":%q,"tenant":"p"}}`,
		recordType, recipient, at.Format(time.RFC3339Nano), at.Unix(), code, enhanced, classification, messageID, strconv.Itoa(position))
}

// injected is the copy of a Message the MTA was handed for recipient.
func (s *mailSuite) injected(messageID, recipient string) (message.EnvelopeRecipient, bool) {
	for _, in := range s.mta.Injections() {
		if in.MessageID.String() != messageID {
			continue
		}
		for _, r := range in.Envelope.Recipients {
			if strings.EqualFold(r.Email, recipient) {
				return r, true
			}
		}
	}

	return message.EnvelopeRecipient{}, false
}

// returnPath is the envelope sender recipient's copy went out with.
func (s *mailSuite) returnPath(messageID, recipient string) string {
	r, ok := s.injected(messageID, recipient)
	s.Require().True(ok, "no copy for %s", recipient)

	return r.ReturnPath
}

func (s *mailSuite) oob(sender, finalRecipient string) string {
	return fmt.Sprintf(`{"type":"OOB","sender":%q,"recipient":[%q],"timestamp":1760263500,
		"response":{"code":550,"enhanced_code":{"class":5,"subject":1,"detail":1},"content":"user unknown"},"bounce_classification":"InvalidRecipient","meta":{}}`,
		sender, finalRecipient)
}

func (s *mailSuite) post(record string) *response {
	return s.mtaHook(http.MethodPost, "/internal/v1/mta/events", record, testInternalToken)
}

func (s *mailSuite) recipient(p project, messageID string, i int) map[string]any {
	detail := s.manage(http.MethodGet, projectPath(p, "/messages/"+messageID), "", p.Owner)
	s.Require().Equal(http.StatusOK, detail.Code, detail.Raw)

	return detail.Body["recipients"].([]any)[i].(map[string]any)
}

func (s *mailSuite) suppressions(p project) []map[string]any {
	r := s.manage(http.MethodGet, projectPath(p, "/suppressions"), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)

	return r.items()
}

func (s *mailSuite) TestEvents_DeliveryMovesTheRecipient() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")

	s.Require().Equal(http.StatusNoContent, s.post(s.delivery(id, "Ana@Example.net", "Delivery", "Uncategorized", 250, `{"class":2,"subject":0,"detail":0}`)).Code)

	ana := s.recipient(p, id, 0)
	s.Equal("delivered", ana["status"])
	events := ana["events"].([]any)
	s.Require().Len(events, 2)
	s.Equal("accepted", events[0].(map[string]any)["type"])
	s.Equal(map[string]any{"code": float64(250), "enhanced_code": "2.0.0", "text": "server says"}, events[1].(map[string]any)["response"])
}

func (s *mailSuite) TestEvents_APermanentBounceSuppressesTheAddress() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "gone@example.net").str("id")

	s.Require().Equal(http.StatusNoContent, s.post(s.delivery(id, "gone@example.net", "Bounce", "InvalidRecipient", 550, `{"class":5,"subject":1,"detail":1}`)).Code)

	s.Equal("bounced", s.recipient(p, id, 0)["status"])
	listed := s.suppressions(p)
	s.Require().Len(listed, 1)
	s.Equal("gone@example.net", listed[0]["address"])
	s.Equal("bounce", listed[0]["reason"])
	s.Equal(id, listed[0]["message_id"])

	again := s.sendTo(key, "no-reply@example.com", "gone@example.net")
	s.Equal([]any{"gone@example.net"}, again.Body["suppressed"])
}

func (s *mailSuite) TestEvents_AContentBlockLeavesTheAddressDeliverable() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")

	s.post(s.delivery(id, "ana@example.net", "Bounce", "SpamBlock", 550, `{"class":5,"subject":7,"detail":1}`))

	s.Equal("bounced", s.recipient(p, id, 0)["status"])
	s.Empty(s.suppressions(p))
}

func (s *mailSuite) TestEvents_ARecordPostedTwiceCountsOnce() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")
	record := s.delivery(id, "ana@example.net", "Bounce", "InvalidRecipient", 550, `{"class":5,"subject":1,"detail":1}`)

	s.post(record)
	s.post(record)

	s.Len(s.recipient(p, id, 0)["events"].([]any), 2)
	s.Equal("100.00", s.overview(p).obj("rates")["bounce_rate"])
}

func (s *mailSuite) TestEvents_AnAsynchronousBounceFindsItsRecipientByTheReturnPath() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net", "bob@example.net").str("id")

	s.Require().Equal(http.StatusNoContent, s.post(s.oob(s.returnPath(id, "bob@example.net"), "bob@example.net")).Code)

	s.Equal("accepted", s.recipient(p, id, 0)["status"])
	s.Equal("bounced", s.recipient(p, id, 1)["status"])
	listed := s.suppressions(p)
	s.Require().Len(listed, 1)
	s.Equal("bob@example.net", listed[0]["address"])
}

func (s *mailSuite) TestEvents_AForwardedAddressBouncesUnderItsOwnName() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")

	s.Require().Equal(http.StatusNoContent, s.post(s.oob(s.returnPath(id, "ana@example.net"), "ana.forwarded@elsewhere.example")).Code)

	s.Equal("bounced", s.recipient(p, id, 0)["status"])
	s.Equal("ana@example.net", s.suppressions(p)[0]["address"])
}

func (s *mailSuite) TestEvents_ABounceToAReturnPathNoderaDidNotMakeIsDropped() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net", "bob@example.net").str("id")
	forged := strings.Replace(s.returnPath(id, "ana@example.net"), ".0.", ".1.", 1)

	s.Require().Equal(http.StatusNoContent, s.post(s.oob(forged, "bob@example.net")).Code)

	s.Equal("accepted", s.recipient(p, id, 1)["status"], "one recipient cannot bounce another")
	s.Empty(s.suppressions(p))
}

func (s *mailSuite) TestEvents_AnExpiryIsABounceThatNeitherSuppressesNorCountsAgainstTheLists() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")

	s.Require().Equal(http.StatusNoContent, s.post(s.delivery(id, "ana@example.net", "Expiration", "", 551, `null`)).Code)

	s.Equal("bounced", s.recipient(p, id, 0)["status"])
	s.Empty(s.suppressions(p))
	s.Equal("0.00", s.overview(p).obj("rates")["bounce_rate"])
}

func (s *mailSuite) TestEvents_AComplaintIsSuppressedForGood() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "ana@example.net").str("id")
	s.post(s.delivery(id, "ana@example.net", "Delivery", "", 250, `null`))

	s.Require().Equal(http.StatusNoContent, s.post(fmt.Sprintf(`{"type":"Feedback","recipient":"fbl@rp.%s","timestamp":1760263600,
		"response":{"code":250,"enhanced_code":null,"content":""},"meta":{},
		"feedback_report":{"feedback_type":"abuse","version":"1","original_rcpto_to":["redacted@example.net"],
		"supplemental_trace":{"x_message_id":%q,"x_return_path":%q}}}`, testZone, id, s.returnPath(id, "ana@example.net"))).Code)

	s.Equal("complained", s.recipient(p, id, 0)["status"])
	listed := s.suppressions(p)
	s.Require().Len(listed, 1)
	s.Equal("complaint", listed[0]["reason"])

	s.expectError(s.manage(http.MethodDelete, projectPath(p, "/suppressions/"+listed[0]["id"].(string)), "", p.Owner), http.StatusConflict, 56, 10002)
}

func (s *mailSuite) TestEvents_ABounceSuppressionCanBeRemoved() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "gone@example.net").str("id")
	s.post(s.delivery(id, "gone@example.net", "Bounce", "InvalidRecipient", 550, `{"class":5,"subject":1,"detail":1}`))
	suppressionID := s.suppressions(p)[0]["id"].(string)

	s.Require().Equal(http.StatusNoContent, s.manage(http.MethodDelete, projectPath(p, "/suppressions/"+suppressionID), "", p.Owner).Code)
	s.Empty(s.suppressions(p))
	s.Equal(float64(1), s.sendTo(key, "no-reply@example.com", "gone@example.net").Body["accepted"])
}

func (s *mailSuite) TestEvents_RecordsNotOursOrNotRecordsAreHandled() {
	s.Equal(http.StatusNoContent, s.post(s.delivery("0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d", "ana@example.net", "Delivery", "", 250, `null`)).Code)
	s.Equal(http.StatusNoContent, s.post(`{"type":"Reception","recipient":"ana@example.net","timestamp":1760263500,"meta":{}}`).Code)
	s.expectError(s.post(`not json`), http.StatusBadRequest, 55, 10014)
	s.Equal(http.StatusUnauthorized, s.mtaHook(http.MethodPost, "/internal/v1/mta/events", `[]`, "wrong").Code)
}
