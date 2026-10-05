package integrationtest

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/providers"
)

var december = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

// mailPackage is a Package of the mail context's catalog as its events carry it.
type mailPackage struct {
	code   string
	emails int
	price  string
}

var (
	package5k   = mailPackage{code: "5k", emails: 5_000, price: "5.00"}
	package10k  = mailPackage{code: "10k", emails: 10_000, price: "9.00"}
	package50k  = mailPackage{code: "50k", emails: 50_000, price: "29.00"}
	package100k = mailPackage{code: "100k", emails: 100_000, price: "49.00"}
)

// mailStarted and mailStopped replay the mail context's Package events.
func (s *billingSuite) mailStarted(teamID, projectID uuid.UUID, project string, p mailPackage, at time.Time) {
	s.consumeMail(constants.MailPackageStarted, fmt.Sprintf(
		`{"payload":{"project_id":%q,"team_id":%q,"project_name":%q,"package_code":%q,"monthly_emails":%d,"monthly_price":%q,"started_at":%q}}`,
		projectID, teamID, project, p.code, p.emails, p.price, stamp(at)))
}

func (s *billingSuite) mailStopped(teamID, projectID uuid.UUID, startedAt, stoppedAt time.Time) {
	s.consumeMail(constants.MailPackageStopped, fmt.Sprintf(
		`{"payload":{"project_id":%q,"team_id":%q,"started_at":%q,"stopped_at":%q}}`, projectID, teamID, stamp(startedAt), stamp(stoppedAt)))
}

// changePackage is how mail moves a Project to another Package: the interval
// that started at `since` stops and the new one starts at the same instant.
func (s *billingSuite) changePackage(teamID, projectID uuid.UUID, project string, since time.Time, to mailPackage, at time.Time) {
	s.mailStopped(teamID, projectID, since, at)
	s.mailStarted(teamID, projectID, project, to, at)
}

func (s *billingSuite) consumeMail(routingKey, payload string) {
	s.Require().NoError(s.mail.Consume(&rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload)}))
}

func mailLine(projectID uuid.UUID, project, code, amount string) map[string]any {
	return map[string]any{"kind": "mail", "project_id": projectID.String(), "project_name": project, "package_code": code, "amount": amount}
}

// renderedFor answers the statement HTML the fake renderer was given for an
// Invoice number.
func (s *billingSuite) renderedFor(number string) string {
	for _, html := range s.renderer.Rendered() {
		if strings.Contains(html, number) {
			return html
		}
	}
	s.Failf("no statement rendered", "for %s", number)

	return ""
}

// A Package held through the Period is one line at its monthly price, and a
// Team with Mail alone is invoiced like any other.
func (s *billingSuite) TestMail_APackageHeldTheWholePeriodIsOneLineAtItsPrice() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	s.mailStarted(teamID, shop, "Shop", package10k, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal("period", detail.str("kind"))
	s.Equal([]map[string]any{mailLine(shop, "Shop", "10k", "9.00")}, s.objectList(detail.Body["lines"]))
	s.Equal("9.00", detail.str("subtotal"))
	s.Equal("9.00", detail.str("total_due"))
	s.Equal("paid", detail.str("status"), "charged on issue like a Droplet Invoice")
	s.Require().Len(s.fake.Charges, 1)
	s.Equal("9.00", s.fake.Charges[0].Amount.StringFixed(2))
}

// An upgrade stops the old interval and starts the new one at the same
// instant; the Period pays the higher price once, not both.
func (s *billingSuite) TestMail_AnUpgradeMidPeriodChargesOnlyTheHigherPrice() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	s.mailStarted(teamID, shop, "Shop", package5k, oct(1, 0, 0))
	s.changePackage(teamID, shop, "Shop", oct(1, 0, 0), package50k, oct(15, 12, 0))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{mailLine(shop, "Shop", "50k", "29.00")}, s.objectList(detail.Body["lines"]), "one line per Project")
	s.Equal("29.00", detail.str("total_due"))
}

