package integrationtest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
)

// The PDF goes through the real Gotenberg container: issued, stored, served
// to the Team, and served signed-out through the issue email's link.
func (s *billingSuite) TestPDF_RenderedThroughGotenbergAndDownloadable() {
	deps := s.deps
	deps.Renderer = nil
	server := s.server
	s.server = app.BuildServer(deps, 1_000_000)
	defer func() { s.server = server }()
	gotenbergJobs := app.BuildJobs(deps)

	owner, teamID := s.turkishTeam()
	s.storeRate(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), "41.6512")
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(9, 4, 0)))
	s.clock.Set(closingTime)
	s.Require().NoError(gotenbergJobs.ClosePeriods(context.Background()))

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal(true, detail.Body["pdf_available"])
	s.Empty(s.renderer.Rendered(), "the fake renderer was not used")

	res := s.get(billingPath(teamID, "/invoices/"+detail.str("id")+"/pdf"), owner.Token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal("application/pdf", res.Hdr.Get("Content-Type"))
	s.True(strings.HasPrefix(res.Raw, "%PDF"), "a PDF: %.20q", res.Raw)
	s.Contains(res.Hdr.Get("Content-Disposition"), detail.str("number")+".pdf")

	link := s.outboxPayloads(constants.InvoiceIssued)[0]["pdf_url"].(string)
	signed := s.get(s.signedPDFPath(link), "")
	s.Require().Equal(http.StatusOK, signed.Code, signed.Raw)
	s.True(strings.HasPrefix(signed.Raw, "%PDF"))
	s.Equal(res.Raw, signed.Raw, "both routes serve the stored document")
}

func (s *billingSuite) TestPDF_SignedLinkIsBoundToItsInvoiceAndExpiry() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	otherOwner, otherTeam := s.germanTeam()
	s.droplet(otherTeam, "web-2", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.closePeriods(closingTime)
	id := s.invoices(teamID, owner.Token)[0]["id"].(string)
	otherID := s.invoices(otherTeam, otherOwner.Token)[0]["id"].(string)
	var link string
	for _, p := range s.outboxPayloads(constants.InvoiceIssued) {
		if strings.Contains(p["pdf_url"].(string), id) {
			link = s.signedPDFPath(p["pdf_url"].(string))
		}
	}
	s.Require().NotEmpty(link)

	s.Equal(http.StatusOK, s.get(link, "").Code)
	s.assertError(s.get(strings.Replace(link, id, otherID, 1), ""), http.StatusGone, int(invoice.Domain), invoice.ErrorLinkInvalid.Code)
	expires := s.queryParam("http://billing"+link, "expires")
	s.assertError(s.get(strings.Replace(link, "expires="+expires, "expires=9999999999", 1), ""), http.StatusGone, int(invoice.Domain), invoice.ErrorLinkInvalid.Code)

	s.clock.Set(closingTime.Add(31 * 24 * time.Hour))
	s.assertError(s.get(link, ""), http.StatusGone, int(invoice.Domain), invoice.ErrorLinkInvalid.Code)
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/invoices/"+id+"/pdf"), owner.Token).Code, "the session route never expires")
}

// A renderer that is down at issue time does not hold the Invoice back; the
// first download renders it.
func (s *billingSuite) TestPDF_RenderFailureIsRetriedOnDownload() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.renderer.FailWith(errors.New("gotenberg: connection refused"))
	s.closePeriods(closingTime)
	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal(false, detail.Body["pdf_available"])
	s.Equal("paid", detail.str("status"), "collection went on")
	path := billingPath(teamID, "/invoices/"+detail.str("id")+"/pdf")

	s.assertError(s.get(path, owner.Token), http.StatusServiceUnavailable, int(invoice.Domain), invoice.ErrorPDFUnavailable.Code)

	s.renderer.FailWith(nil)
	res := s.get(path, owner.Token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	s.True(strings.HasPrefix(res.Raw, "%PDF"))
	s.Equal(true, s.onlyInvoice(teamID, owner.Token).Body["pdf_available"])
	s.Contains(res.Raw, "statement of account, not a tax invoice", "a German Team's statement is in English")
	s.Contains(res.Raw, "Export-service exemption")
}
