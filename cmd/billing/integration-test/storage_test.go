package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/providers"
)

// storagePackage is a Package of the storage context's catalog as its events carry it.
type storagePackage struct {
	code  string
	bytes int64
	price string
}

var (
	storage10gb  = storagePackage{code: "10gb", bytes: 10_000_000_000, price: "1.00"}
	storage50gb  = storagePackage{code: "50gb", bytes: 50_000_000_000, price: "3.00"}
	storage250gb = storagePackage{code: "250gb", bytes: 250_000_000_000, price: "10.00"}
	storage1tb   = storagePackage{code: "1tb", bytes: 1_000_000_000_000, price: "35.00"}
)

// bucket is a Storage as the storage context names it in its events: its id,
// its Bucket name and the Project it lives in.
type bucket struct {
	id        uuid.UUID
	name      string
	projectID uuid.UUID
	project   string
}

func newBucket(name, project string) bucket {
	return bucket{id: uuid.New(), name: name, projectID: uuid.New(), project: project}
}

// storageStarted and storageStopped replay the storage context's Package events.
func (s *billingSuite) storageStarted(teamID uuid.UUID, b bucket, p storagePackage, at time.Time) {
	s.consumeStorage(constants.StoragePackageStarted, fmt.Sprintf(
		`{"payload":{"storage_id":%q,"team_id":%q,"project_id":%q,"project_name":%q,"bucket_name":%q,"region":"eu-central-1",`+
			`"package_code":%q,"size_bytes":%d,"monthly_price":%q,"started_at":%q}}`,
		b.id, teamID, b.projectID, b.project, b.name, p.code, p.bytes, p.price, stamp(at)))
}

func (s *billingSuite) storageStopped(teamID uuid.UUID, b bucket, startedAt, stoppedAt time.Time) {
	s.consumeStorage(constants.StoragePackageStopped, fmt.Sprintf(
		`{"payload":{"storage_id":%q,"team_id":%q,"started_at":%q,"stopped_at":%q}}`, b.id, teamID, stamp(startedAt), stamp(stoppedAt)))
}

// moveStorage is how storage moves a Storage to another Package: the interval
// that started at `since` stops and the new one starts at the same instant.
func (s *billingSuite) moveStorage(teamID uuid.UUID, b bucket, since time.Time, to storagePackage, at time.Time) {
	s.storageStopped(teamID, b, since, at)
	s.storageStarted(teamID, b, to, at)
}

func (s *billingSuite) consumeStorage(routingKey, payload string) {
	s.Require().NoError(s.storage.Consume(&rabbitmq.Message{RoutingKey: routingKey, Payload: []byte(payload)}))
}

func storageLine(b bucket, code, amount string) map[string]any {
	return map[string]any{
		"kind": "storage", "storage_id": b.id.String(), "bucket_name": b.name, "project_id": b.projectID.String(),
		"project_name": b.project, "package_code": code, "amount": amount,
	}
}

// The draft of the open Period carries one line per Storage at its Package's
// monthly price, with what the web app needs to word it.
func (s *billingSuite) TestStorage_TheDraftCarriesOneLinePerStorage() {
	owner, teamID := s.germanTeam()
	assets, backups := newBucket("shop-assets", "Shop"), newBucket("shop-backups", "Shop")
	backups.projectID = assets.projectID
	s.storageStarted(teamID, assets, storage50gb, oct(1, 0, 0))
	s.storageStarted(teamID, backups, storage250gb, oct(2, 0, 0))
	s.clock.Set(oct(20, 0, 0))

	draft := s.usageOf(teamID, owner.Token)

	s.Equal([]map[string]any{storageLine(assets, "50gb", "3.00"), storageLine(backups, "250gb", "10.00")},
		s.objectList(draft.Body["lines"]), "two Storages of one Project are two lines")
	s.Equal("13.00", draft.str("subtotal"))
}

// A Storage that moved to a larger Package pays the larger one's price once
// for the Period, and a Team with Storage alone is invoiced like any other.
func (s *billingSuite) TestStorage_ThePeriodChargesEachStorageItsHighestPrice() {
	owner, teamID := s.germanTeam()
	assets, backups := newBucket("shop-assets", "Shop"), newBucket("blog-backups", "Blog")
	s.storageStarted(teamID, assets, storage10gb, oct(1, 0, 0))
	s.moveStorage(teamID, assets, oct(1, 0, 0), storage250gb, oct(15, 12, 0))
	s.storageStarted(teamID, backups, storage50gb, oct(3, 0, 0))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal("period", detail.str("kind"))
	s.Equal([]map[string]any{storageLine(assets, "250gb", "10.00"), storageLine(backups, "50gb", "3.00")},
		s.objectList(detail.Body["lines"]), "one line per Storage, the moved one at the higher price only")
	s.Equal("13.00", detail.str("subtotal"))
	s.Equal("13.00", detail.str("total_due"))
	s.Equal("paid", detail.str("status"), "charged on issue like a Droplet Invoice")
	s.Require().Len(s.fake.Charges, 1)
	s.Equal("13.00", s.fake.Charges[0].Amount.StringFixed(2))
}