// A downgrade takes effect at the boundary instant: the old Period keeps the
// old price, the new one pays the new price.
func (s *billingSuite) TestMail_ADowngradeAtThePeriodBoundaryChargesEachPeriodItsOwnPrice() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	s.mailStarted(teamID, shop, "Shop", package50k, oct(3, 0, 0))
	s.changePackage(teamID, shop, "Shop", oct(3, 0, 0), package10k, november)

	s.closePeriods(closingTime)
	s.closePeriods(december.Add(5 * time.Minute))

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 2)
	novemberInvoice, octoberInvoice := invoices[0], invoices[1]
	s.Equal("2026-10-01T00:00:00Z", octoberInvoice["period_start"])
	s.Equal([]map[string]any{mailLine(shop, "Shop", "50k", "29.00")},
		s.objectList(s.invoiceDetail(teamID, owner.Token, octoberInvoice["id"].(string)).Body["lines"]))
	s.Equal("2026-11-01T00:00:00Z", novemberInvoice["period_start"])
	s.Equal([]map[string]any{mailLine(shop, "Shop", "10k", "9.00")},
		s.objectList(s.invoiceDetail(teamID, owner.Token, novemberInvoice["id"].(string)).Body["lines"]),
		"the larger Package stopped at November's first instant, so it was never held in November")
}

// A Threshold Invoice charges the Package held so far; an upgrade after it
// leaves only the difference for the Period's Invoice.
func (s *billingSuite) TestMail_AThresholdInvoiceThenAnUpgradeChargesTheDifference() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	s.mailStarted(teamID, shop, "Shop", package10k, oct(1, 0, 0))
	s.droplet(teamID, "web-1", "0.5000", oct(1, 0, 0), at(oct(5, 4, 0)))

	s.checkThresholds(oct(5, 4, 0))

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1)
	s.Equal("threshold", invoices[0]["kind"])
	s.Equal("59.00", invoices[0]["total_due"], "100 hours at 0.50 and the Package's 9.00 reach the new Team's 50.00")
	thresholdLines := s.objectList(s.invoiceDetail(teamID, owner.Token, invoices[0]["id"].(string)).Body["lines"])
	s.Require().Len(thresholdLines, 2)
	s.Equal("droplet", thresholdLines[0]["kind"])
	s.Equal(mailLine(shop, "Shop", "10k", "9.00"), thresholdLines[1])

	s.changePackage(teamID, shop, "Shop", oct(1, 0, 0), package50k, oct(15, 0, 0))
	s.clock.Set(oct(20, 0, 0))
	draft := s.usageOf(teamID, owner.Token)
	s.Equal([]map[string]any{mailLine(shop, "Shop", "50k", "20.00")}, s.objectList(draft.Body["lines"]))
	s.Equal("20.00", draft.str("subtotal"))

	s.checkThresholds(oct(20, 0, 0))
	s.closePeriods(closingTime)

	invoices = s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 2, "20.00 unbilled stays under the Threshold")
	period := s.invoiceDetail(teamID, owner.Token, invoices[0]["id"].(string))
	s.Equal("period", period.str("kind"))
	s.Equal([]map[string]any{mailLine(shop, "Shop", "50k", "20.00")}, s.objectList(period.Body["lines"]),
		"29.00 less the 9.00 already charged; every Droplet hour was on the Threshold Invoice")
	s.Equal("20.00", period.str("total_due"))
}

// Packages alone cross a Threshold like Droplet hours do.
func (s *billingSuite) TestMail_PackagesAloneCrossTheThreshold() {
	owner, teamID := s.germanTeam()
	shop, blog := uuid.New(), uuid.New()
	s.mailStarted(teamID, shop, "Shop", package100k, oct(2, 0, 0))

	s.checkThresholds(oct(3, 0, 0))
	s.Empty(s.invoices(teamID, owner.Token), "49.00 is under 50.00")

	s.mailStarted(teamID, blog, "Blog", package5k, oct(4, 0, 0))
	s.checkThresholds(oct(5, 0, 0))

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1)
	s.Equal("threshold", invoices[0]["kind"])
	s.Equal("54.00", invoices[0]["total_due"])
	s.Equal([]map[string]any{mailLine(shop, "Shop", "100k", "49.00"), mailLine(blog, "Blog", "5k", "5.00")},
		s.objectList(s.invoiceDetail(teamID, owner.Token, invoices[0]["id"].(string)).Body["lines"]))

	s.closePeriods(closingTime)
	s.Len(s.invoices(teamID, owner.Token), 1, "the Period owes nothing more: no Period Invoice")
}

