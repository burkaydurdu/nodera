package integrationtest

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/providers/fake"
)

const fakeThreeDSPage = "https://hosted.payments.test/3ds/"

func (s *billingSuite) postForm(path string, form url.Values) *response {
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")

	return s.requestWith(http.MethodPost, path, form.Encode(), "", headers)
}

// confirmationLink closes October for a Team whose bank refuses the charge
// without the cardholder, and answers the attempt and its link's token.
func (s *billingSuite) confirmationLink() (user, uuid.UUID, string, string) {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeNeedsConfirmation()
	s.closePeriods(closingTime)

	requests := s.outboxPayloads(constants.PaymentConfirmationRequired)
	s.Require().Len(requests, 1)
	link := requests[0]["confirm_url"].(string)

	return owner, teamID, s.queryParam(link, "payment"), s.queryParam(link, "token")
}

func paymentPath(paymentID, rest, token string) string {
	return "/v1/billing/payments/" + paymentID + rest + "?token=" + url.QueryEscape(token)
}

// The bank refused the charge without the cardholder: nothing is opened at
// the provider until the cardholder follows the link, the provider calls the
// service back, and the service completes the charge and sends the browser to
// the result page.
func (s *billingSuite) TestConfirm_TheLinkOpens3DSAndTheCallbackCompletesTheCharge() {
	owner, teamID, paymentID, token := s.confirmationLink()
	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal("issued", detail.str("status"))
	attempt := s.objectList(detail.Body["payments"])[0]
	s.Equal("confirmation_required", attempt["status"])
	link := s.outboxPayloads(constants.PaymentConfirmationRequired)[0]["confirm_url"].(string)
	s.True(strings.HasPrefix(link, frontURL+"/billing/confirm?payment="+paymentID+"&token="), link)
	s.Equal(link, attempt["confirmation_url"], "the app sends the cardholder to the same page")
	s.Empty(s.fake.Confirmations, "no 3-D Secure step before the cardholder is there")

	status := s.get(paymentPath(paymentID, "/confirm", token), "")
	s.Require().Equal(http.StatusOK, status.Code, status.Raw)
	s.Equal("confirmation_required", status.str("status"))
	s.Empty(s.fake.Confirmations, "the status route never calls the provider")

	started := s.request(http.MethodPost, paymentPath(paymentID, "/3ds/start", token), "", "")
	s.Require().Equal(http.StatusOK, started.Code, started.Raw)
	redirect := started.str("redirect_url")
	s.True(strings.HasPrefix(redirect, fakeThreeDSPage), redirect)
	s.Nil(started.Body["html_content"])
	s.Require().Len(s.fake.Confirmations, 1)
	sent := s.fake.Confirmations[0]
	s.Equal(gatewayURL+paymentPath(paymentID, "/3ds/callback", token), sent.ReturnURL, "the provider calls the gateway, not a web page")
	s.Equal("invoice:"+detail.str("id")+":attempt:1", sent.IdempotencyKey)
	s.Equal("10.00", sent.Amount.StringFixed(2))
	ref := strings.SplitN(strings.TrimPrefix(redirect, fakeThreeDSPage), "?", 2)[0]
	callback := paymentPath(paymentID, "/3ds/callback", token)

	forged := fake.Callback(ref, fake.StatusSuccess)
	forged.Set(fake.CallbackHash, strings.Repeat("0", 64))
	s.assertError(s.postForm(callback, forged), http.StatusUnauthorized, int(payment.Domain), payment.ErrorCallbackNotAuthentic.Code)
	s.assertError(s.postForm(callback, fake.Callback("3ds-404", fake.StatusSuccess)),
		http.StatusUnauthorized, int(payment.Domain), payment.ErrorCallbackNotAuthentic.Code)
	s.assertError(s.postForm(paymentPath(paymentID, "/3ds/callback", strings.Repeat("ab", 32)), fake.Callback(ref, fake.StatusSuccess)),
		http.StatusGone, int(payment.Domain), payment.ErrorConfirmTokenInvalid.Code)
	s.Empty(s.outboxPayloads(constants.PaymentSucceeded))

	done := s.postForm(callback, fake.Callback(ref, fake.StatusSuccess))
	s.Require().Equal(http.StatusFound, done.Code, done.Raw)
	s.Equal(frontURL+"/billing/confirm?payment="+paymentID+"&result=succeeded", done.Hdr.Get("Location"))
	again := s.postForm(callback, fake.Callback(ref, fake.StatusSuccess))
	s.Equal(http.StatusFound, again.Code)
	s.Equal(frontURL+"/billing/confirm?payment="+paymentID+"&result=succeeded", again.Hdr.Get("Location"))

	s.Equal("paid", s.onlyInvoice(teamID, owner.Token).str("status"))
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
	s.Equal(1, s.fake.Captures())
	s.Equal("succeeded", s.get(paymentPath(paymentID, "/confirm", token), "").str("status"))
	s.assertError(s.request(http.MethodPost, paymentPath(paymentID, "/3ds/start", token), "", ""),
		http.StatusConflict, int(payment.Domain), payment.ErrorNotAwaitingConfirmation.Code)
}

