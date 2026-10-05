package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/contexts/billing/aggregates/taxrule"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/contexts/billing/pdf"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const pdfKeyFormat = "invoices/%s/%s.pdf"

// Issue issues the Team's Invoice for the window and starts collecting it.
// It answers nil, without an error, when there is nothing to issue: a Period
// Invoice already exists, or the window holds no unbilled usage.
func (c *Collection) Issue(ctx context.Context, teamID uuid.UUID, kind invoice.Kind, period usage.Period, windowEnd, at time.Time) (*invoice.Invoice, error) {
	inv, err := c.issue(ctx, teamID, kind, period, windowEnd, at)
	if err != nil || inv == nil {
		return nil, err
	}
	c.afterIssue(ctx, inv, at)

	return inv, nil
}

// issue computes and persists the Invoice in one transaction. The Team's
// status row is locked first, so a Period close and a Threshold check never
// bill the same hours twice.
func (c *Collection) issue(ctx context.Context, teamID uuid.UUID, kind invoice.Kind, period usage.Period, windowEnd, at time.Time) (*invoice.Invoice, error) {
	var issued *invoice.Invoice
	err := inTransaction(ctx, c.Transactions, func(ctx context.Context) error {
		if _, err := c.Statuses.FindForUpdate(ctx, teamID, at); err != nil {
			return err
		}
		if kind == invoice.KindPeriod {
			exists, err := c.Invoices.PeriodInvoiceExists(ctx, teamID, period.Start)
			if err != nil || exists {
				return err
			}
		}

		d, err := c.draft(ctx, teamID, period, windowEnd)
		if err != nil {
			return err
		}
		if !d.Subtotal.IsPositive() {
			return nil
		}

		p, err := c.profileOf(ctx, teamID)
		if err != nil {
			return err
		}
		rule, err := c.taxRuleFor(ctx, p, at)
		if err != nil {
			return err
		}
		credit, err := c.Invoices.CreditBalance(ctx, teamID)
		if err != nil {
			return err
		}
		tax := rule.Amount(d.Subtotal)
		totals := invoice.ComputeTotals(d.Subtotal, tax, credit)

		inv := &invoice.Invoice{
			TeamID: teamID, Kind: kind, Status: invoice.StatusIssued,
			PeriodStart: period.Start, PeriodEnd: period.End, WindowEnd: minTime(period.End, windowEnd),
			Currency: invoice.Currency, Subtotal: totals.Subtotal, TaxTotal: totals.TaxTotal,
			CreditApplied: totals.CreditApplied, TotalDue: totals.TotalDue,
			Locale: invoiceLocale(p), Profile: snapshot(p), IssuedAt: at, Lines: d.Lines,
			TaxLines: []invoice.TaxLine{{
				LabelKey: rule.LabelKey, Label: taxrule.Label(rule.LabelKey, invoiceLocale(p)),
				RateBps: rule.RateBps, Base: d.Subtotal, Amount: tax,
			}},
		}
		// Credit that covers everything pays the Invoice on the spot.
		if !totals.TotalDue.IsPositive() {
			inv.Status, inv.PaidAt = invoice.StatusPaid, &at
		}
		if p != nil && p.Country == profile.CountryTR {
			if err := c.settleInTRY(ctx, inv, at); err != nil {
				return err
			}
		}
		if err := c.Invoices.Issue(ctx, inv); err != nil {
			return err
		}
		issued = inv

		return nil
	})

	return issued, err
}