// The broker may deliver an interval's stop before its start: the stop is
// kept, the late start fills in the price, code and name, and the stop
// survives it.
func (s *billingSuite) TestMail_AStopBeforeItsStartStillMakesTheRightLine() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	since := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	s.clock.Set(oct(20, 0, 0))

	s.mailStopped(teamID, shop, since, november)
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "a stop alone has no price to charge")

	s.mailStarted(teamID, shop, "Shop", package10k, since)
	s.Equal([]map[string]any{mailLine(shop, "Shop", "10k", "9.00")}, s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]))

	s.closePeriods(closingTime)
	s.Equal("9.00", s.onlyInvoice(teamID, owner.Token).str("total_due"))

	s.clock.Set(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "the late start did not reopen the interval")
	s.closePeriods(december.Add(5 * time.Minute))
	s.Len(s.invoices(teamID, owner.Token), 1, "nothing was held in November")
}

// Redeliveries change nothing, and a replayed start never moves the price,
// the Package or the name an interval started with.
func (s *billingSuite) TestMail_RedeliveredEventsChangeNothing() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	for range 2 {
		s.mailStarted(teamID, shop, "Shop", package5k, oct(1, 0, 0))
		s.changePackage(teamID, shop, "Shop", oct(1, 0, 0), package50k, oct(15, 0, 0))
	}
	s.mailStarted(teamID, shop, "Shop EU", package100k, oct(15, 0, 0))
	s.clock.Set(oct(20, 0, 0))

	s.Equal([]map[string]any{mailLine(shop, "Shop", "50k", "29.00")}, s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]))

	s.closePeriods(closingTime)
	s.changePackage(teamID, shop, "Shop", oct(1, 0, 0), package50k, oct(15, 0, 0))
	s.closePeriods(closingTime.Add(10 * time.Minute))

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{mailLine(shop, "Shop", "50k", "29.00")}, s.objectList(detail.Body["lines"]))
	s.Equal("29.00", detail.str("total_due"))
	s.Len(s.fake.Charges, 1)
}

// Day 21 of Dunning asks for the Team's Droplets to be deleted; mail ends
// every Package of the Team then. The Period it happened in was still held;
// the next one has no Mail line.
func (s *billingSuite) TestMail_AfterDay21OfDunningTheNextPeriodHasNoMailLine() {
	owner, teamID := s.germanTeam()
	shop, blog := uuid.New(), uuid.New()
	s.mailStarted(teamID, shop, "Shop", package10k, oct(1, 0, 0))
	s.mailStarted(teamID, blog, "Blog", package5k, oct(2, 0, 0))
	for range 4 {
		s.fake.NextChargeFails(providers.ReasonInsufficientFunds)
	}
	failedAt := closingTime
	day := func(n int) time.Time { return failedAt.AddDate(0, 0, n) }

	s.closePeriods(failedAt)
	s.Equal("payment_failed", s.onlyInvoice(teamID, owner.Token).str("status"))
	for _, n := range []int{1, 3, 7, 14, 20, 21} {
		s.runDunning(day(n))
	}
	deletions := s.outboxPayloads(constants.TeamDropletsDeletionRequested)
	s.Require().Len(deletions, 1)
	s.Equal(teamID.String(), deletions[0]["team_id"])

	s.mailStopped(teamID, shop, oct(1, 0, 0), day(21))
	s.mailStopped(teamID, blog, oct(2, 0, 0), day(21))

	s.clock.Set(day(25))
	s.Equal([]map[string]any{mailLine(shop, "Shop", "10k", "9.00"), mailLine(blog, "Blog", "5k", "5.00")},
		s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "both Packages were held in November until day 21")

	s.clock.Set(time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC))
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "no Package is held in December")
	s.closePeriods(time.Date(2027, 1, 1, 0, 5, 0, 0, time.UTC))
	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1, "closing December issues nothing")
	s.Equal("2026-10-01T00:00:00Z", invoices[0]["period_start"])
}

