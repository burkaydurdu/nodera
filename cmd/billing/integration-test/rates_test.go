package integrationtest

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
	"github.com/piggystats/nodera/contexts/billing/aggregates/taxrule"
)

var (
	friday   = time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	saturday = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	monday   = time.Date(2026, 10, 5, 16, 0, 0, 0, time.UTC)
)

// The seed's two rules: 20% KDV for every Turkish Team, the export-service
// exemption for everyone else.
func (s *billingSuite) TestTaxRules_SeedAppliesByCountry() {
	ctx := context.Background()
	at := time.Date(2026, 10, 31, 23, 59, 0, 0, time.UTC)

	for _, accountType := range []string{"company", "individual"} {
		rule, err := s.services.TaxRules.Lookup(ctx, "TR", accountType, at)
		s.Require().NoError(err)
		s.Equal(2000, rule.RateBps, accountType)
		s.Equal("tax.kdv_20", rule.LabelKey)
		s.Equal("24.00", rule.Amount(mustDecimal("120.00")).StringFixed(2))
	}

	for _, country := range []string{"DE", "US"} {
		rule, err := s.services.TaxRules.Lookup(ctx, country, "company", at)
		s.Require().NoError(err)
		s.Equal(0, rule.RateBps, country)
		s.Equal("tax.export_service_exemption", rule.LabelKey)
		s.True(rule.Amount(mustDecimal("120.00")).IsZero())
	}
}

// An operator row naming the account type beats the country-wide one, but
// only from its valid-from on.
func (s *billingSuite) TestTaxRules_MostSpecificValidRuleWins() {
	ctx := context.Background()
	validFrom := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.pgPool.Exec(ctx, `INSERT INTO tax_rules (country, account_type, rate_bps, label_key, valid_from) VALUES ('TR', 'individual', 1000, 'tax.kdv_10', $1)`, validFrom)
	s.Require().NoError(err)

	before, err := s.services.TaxRules.Lookup(ctx, "TR", "individual", validFrom.Add(-time.Second))
	s.Require().NoError(err)
	s.Equal(2000, before.RateBps)

	after, err := s.services.TaxRules.Lookup(ctx, "TR", "individual", validFrom)
	s.Require().NoError(err)
	s.Equal(1000, after.RateBps)
	s.Equal("tax.kdv_10", after.LabelKey)

	company, err := s.services.TaxRules.Lookup(ctx, "TR", "company", validFrom)
	s.Require().NoError(err)
	s.Equal(2000, company.RateBps, "the company stays on the country-wide rule")
}

func (s *billingSuite) TestTaxRules_NothingValidYetIsAnError() {
	_, err := s.services.TaxRules.Lookup(context.Background(), "TR", "company", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	s.ErrorIs(err, taxrule.ErrorNoRule)
}

func (s *billingSuite) TestExchangeRate_FetchStoresTheBulletinOnce() {
	ctx := context.Background()
	s.clock.Set(friday)
	s.rates.script(friday, "41.6512")
	calls := s.rates.callCount()

	s.Require().NoError(s.jobs.FetchExchangeRate(ctx))
	s.Require().NoError(s.jobs.FetchExchangeRate(ctx))

	s.Equal(calls+1, s.rates.callCount(), "a covered day is not fetched again")
	rate, err := s.services.ExchangeRates.RateFor(ctx, friday)
	s.Require().NoError(err)
	s.Equal("41.6512", rate.USDBuying.String())
	s.False(rate.IsFallbackFor(friday))
}

// The bulletin is published in the afternoon: until it is, the job stores
// yesterday's again and keeps asking.
func (s *billingSuite) TestExchangeRate_YesterdaysBulletinKeepsTheJobAsking() {
	ctx := context.Background()
	s.clock.Set(monday)
	s.rates.script(friday, "41.6512")
	calls := s.rates.callCount()

	s.Require().NoError(s.jobs.FetchExchangeRate(ctx))
	s.Require().NoError(s.jobs.FetchExchangeRate(ctx))
	s.Equal(calls+2, s.rates.callCount())

	s.rates.script(monday, "41.8000")
	s.Require().NoError(s.jobs.FetchExchangeRate(ctx))
	s.Require().NoError(s.jobs.FetchExchangeRate(ctx))
	s.Equal(calls+3, s.rates.callCount())

	rate, err := s.services.ExchangeRates.RateFor(ctx, monday)
	s.Require().NoError(err)
	s.Equal("41.8", rate.USDBuying.String())
}

func (s *billingSuite) TestExchangeRate_WeekendFallsBackToTheLatestEarlierRate() {
	ctx := context.Background()
	s.Require().NoError(s.services.ExchangeRates.Store(ctx, &exchangerate.Rate{RateDate: friday, USDBuying: mustDecimal("41.6512"), Source: exchangerate.SourceTCMB}))

	rate, err := s.services.ExchangeRates.RateFor(ctx, saturday)

	s.Require().NoError(err)
	s.Equal(exchangerate.Day(friday), rate.RateDate)
	s.Equal("41.6512", rate.USDBuying.String())
	s.True(rate.IsFallbackFor(saturday))

	_, err = s.services.ExchangeRates.RateFor(ctx, friday.AddDate(0, 0, -1))
	s.ErrorIs(err, exchangerate.ErrorNoRate, "nothing on or before the day")
}

func (s *billingSuite) TestExchangeRate_FetchFailureIsReported() {
	s.clock.Set(friday)
	s.rates.mu.Lock()
	s.rates.err = exchangerate.ErrorFetchFailed
	s.rates.mu.Unlock()
	defer s.rates.script(friday, "41.6512")

	s.ErrorIs(s.jobs.FetchExchangeRate(context.Background()), exchangerate.ErrorFetchFailed)
}