// settleInTRY fixes the TRY equivalent with the latest central-bank
// bulletin dated before the issue date: the bank publishes in the afternoon
// the rates that apply from the next day, so the rule does not depend on the
// hour an Invoice is issued. To be confirmed with the accountant. A later
// collection never changes it (ADR-0006).
func (c *Collection) settleInTRY(ctx context.Context, inv *invoice.Invoice, at time.Time) error {
	rate, err := c.ExchangeRates.RateBefore(ctx, at)
	if err != nil {
		return err
	}
	if rate.IsFallbackFor(at.AddDate(0, 0, -1)) {
		c.Log.InfoWithFields("invoice settled with an earlier exchange rate", logger.NewFields().
			Add("team", inv.TeamID.String()).Add("rate_date", rate.RateDate.Format(time.DateOnly)))
	}
	currency, amount, rateDate := settlementCurrencyTR, invoice.Settlement(inv.TotalDue, rate.USDBuying), rate.RateDate
	inv.SettlementCurrency, inv.SettlementAmount = &currency, &amount
	inv.ExchangeRate, inv.ExchangeRateDate = &rate.USDBuying, &rateDate

	return nil
}

// afterIssue runs once the Invoice is committed: the PDF, the e-invoice
// issuer, the email and the first charge. A failure is logged; the PDF is
// rendered again on its first download, and the sweep sends an email that
// never went out and charges an Invoice nobody charged.
func (c *Collection) afterIssue(ctx context.Context, inv *invoice.Invoice, at time.Time) {
	fields := func() logger.Fields { return logger.NewFields().Add("invoice", inv.ID.String()) }

	if _, err := c.renderPDF(ctx, inv); err != nil {
		c.logFailure(err, "render invoice pdf", fields())
	}
	if ref, err := c.EInvoice.Issue(ctx, inv); err != nil {
		c.logFailure(err, "issue e-invoice", fields())
	} else if str.IsNotEmpty(ref) {
		if err := c.Invoices.SetEInvoiceRef(ctx, inv.ID, ref); err != nil {
			c.logFailure(err, "store e-invoice reference", fields())
		}
	}
	if err := c.notifyIssued(ctx, inv, at); err != nil {
		c.logFailure(err, "publish invoice issued", fields())
	} else if err := c.Invoices.SetNotified(ctx, inv.ID, at); err != nil {
		c.logFailure(err, "record invoice issued email", fields())
	}
	if inv.Status != invoice.StatusIssued {
		return
	}
	// A charge that fails to start is left to the sweep, which collects any
	// issued Invoice without an attempt in progress.
	if _, err := c.charge(ctx, inv.ID, nil, payment.TriggerIssue, at); err != nil {
		c.logFailure(err, "charge issued invoice", fields())
	}
}

func (c *Collection) notifyIssued(ctx context.Context, inv *invoice.Invoice, at time.Time) error {
	address, err := c.invoiceAddress(ctx, inv.TeamID)
	if err != nil {
		return err
	}
	body := events.InvoiceIssuedBody{
		TeamID: inv.TeamID, InvoiceID: inv.ID, InvoiceAddress: address, InvoiceNumber: inv.Number,
		PeriodStart: inv.PeriodStart.UTC(), PeriodEnd: inv.PeriodEnd.UTC(),
		TotalDue: inv.TotalDue.StringFixed(2), Currency: inv.Currency, PDFURL: c.pdfURL(inv.ID, at),
	}
	if inv.SettlementCurrency != nil && inv.SettlementAmount != nil {
		amount, currency := inv.SettlementAmount.StringFixed(2), *inv.SettlementCurrency
		body.SettlementAmount, body.SettlementCurr = &amount, &currency
	}

	return c.Notifier.InvoiceIssued(ctx, body)
}

// PDF answers the Invoice's stored PDF, rendering it first when the render
// after issue did not succeed.
func (c *Collection) PDF(ctx context.Context, inv *invoice.Invoice) ([]byte, error) {
	if inv.PDFKey != nil {
		data, err := c.Store.Get(ctx, *inv.PDFKey)
		if err == nil {
			return data, nil
		}
		c.logFailure(err, "read stored invoice pdf", logger.NewFields().Add("invoice", inv.ID.String()))
	}

	data, err := c.renderPDF(ctx, inv)
	if err != nil {
		c.logFailure(err, "render invoice pdf on download", logger.NewFields().Add("invoice", inv.ID.String()))

		return nil, invoice.ErrorPDFUnavailable
	}

	return data, nil
}