func (s *billingSuite) TestConfirm_TheCardholderFailing3DSFailsTheAttempt() {
	owner, teamID, paymentID, token := s.confirmationLink()
	started := s.request(http.MethodPost, paymentPath(paymentID, "/3ds/start", token), "", "")
	s.Require().Equal(http.StatusOK, started.Code, started.Raw)
	ref := strings.SplitN(strings.TrimPrefix(started.str("redirect_url"), fakeThreeDSPage), "?", 2)[0]

	done := s.postForm(paymentPath(paymentID, "/3ds/callback", token), fake.Callback(ref, fake.StatusFailure))

	s.Require().Equal(http.StatusFound, done.Code, done.Raw)
	s.Equal(frontURL+"/billing/confirm?payment="+paymentID+"&result=failed", done.Hdr.Get("Location"))
	s.Equal("payment_failed", s.onlyInvoice(teamID, owner.Token).str("status"))
	failures := s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 1)
	s.Equal("not_completed", failures[0]["failure_reason"])
	s.Equal(0, s.fake.Captures())
}

// The bank's step is a page to render or one to redirect to, never neither.
func (s *billingSuite) TestConfirm_TheBanksStepIsAPageOrARedirect() {
	_, _, paymentID, token := s.confirmationLink()
	page := "<!doctype html><html><body><form action=\"https://bank.test/acs\"></form></body></html>"
	s.fake.NextConfirmationPage(page, "")

	rendered := s.request(http.MethodPost, paymentPath(paymentID, "/3ds/start", token), "", "")

	s.Require().Equal(http.StatusOK, rendered.Code, rendered.Raw)
	s.Equal(page, rendered.str("html_content"), "the document itself, decoded")
	s.Nil(rendered.Body["redirect_url"])

	s.fake.NextConfirmationPage("", "")
	s.assertError(s.request(http.MethodPost, paymentPath(paymentID, "/3ds/start", token), "", ""),
		http.StatusBadGateway, int(payment.Domain), payment.ErrorProviderUnavailable.Code)
}

func (s *billingSuite) TestConfirm_ExpiredLinkIsGone() {
	owner, teamID, paymentID, token := s.confirmationLink()
	s.clock.Set(closingTime.Add(7*24*time.Hour + time.Second))

	s.assertError(s.get(paymentPath(paymentID, "/confirm", token), ""), http.StatusGone, int(payment.Domain), payment.ErrorConfirmTokenInvalid.Code)
	s.assertError(s.request(http.MethodPost, paymentPath(paymentID, "/3ds/start", token), "", ""),
		http.StatusGone, int(payment.Domain), payment.ErrorConfirmTokenInvalid.Code)
	s.assertError(s.get(paymentPath(uuid.NewString(), "/confirm", token), ""), http.StatusGone, int(payment.Domain), payment.ErrorConfirmTokenInvalid.Code)
	s.Equal("issued", s.onlyInvoice(teamID, owner.Token).str("status"))
	s.Empty(s.fake.Confirmations)
}
