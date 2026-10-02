package integrationtest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
)

// October 2026 is the Period most scenarios bill; closing it runs a few
// minutes into November, as the worker would.
var (
	october     = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	november    = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	closingTime = november.Add(5 * time.Minute)
)

func oct(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// started and stopped replay the droplet context's usage events.
func (s *billingSuite) started(teamID, dropletID uuid.UUID, hostname, price string, at time.Time) {
	s.consumeUsage(constants.DropletBillingStarted, fmt.Sprintf(
		`{"payload":{"droplet_id":%q,"team_id":%q,"project_id":%q,"hostname":%q,"plan_code":"c1-small","hourly_price":%q,"started_at":%q}}`,
		dropletID, teamID, uuid.New(), hostname, price, stamp(at)))
}

func (s *billingSuite) stopped(teamID, dropletID uuid.UUID, at time.Time) {
	s.consumeUsage(constants.DropletBillingStopped, fmt.Sprintf(`{"payload":{"droplet_id":%q,"team_id":%q,"stopped_at":%q}}`,
		dropletID, teamID, stamp(at)))
}

// droplet starts a Droplet and, when stop is set, stops it.
func (s *billingSuite) droplet(teamID uuid.UUID, hostname, price string, start time.Time, stop *time.Time) uuid.UUID {
	id := uuid.New()
	s.started(teamID, id, hostname, price, start)
	if stop != nil {
		s.stopped(teamID, id, *stop)
	}

	return id
}

func (s *billingSuite) consumeUsage(routingKey, payload string) {
	s.Require().NoError(s.usage.Consume(&rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload)}))
}

func at(t time.Time) *time.Time { return &t }

// germanTeam is a Team with a German company profile and a USD card.
func (s *billingSuite) germanTeam() (user, uuid.UUID) {
	owner := s.signIn()
	teamID := s.team(owner)
	s.Require().Equal(http.StatusOK, s.putProfile(teamID, owner.Token, deCompany()).Code)
	s.addCard(teamID, owner.Token)

	return owner, teamID
}

// turkishTeam is a Team with a Turkish company profile and a TRY card.
func (s *billingSuite) turkishTeam() (user, uuid.UUID) {
	owner, teamID := s.payingTeam()
	s.addCard(teamID, owner.Token)

	return owner, teamID
}

// teamCreatedAt makes a Team known as created at `created`, for the
// Threshold's age.
func (s *billingSuite) teamCreatedAt(owner user, created time.Time) uuid.UUID {
	teamID := uuid.New()
	s.consume(constants.TeamCreated, fmt.Sprintf(`{"payload":{"team_id":%q,"owner_user_id":%q,"name":"Veteran","owner_email":%q,"owner_full_name":"Owner Person","owner_locale":"en","created_at":%q}}`,
		teamID, owner.ID, owner.Email, stamp(created)))

	return teamID
}

func (s *billingSuite) storeRate(day time.Time, rate string) {
	s.Require().NoError(s.services.ExchangeRates.Store(context.Background(),
		&exchangerate.Rate{RateDate: exchangerate.Day(day), USDBuying: mustDecimal(rate), Source: exchangerate.SourceTCMB}))
}

func (s *billingSuite) grantCredit(teamID uuid.UUID, amount string) {
	s.Require().NoError(s.services.Invoices.GrantCredit(context.Background(), teamID, mustDecimal(amount), "test grant"))
}

func (s *billingSuite) closePeriods(now time.Time) {
	s.clock.Set(now)
	s.Require().NoError(s.jobs.ClosePeriods(context.Background()))
}

func (s *billingSuite) checkThresholds(now time.Time) {
	s.clock.Set(now)
	s.Require().NoError(s.jobs.CheckThresholds(context.Background()))
}

func (s *billingSuite) runDunning(now time.Time) {
	s.clock.Set(now)
	s.Require().NoError(s.jobs.RunDunning(context.Background()))
}