// A Threshold Invoice charges the Package held so far; a move to a larger
// Package after it leaves only the difference for the Period's Invoice.
func (s *billingSuite) TestStorage_AThresholdInvoiceThenALargerPackageChargesTheDifference() {
	owner, teamID := s.germanTeam()
	assets := newBucket("shop-assets", "Shop")
	s.storageStarted(teamID, assets, storage250gb, oct(1, 0, 0))
	s.droplet(teamID, "web-1", "0.5000", oct(1, 0, 0), at(oct(5, 4, 0)))

	s.checkThresholds(oct(5, 4, 0))

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1)
	s.Equal("threshold", invoices[0]["kind"])
	s.Equal("60.00", invoices[0]["total_due"], "100 hours at 0.50 and the Package's 10.00 reach the new Team's 50.00")
	thresholdLines := s.objectList(s.invoiceDetail(teamID, owner.Token, invoices[0]["id"].(string)).Body["lines"])
	s.Require().Len(thresholdLines, 2)
	s.Equal("droplet", thresholdLines[0]["kind"])
	s.Equal(storageLine(assets, "250gb", "10.00"), thresholdLines[1])

	s.moveStorage(teamID, assets, oct(1, 0, 0), storage1tb, oct(15, 0, 0))
	s.clock.Set(oct(20, 0, 0))
	draft := s.usageOf(teamID, owner.Token)
	s.Equal([]map[string]any{storageLine(assets, "1tb", "25.00")}, s.objectList(draft.Body["lines"]))
	s.Equal("25.00", draft.str("subtotal"))

	s.checkThresholds(oct(20, 0, 0))
	s.closePeriods(closingTime)

	invoices = s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 2, "25.00 unbilled stays under the Threshold")
	period := s.invoiceDetail(teamID, owner.Token, invoices[0]["id"].(string))
	s.Equal("period", period.str("kind"))
	s.Equal([]map[string]any{storageLine(assets, "1tb", "25.00")}, s.objectList(period.Body["lines"]),
		"35.00 less the 10.00 already charged; every Droplet hour was on the Threshold Invoice")
	s.Equal("25.00", period.str("total_due"))
}

// Storage alone crosses a Threshold like Droplet hours do.
func (s *billingSuite) TestStorage_PackagesAloneCrossTheThreshold() {
	owner, teamID := s.germanTeam()
	assets, backups := newBucket("shop-assets", "Shop"), newBucket("shop-backups", "Shop")
	s.storageStarted(teamID, assets, storage1tb, oct(2, 0, 0))

	s.checkThresholds(oct(3, 0, 0))
	s.Empty(s.invoices(teamID, owner.Token), "35.00 is under 50.00")

	s.storageStarted(teamID, backups, storage1tb, oct(4, 0, 0))
	s.checkThresholds(oct(5, 0, 0))

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1)
	s.Equal("threshold", invoices[0]["kind"])
	s.Equal("70.00", invoices[0]["total_due"])
	s.Equal([]map[string]any{storageLine(assets, "1tb", "35.00"), storageLine(backups, "1tb", "35.00")},
		s.objectList(s.invoiceDetail(teamID, owner.Token, invoices[0]["id"].(string)).Body["lines"]))

	s.closePeriods(closingTime)
	s.Len(s.invoices(teamID, owner.Token), 1, "the Period owes nothing more: no Period Invoice")
}

// A Storage opened on the 25th and one deleted on the 3rd each pay the whole
// month: a Package is never prorated.
func (s *billingSuite) TestStorage_OpenedLateOrDeletedEarlyIsChargedInFull() {
	owner, teamID := s.germanTeam()
	late, early := newBucket("late-assets", "Shop"), newBucket("early-assets", "Shop")
	s.storageStarted(teamID, late, storage50gb, oct(25, 18, 0))
	s.storageStarted(teamID, early, storage250gb, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	s.storageStopped(teamID, early, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), oct(3, 9, 0))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{storageLine(early, "250gb", "10.00"), storageLine(late, "50gb", "3.00")},
		s.objectList(detail.Body["lines"]), "the earlier interval first, each at its full price")
	s.Equal("13.00", detail.str("total_due"))
}