// Mail lines join the subtotal, so a Turkish Team pays KDV on them, Credit
// covers them and the TRY equivalent includes them, as for Droplet lines.
func (s *billingSuite) TestMail_TaxCreditAndTheTRYEquivalentTreatMailLikeDroplets() {
	owner, teamID := s.turkishTeam()
	s.storeRate(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), "41.6512")
	s.grantCredit(teamID, "10.00")
	shop := uuid.New()
	s.mailStarted(teamID, shop, "Mağaza", package50k, oct(2, 0, 0))
	s.clock.Set(oct(20, 0, 0))

	estimate := s.usageOf(teamID, owner.Token)
	s.Equal("29.00", estimate.str("subtotal"))
	s.Equal("5.80", estimate.str("estimated_tax"))
	s.Equal("34.80", estimate.str("estimated_total"))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{mailLine(shop, "Mağaza", "50k", "29.00")}, s.objectList(detail.Body["lines"]))
	s.Equal("29.00", detail.str("subtotal"))
	s.Equal("5.80", detail.str("tax_total"))
	s.Equal("10.00", detail.str("credit_applied"))
	s.Equal("24.80", detail.str("total_due"))
	s.Equal("1032.95", detail.str("settlement_amount"), "24.80 × 41.6512 = 1032.94976")
	taxLines := s.objectList(detail.Body["tax_lines"])
	s.Require().Len(taxLines, 1)
	s.Equal("KDV %20", taxLines[0]["label"])
	s.Equal("29.00", taxLines[0]["base"])
	s.Equal("5.80", taxLines[0]["amount"])
	s.Require().Len(s.fake.Charges, 1)
	s.Equal("1032.95", s.fake.Charges[0].Amount.StringFixed(2))
	s.Equal("TRY", s.fake.Charges[0].Currency)

	statement := s.renderedFor(detail.str("number"))
	s.Contains(statement, `<tr><td>Mail · Mağaza · 50k</td><td class="num">29.00</td></tr>`, "the Turkish statement names the line the same way")
	s.Contains(statement, "Mail paketi")
	s.NotContains(statement, "Saatlik fiyat", "no Droplet table without Droplet lines")
}

// A Team without Mail gets the Invoice it got before Mail existed: its lines
// gain only their kind, and its statement says nothing of Mail, whatever
// other Teams hold.
func (s *billingSuite) TestMail_DropletOnlyTeamsInvoicesAreUnchanged() {
	owner, teamID := s.germanTeam()
	dropletID := s.droplet(teamID, "web-1", "1.0000", oct(10, 0, 0), at(oct(10, 10, 0)))
	_, mailTeam := s.germanTeam()
	s.mailStarted(mailTeam, uuid.New(), "Shop", package10k, oct(1, 0, 0))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{{
		"kind": "droplet", "droplet_id": dropletID.String(), "hostname": "web-1", "plan_code": "c1-small",
		"hours": float64(10), "hourly_price": "1.0000", "amount": "10.00",
	}}, s.objectList(detail.Body["lines"]))
	s.Equal("10.00", detail.str("subtotal"))
	s.Equal("10.00", detail.str("total_due"))

	statement := s.renderedFor(detail.str("number"))
	s.Contains(statement, `<tr><td>web-1</td><td>c1-small</td><td class="num">10</td><td class="num">1.0000</td><td class="num">10.00</td></tr>`)
	s.Contains(statement, "Hours are counted from the moment a Droplet became active")
	s.NotContains(statement, "Mail")
}