func (c *Collection) renderPDF(ctx context.Context, inv *invoice.Invoice) ([]byte, error) {
	team, err := c.Access.FindTeam(ctx, inv.TeamID)
	if err != nil {
		return nil, err
	}
	teamName := ""
	if team != nil {
		teamName = team.Name
	}

	html, err := pdf.HTML(statementOf(inv, teamName))
	if err != nil {
		return nil, err
	}
	data, err := c.Renderer.Render(ctx, html)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf(pdfKeyFormat, inv.TeamID, inv.ID)
	if err := c.Store.Put(ctx, key, data); err != nil {
		return nil, err
	}
	if err := c.Invoices.SetPDFKey(ctx, inv.ID, key); err != nil {
		return nil, err
	}
	inv.PDFKey = &key

	return data, nil
}

func statementOf(inv *invoice.Invoice, teamName string) *pdf.Statement {
	s := &pdf.Statement{
		Locale: inv.Locale, Number: inv.Number, Threshold: inv.Kind == invoice.KindThreshold,
		IssuedAt: inv.IssuedAt, PeriodStart: inv.PeriodStart, PeriodEnd: inv.PeriodEnd.Add(-time.Second), TeamName: teamName,
		Payer: pdf.Payer{
			LegalName: inv.Profile.LegalName, Company: inv.Profile.AccountType == string(profile.AccountCompany),
			AddressLine1: inv.Profile.AddressLine1, AddressLine2: inv.Profile.AddressLine2, City: inv.Profile.City,
			PostalCode: inv.Profile.PostalCode, Country: inv.Profile.Country, TaxID: inv.Profile.TaxID,
			TaxOffice: inv.Profile.TaxOffice, VATID: inv.Profile.VATID, InvoiceEmail: inv.Profile.InvoiceEmail,
		},
		Currency: inv.Currency, Subtotal: inv.Subtotal.StringFixed(2), Credit: inv.CreditApplied.StringFixed(2),
		HasCredit: inv.CreditApplied.IsPositive(), TotalDue: inv.TotalDue.StringFixed(2),
	}
	for _, l := range inv.Lines {
		if l.Kind == invoice.LineMail {
			s.MailLines = append(s.MailLines, pdf.MailLine{ProjectName: l.ProjectName, PackageCode: l.PackageCode, Amount: l.Amount.StringFixed(2)})

			continue
		}
		s.DropletLines = append(s.DropletLines, pdf.DropletLine{Hostname: l.Hostname, PlanCode: l.PlanCode, Hours: l.Hours,
			HourlyPrice: l.HourlyPrice.StringFixed(4), Amount: l.Amount.StringFixed(2)})
	}
	for _, t := range inv.TaxLines {
		s.TaxLines = append(s.TaxLines, pdf.TaxLine{Label: t.Label, RateBps: t.RateBps, Base: t.Base.StringFixed(2), Amount: t.Amount.StringFixed(2)})
	}
	if inv.SettlementCurrency != nil && inv.SettlementAmount != nil && inv.ExchangeRate != nil && inv.ExchangeRateDate != nil {
		s.Settlement = &pdf.Settlement{Currency: *inv.SettlementCurrency, Amount: inv.SettlementAmount.StringFixed(2),
			Rate: inv.ExchangeRate.StringFixed(4), RateDate: *inv.ExchangeRateDate}
	}

	return s
}

func invoiceLocale(p *profile.Profile) string {
	if p == nil {
		return localeFor("")
	}

	return localeFor(p.Country)
}

func snapshot(p *profile.Profile) invoice.ProfileSnapshot {
	if p == nil {
		return invoice.ProfileSnapshot{}
	}

	return invoice.ProfileSnapshot{
		AccountType: string(p.AccountType), LegalName: p.LegalName, Country: p.Country,
		AddressLine1: p.AddressLine1, AddressLine2: p.AddressLine2, City: p.City, PostalCode: p.PostalCode,
		TaxID: p.TaxID, TaxOffice: p.TaxOffice, VATID: p.VATID, InvoiceEmail: p.InvoiceEmail,
	}
}