// A Storage whose Package stopped before the Period, deleted or ended by day
// 21 of Dunning, is not on the Period's Invoice.
func (s *billingSuite) TestStorage_NothingIsChargedAfterThePackageStopped() {
	owner, teamID := s.germanTeam()
	assets := newBucket("shop-assets", "Shop")
	s.storageStarted(teamID, assets, storage250gb, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	s.storageStopped(teamID, assets, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC))
	s.clock.Set(oct(20, 0, 0))

	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]))

	s.closePeriods(closingTime)
	s.Empty(s.invoices(teamID, owner.Token), "nothing was held in October")
}

// Day 21 of Dunning asks for the Team's Droplets to be deleted; storage
// deletes the Team's Storages then and ends their Packages. The Period it
// happened in was still held; the next one has no Storage line.
func (s *billingSuite) TestStorage_AfterDay21OfDunningTheNextPeriodHasNoStorageLine() {
	owner, teamID := s.germanTeam()
	assets, backups := newBucket("shop-assets", "Shop"), newBucket("blog-backups", "Blog")
	s.storageStarted(teamID, assets, storage250gb, oct(1, 0, 0))
	s.storageStarted(teamID, backups, storage50gb, oct(2, 0, 0))
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

	s.storageStopped(teamID, assets, oct(1, 0, 0), day(21))
	s.storageStopped(teamID, backups, oct(2, 0, 0), day(21))

	s.clock.Set(day(25))
	s.Equal([]map[string]any{storageLine(assets, "250gb", "10.00"), storageLine(backups, "50gb", "3.00")},
		s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "both Packages were held in November until day 21")

	s.clock.Set(time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC))
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "no Package is held in December")
	s.closePeriods(time.Date(2027, 1, 1, 0, 5, 0, 0, time.UTC))
	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1, "closing December issues nothing")
	s.Equal("2026-10-01T00:00:00Z", invoices[0]["period_start"])
}

// The broker may deliver an interval's stop before its start: the stop is
// kept, the late start fills in the price, code and names, and the stop
// survives it.
func (s *billingSuite) TestStorage_AStopBeforeItsStartStillMakesTheRightLine() {
	owner, teamID := s.germanTeam()
	assets := newBucket("shop-assets", "Shop")
	since := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	s.clock.Set(oct(20, 0, 0))

	s.storageStopped(teamID, assets, since, november)
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "a stop alone has no price to charge")

	s.storageStarted(teamID, assets, storage250gb, since)
	s.Equal([]map[string]any{storageLine(assets, "250gb", "10.00")}, s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]))

	s.closePeriods(closingTime)
	s.Equal("10.00", s.onlyInvoice(teamID, owner.Token).str("total_due"))

	s.clock.Set(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "the late start did not reopen the interval")
	s.closePeriods(december.Add(5 * time.Minute))
	s.Len(s.invoices(teamID, owner.Token), 1, "nothing was held in November")
}

// Redeliveries change nothing, and a replayed start never moves the price,
// the Package or the names an interval started with.
func (s *billingSuite) TestStorage_RedeliveredEventsChangeNothing() {
	owner, teamID := s.germanTeam()
	assets := newBucket("shop-assets", "Shop")
	for range 2 {
		s.storageStarted(teamID, assets, storage10gb, oct(1, 0, 0))
		s.moveStorage(teamID, assets, oct(1, 0, 0), storage250gb, oct(15, 0, 0))
	}
	renamed := assets
	renamed.project = "Shop EU"
	s.storageStarted(teamID, renamed, storage1tb, oct(15, 0, 0))
	s.clock.Set(oct(20, 0, 0))

	s.Equal([]map[string]any{storageLine(assets, "250gb", "10.00")}, s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]))

	s.closePeriods(closingTime)
	s.moveStorage(teamID, assets, oct(1, 0, 0), storage250gb, oct(15, 0, 0))
	s.closePeriods(closingTime.Add(10 * time.Minute))

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{storageLine(assets, "250gb", "10.00")}, s.objectList(detail.Body["lines"]))
	s.Equal("10.00", detail.str("total_due"))
	s.Len(s.fake.Charges, 1)
}