// The draft, the Invoice and its PDF list Droplet lines first, then one
// Mail line per Project in the order their Packages started, each kind with
// its own fields only.
func (s *billingSuite) TestMail_TheInvoiceAndItsPDFShowTheMailLines() {
	owner, teamID := s.germanTeam()
	shop, blog := uuid.New(), uuid.New()
	s.mailStarted(teamID, shop, "Shop", package10k, oct(3, 0, 0))
	s.mailStarted(teamID, blog, "Blog", package5k, oct(1, 0, 0))
	dropletID := s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	want := []map[string]any{
		{"kind": "droplet", "droplet_id": dropletID.String(), "hostname": "web-1", "plan_code": "c1-small",
			"hours": float64(10), "hourly_price": "1.0000", "amount": "10.00"},
		mailLine(blog, "Blog", "5k", "5.00"),
		mailLine(shop, "Shop", "10k", "9.00"),
	}
	s.clock.Set(oct(20, 0, 0))

	draft := s.usageOf(teamID, owner.Token)
	s.Equal(want, s.objectList(draft.Body["lines"]))
	s.Equal("24.00", draft.str("subtotal"))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal(want, s.objectList(detail.Body["lines"]))
	s.Equal("24.00", detail.str("subtotal"))
	statement := s.renderedFor(detail.str("number"))
	s.Contains(statement, `<tr><td>Mail · Blog · 5k</td><td class="num">5.00</td></tr>`)
	s.Contains(statement, `<tr><td>Mail · Shop · 10k</td><td class="num">9.00</td></tr>`)
	s.Contains(statement, `<tr><td>web-1</td><td>c1-small</td>`)
	s.Contains(statement, "the largest Package the Project held in the period")

	pdf := s.get(billingPath(teamID, "/invoices/"+detail.str("id")+"/pdf"), owner.Token)
	s.Require().Equal(http.StatusOK, pdf.Code, pdf.Raw)
	s.Contains(pdf.Raw, "Mail · Shop · 10k", "the stored statement is the one served")
}

// The worker binds one billing.-prefixed queue per routing key of its
// consumers; a consumer missing here would leave mail's events unread.
func (s *billingSuite) TestMail_TheWorkerBindsThePackageEvents() {
	var keys []string
	for _, source := range app.BuildConsumers(s.deps) {
		keys = append(keys, source.RoutingKeys()...)
	}

	s.Contains(keys, constants.MailPackageStarted)
	s.Contains(keys, constants.MailPackageStopped)
}

func (s *billingSuite) TestMail_MalformedEventIsTerminal() {
	project, team := uuid.NewString(), uuid.NewString()
	for name, message := range map[string]*rabbitmq.Message{
		"not json": rabbitMessage(constants.MailPackageStarted, `{"payload":`),
		"no valid price": rabbitMessage(constants.MailPackageStarted, `{"payload":{"project_id":"`+project+`","team_id":"`+team+
			`","project_name":"Shop","package_code":"10k","monthly_emails":10000,"monthly_price":"cheap","started_at":"2026-10-02T10:00:00Z"}}`),
		"a negative price": rabbitMessage(constants.MailPackageStarted, `{"payload":{"project_id":"`+project+`","team_id":"`+team+
			`","project_name":"Shop","package_code":"10k","monthly_emails":10000,"monthly_price":"-9.00","started_at":"2026-10-02T10:00:00Z"}}`),
		"no package": rabbitMessage(constants.MailPackageStarted, `{"payload":{"project_id":"`+project+`","team_id":"`+team+
			`","project_name":"Shop","package_code":" ","monthly_emails":10000,"monthly_price":"9.00","started_at":"2026-10-02T10:00:00Z"}}`),
		"no emails": rabbitMessage(constants.MailPackageStarted, `{"payload":{"project_id":"`+project+`","team_id":"`+team+
			`","project_name":"Shop","package_code":"10k","monthly_price":"9.00","started_at":"2026-10-02T10:00:00Z"}}`),
		"a stop without the start it closes": rabbitMessage(constants.MailPackageStopped, `{"payload":{"project_id":"`+project+`","team_id":"`+team+
			`","stopped_at":"2026-10-02T10:00:00Z"}}`),
		"a stop before its start": rabbitMessage(constants.MailPackageStopped, `{"payload":{"project_id":"`+project+`","team_id":"`+team+
			`","started_at":"2026-10-02T10:00:00Z","stopped_at":"2026-10-02T09:59:59Z"}}`),
	} {
		s.ErrorIs(s.mail.Consume(message), usage.ErrorMalformedEvent, name)
	}
}