func (s *billingSuite) invoices(teamID uuid.UUID, token string) []map[string]any {
	res := s.get(billingPath(teamID, "/invoices"), token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return res.List
}

// onlyInvoice answers the detail of the Team's single Invoice.
func (s *billingSuite) onlyInvoice(teamID uuid.UUID, token string) *response {
	list := s.invoices(teamID, token)
	s.Require().Len(list, 1)
	res := s.get(billingPath(teamID, "/invoices/"+list[0]["id"].(string)), token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return res
}

func (s *billingSuite) invoiceDetail(teamID uuid.UUID, token, invoiceID string) *response {
	res := s.get(billingPath(teamID, "/invoices/"+invoiceID), token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return res
}

func (s *billingSuite) pay(teamID uuid.UUID, token, invoiceID, body string) *response {
	return s.request(http.MethodPost, billingPath(teamID, "/invoices/"+invoiceID+"/pay"), body, token)
}

func recipientEmails(payload map[string]any) []string {
	list, _ := payload["recipients"].([]any)
	out := make([]string, 0, len(list))
	for _, r := range list {
		m, _ := r.(map[string]any)
		email, _ := m["email"].(string)
		out = append(out, email)
	}

	return out
}

func (s *billingSuite) objectList(v any) []map[string]any {
	list, ok := v.([]any)
	s.Require().True(ok, "%v is not a list", v)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		out = append(out, item.(map[string]any))
	}

	return out
}

// pathOf strips the origin off a link the service built, so the suite can
// call it on the in-process server.
// signedPDFPath turns the emailed web-app link into the API call that page
// makes, the way the web app does.
func (s *billingSuite) signedPDFPath(link string) string {
	s.Require().True(strings.HasPrefix(link, frontURL+"/billing/invoice-pdf?"), link)
	parsed, err := url.Parse(link)
	s.Require().NoError(err)
	q := parsed.Query()

	return "/v1/billing/invoices/" + q.Get("invoice") + "/pdf?expires=" + q.Get("expires") + "&token=" + url.QueryEscape(q.Get("token"))
}

func (s *billingSuite) queryParam(link, key string) string {
	parsed, err := url.Parse(link)
	s.Require().NoError(err)

	return parsed.Query().Get(key)
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// cardRef answers the provider reference of the Team's default card.
func (s *billingSuite) cardRef(teamID uuid.UUID) string {
	var ref string
	s.Require().NoError(s.pgPool.QueryRow(context.Background(),
		`SELECT provider_card_ref FROM payment_methods WHERE team_id = $1 AND is_default AND deleted_at IS NULL`, teamID).Scan(&ref))

	return ref
}

// statusTimesIncrease holds that every TeamBillingStatusChanged of the Team
// carries a later changed_at than the one before, as the droplet context's
// last-writer-wins read model needs.
func (s *billingSuite) statusTimesIncrease(teamID uuid.UUID) {
	s.T().Helper()
	var previous time.Time
	for _, p := range s.outboxPayloads(constants.TeamBillingStatusChanged) {
		if p["team_id"] != teamID.String() {
			continue
		}
		changedAt, err := time.Parse(time.RFC3339Nano, p["changed_at"].(string))
		s.Require().NoError(err)
		s.True(changedAt.After(previous), "%s after %s", changedAt, previous)
		previous = changedAt
	}
}

func (s *billingSuite) sweep(now time.Time) {
	s.clock.Set(now)
	s.Require().NoError(s.jobs.SweepInvoices(context.Background()))
}

func (s *billingSuite) reconcile(now time.Time) {
	s.clock.Set(now)
	s.Require().NoError(s.jobs.ReconcilePayments(context.Background()))
}

// onlyPayment answers the Team's single Invoice's attempts.
func (s *billingSuite) attempts(teamID uuid.UUID, token string) []map[string]any {
	return s.objectList(s.onlyInvoice(teamID, token).Body["payments"])
}