// Storage lines join the subtotal, so a Turkish Team pays KDV on them, Credit
// covers them and the TRY equivalent includes them, as for the other lines.
func (s *billingSuite) TestStorage_TaxCreditAndTheTRYEquivalentTreatStorageLikeTheRest() {
	owner, teamID := s.turkishTeam()
	s.storeRate(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), "41.6512")
	s.grantCredit(teamID, "10.00")
	assets := newBucket("magaza-dosyalar", "Mağaza")
	s.storageStarted(teamID, assets, storage1tb, oct(2, 0, 0))
	s.clock.Set(oct(20, 0, 0))

	estimate := s.usageOf(teamID, owner.Token)
	s.Equal("35.00", estimate.str("subtotal"))
	s.Equal("7.00", estimate.str("estimated_tax"))
	s.Equal("42.00", estimate.str("estimated_total"))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{storageLine(assets, "1tb", "35.00")}, s.objectList(detail.Body["lines"]))
	s.Equal("35.00", detail.str("subtotal"))
	s.Equal("7.00", detail.str("tax_total"))
	s.Equal("10.00", detail.str("credit_applied"))
	s.Equal("32.00", detail.str("total_due"))
	s.Equal("1332.84", detail.str("settlement_amount"), "32.00 × 41.6512 = 1332.8384")
	taxLines := s.objectList(detail.Body["tax_lines"])
	s.Require().Len(taxLines, 1)
	s.Equal("KDV %20", taxLines[0]["label"])
	s.Equal("35.00", taxLines[0]["base"])
	s.Equal("7.00", taxLines[0]["amount"])
	s.Require().Len(s.fake.Charges, 1)
	s.Equal("1332.84", s.fake.Charges[0].Amount.StringFixed(2))
	s.Equal("TRY", s.fake.Charges[0].Currency)

	statement := s.renderedFor(detail.str("number"))
	s.Contains(statement, `<tr><td>Storage · magaza-dosyalar · 1tb</td><td class="num">35.00</td></tr>`, "the Turkish statement names the line the same way")
	s.Contains(statement, "Storage paketi")
	s.NotContains(statement, "Saatlik fiyat", "no Droplet table without Droplet lines")
	s.NotContains(statement, "Mail", "no Mail table without Mail lines")
}

// The draft, the Invoice and its PDF list Droplet lines first, then the Mail
// lines, then one Storage line per Storage in the order their Packages
// started, each kind with its own fields only; what Droplet and Mail lines
// carried before Storage existed, they carry still.
func (s *billingSuite) TestStorage_TheInvoiceAndItsPDFShowAllThreeKindsOfLine() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	assets, backups := newBucket("shop-assets", "Shop"), newBucket("shop-backups", "Shop")
	assets.projectID, backups.projectID = shop, shop
	s.storageStarted(teamID, assets, storage250gb, oct(3, 0, 0))
	s.storageStarted(teamID, backups, storage50gb, oct(1, 0, 0))
	s.mailStarted(teamID, shop, "Shop", package10k, oct(4, 0, 0))
	dropletID := s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	want := []map[string]any{
		{"kind": "droplet", "droplet_id": dropletID.String(), "hostname": "web-1", "plan_code": "c1-small",
			"hours": float64(10), "hourly_price": "1.0000", "amount": "10.00"},
		mailLine(shop, "Shop", "10k", "9.00"),
		storageLine(backups, "50gb", "3.00"),
		storageLine(assets, "250gb", "10.00"),
	}
	s.clock.Set(oct(20, 0, 0))

	draft := s.usageOf(teamID, owner.Token)
	s.Equal(want, s.objectList(draft.Body["lines"]))
	s.Equal("32.00", draft.str("subtotal"))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal(want, s.objectList(detail.Body["lines"]))
	s.Equal("32.00", detail.str("subtotal"))
	statement := s.renderedFor(detail.str("number"))
	s.Contains(statement, `<tr><td>web-1</td><td>c1-small</td><td class="num">10</td><td class="num">1.0000</td><td class="num">10.00</td></tr>`)
	s.Contains(statement, `<tr><td>Mail · Shop · 10k</td><td class="num">9.00</td></tr>`)
	s.Contains(statement, `<tr><td>Storage · shop-backups · 50gb</td><td class="num">3.00</td></tr>`)
	s.Contains(statement, `<tr><td>Storage · shop-assets · 250gb</td><td class="num">10.00</td></tr>`)
	s.Contains(statement, "the largest Package the Storage held in the period")

	pdf := s.get(billingPath(teamID, "/invoices/"+detail.str("id")+"/pdf"), owner.Token)
	s.Require().Equal(http.StatusOK, pdf.Code, pdf.Raw)
	s.Contains(pdf.Raw, "Storage · shop-assets · 250gb", "the stored statement is the one served")
}

