package integrationtest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *mailSuite) createWebhook(p project, events ...string) *response {
	list, _ := json.Marshal(events)
	r := s.manage(http.MethodPost, projectPath(p, "/webhooks"), fmt.Sprintf(`{"url":%q,"events":%s}`, s.hook.server.URL+"/nodera", list), p.Owner)
	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)

	return r
}

func verify(secret string, r received) bool {
	parts := strings.Split(r.Header.Get("Nodera-Signature"), ",")
	if len(parts) != 2 {
		return false
	}
	t, v1 := strings.TrimPrefix(parts[0], "t="), strings.TrimPrefix(parts[1], "v1=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(t + "."))
	mac.Write(r.Body)

	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(v1))
}

func (s *mailSuite) TestWebhooks_TheSecretIsShownOnce() {
	p := s.grantProject()

	created := s.createWebhook(p, "delivered", "accepted")
	s.Regexp(`^whsec_`, created.str("secret"))
	s.Equal([]any{"accepted", "delivered"}, created.Body["events"])

	listed := s.manage(http.MethodGet, projectPath(p, "/webhooks"), "", p.Owner)
	s.Require().Len(listed.List, 1)
	s.Nil(listed.List[0]["secret"])

	s.createWebhook(p, "bounced")
	s.createWebhook(p, "complained")
	s.expectError(s.manage(http.MethodPost, projectPath(p, "/webhooks"), `{"url":"http://127.0.0.1/x","events":["bounced"]}`, p.Owner), http.StatusConflict, 57, 10002)

	other := s.grantProject()
	s.expectError(s.manage(http.MethodPost, projectPath(other, "/webhooks"), `{"url":"ftp://hooks.example.com","events":["bounced"]}`, other.Owner), http.StatusBadRequest, 57, 10003)
	s.Equal(http.StatusBadRequest, s.manage(http.MethodPost, projectPath(other, "/webhooks"), `{"url":"https://hooks.example.com","events":["opened"]}`, other.Owner).Code)
}

func (s *mailSuite) TestWebhooks_DeliverSignedBatches() {
	p, key := s.ready()
	secret := s.createWebhook(p, "accepted", "delivered").str("secret")

	id := s.sendTo(key, "no-reply@example.com", "ana@example.net", "bob@example.net").str("id")
	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))

	got := s.hook.received()
	s.Require().Len(got, 1)
	s.True(verify(secret, got[0]), "the signature checks with the secret")
	s.NotEmpty(got[0].Header.Get("Nodera-Delivery-Id"))
	var batch []map[string]any
	s.Require().NoError(json.Unmarshal(got[0].Body, &batch))
	s.Require().Len(batch, 2)
	s.Equal("accepted", batch[0]["type"])
	s.Equal(id, batch[0]["message_id"])
	s.Equal("ana@example.net", batch[0]["recipient"])
	s.Equal([]any{"auth"}, batch[0]["tags"])

	s.post(s.delivery(id, "ana@example.net", "Delivery", "", 250, `null`))
	s.post(s.delivery(id, "bob@example.net", "TransientFailure", "", 451, `null`))
	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))
	got = s.hook.received()
	s.Require().Len(got, 2, "deferred is not subscribed")
	s.Require().NoError(json.Unmarshal(got[1].Body, &batch))
	s.Require().Len(batch, 1)
	s.Equal("delivered", batch[0]["type"])
}

func (s *mailSuite) TestWebhooks_RetryForADayThenDrop() {
	p, key := s.ready()
	webhookID := s.createWebhook(p, "accepted").str("id")
	s.hook.answer(http.StatusInternalServerError)
	s.sendTo(key, "no-reply@example.com", "ana@example.net")

	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))
	deliveries := s.manage(http.MethodGet, projectPath(p, "/webhooks/"+webhookID+"/deliveries"), "", p.Owner)
	s.Require().Len(deliveries.items(), 1)
	d := deliveries.items()[0]
	s.Equal("pending", d["status"])
	s.Equal(float64(1), d["attempts"])
	s.Equal(float64(500), d["last_status_code"])

	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))
	s.Len(s.hook.received(), 1, "the retry waits its turn")

	s.clock.Advance(90 * time.Second)
	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))
	s.Len(s.hook.received(), 2)

	s.clock.Advance(25 * time.Hour)
	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))
	d = s.manage(http.MethodGet, projectPath(p, "/webhooks/"+webhookID+"/deliveries"), "", p.Owner).items()[0]
	s.Equal("failed", d["status"])
	s.Equal("the webhook answered 500", d["last_error"])
}

func (s *mailSuite) TestWebhooks_ADisabledWebhookGetsNothing() {
	p, key := s.ready()
	webhookID := s.createWebhook(p, "accepted").str("id")
	updated := s.manage(http.MethodPut, projectPath(p, "/webhooks/"+webhookID), fmt.Sprintf(`{"url":%q,"events":["accepted"],"active":false}`, s.hook.server.URL), p.Owner)
	s.Require().Equal(http.StatusOK, updated.Code, updated.Raw)

	s.sendTo(key, "no-reply@example.com", "ana@example.net")
	s.Require().NoError(s.jobs.DeliverWebhooks(s.T().Context()))
	s.Empty(s.hook.received())

	s.Require().Equal(http.StatusNoContent, s.manage(http.MethodDelete, projectPath(p, "/webhooks/"+webhookID), "", p.Owner).Code)
	s.Empty(s.manage(http.MethodGet, projectPath(p, "/webhooks"), "", p.Owner).List)
}