// A Team without Storage gets the Invoice it got before Storage existed, and
// its statement says nothing of Storage, whatever other Teams hold.
func (s *billingSuite) TestStorage_TeamsWithoutStorageAreUnchanged() {
	owner, teamID := s.germanTeam()
	shop := uuid.New()
	dropletID := s.droplet(teamID, "web-1", "1.0000", oct(10, 0, 0), at(oct(10, 10, 0)))
	s.mailStarted(teamID, shop, "Shop", package10k, oct(1, 0, 0))
	_, storageTeam := s.germanTeam()
	s.storageStarted(storageTeam, newBucket("other-assets", "Other"), storage1tb, oct(1, 0, 0))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal([]map[string]any{
		{"kind": "droplet", "droplet_id": dropletID.String(), "hostname": "web-1", "plan_code": "c1-small",
			"hours": float64(10), "hourly_price": "1.0000", "amount": "10.00"},
		mailLine(shop, "Shop", "10k", "9.00"),
	}, s.objectList(detail.Body["lines"]))
	s.Equal("19.00", detail.str("total_due"))
	s.NotContains(s.renderedFor(detail.str("number")), "Storage")
}

// The worker binds one billing.-prefixed queue per routing key of its
// consumers; a consumer missing here would leave storage's events unread.
func (s *billingSuite) TestStorage_TheWorkerBindsThePackageEvents() {
	var keys []string
	for _, source := range app.BuildConsumers(s.deps) {
		keys = append(keys, source.RoutingKeys()...)
	}

	s.Contains(keys, constants.StoragePackageStarted)
	s.Contains(keys, constants.StoragePackageStopped)
}

func (s *billingSuite) TestStorage_MalformedEventIsTerminal() {
	storage, project, team := uuid.NewString(), uuid.NewString(), uuid.NewString()
	started := func(fields string) *rabbitmq.Message {
		return rabbitMessage(constants.StoragePackageStarted, `{"payload":{"storage_id":"`+storage+`","team_id":"`+team+`",`+fields+`}}`)
	}
	for name, message := range map[string]*rabbitmq.Message{
		"not json": rabbitMessage(constants.StoragePackageStarted, `{"payload":`),
		"no valid price": started(`"project_id":"` + project + `","project_name":"Shop","bucket_name":"shop-assets","package_code":"250gb",` +
			`"size_bytes":250000000000,"monthly_price":"cheap","started_at":"2026-10-02T10:00:00Z"`),
		"a negative price": started(`"project_id":"` + project + `","project_name":"Shop","bucket_name":"shop-assets","package_code":"250gb",` +
			`"size_bytes":250000000000,"monthly_price":"-10.00","started_at":"2026-10-02T10:00:00Z"`),
		"no package": started(`"project_id":"` + project + `","project_name":"Shop","bucket_name":"shop-assets","package_code":" ",` +
			`"size_bytes":250000000000,"monthly_price":"10.00","started_at":"2026-10-02T10:00:00Z"`),
		"no bucket name": started(`"project_id":"` + project + `","project_name":"Shop","bucket_name":"","package_code":"250gb",` +
			`"size_bytes":250000000000,"monthly_price":"10.00","started_at":"2026-10-02T10:00:00Z"`),
		"no project": started(`"project_name":"Shop","bucket_name":"shop-assets","package_code":"250gb",` +
			`"size_bytes":250000000000,"monthly_price":"10.00","started_at":"2026-10-02T10:00:00Z"`),
		"no size": started(`"project_id":"` + project + `","project_name":"Shop","bucket_name":"shop-assets","package_code":"250gb",` +
			`"monthly_price":"10.00","started_at":"2026-10-02T10:00:00Z"`),
		"a stop without the start it closes": rabbitMessage(constants.StoragePackageStopped, `{"payload":{"storage_id":"`+storage+`","team_id":"`+team+
			`","stopped_at":"2026-10-02T10:00:00Z"}}`),
		"a stop before its start": rabbitMessage(constants.StoragePackageStopped, `{"payload":{"storage_id":"`+storage+`","team_id":"`+team+
			`","started_at":"2026-10-02T10:00:00Z","stopped_at":"2026-10-02T09:59:59Z"}}`),
	} {
		err := s.storage.Consume(message)
		s.ErrorIs(err, usage.ErrorMalformedEvent, name)
		var rerr rerror.Error
		s.Require().ErrorAs(err, &rerr, name)
		s.Equal(http.StatusBadRequest, rerr.HttpStatus, name)
	}
}
