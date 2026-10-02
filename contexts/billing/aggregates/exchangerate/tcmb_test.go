package exchangerate_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
)

// A trimmed copy of the bulletin's real shape.
const bulletin = `<?xml version="1.0" encoding="UTF-8"?>
<Tarih_Date Tarih="01.10.2026" Date="10/01/2026" Bulten_No="2026/185">
  <Currency CrossOrder="0" Kod="USD" CurrencyCode="USD">
    <Unit>1</Unit><Isim>ABD DOLARI</Isim><CurrencyName>US DOLLAR</CurrencyName>
    <ForexBuying>41.6512</ForexBuying><ForexSelling>41.7262</ForexSelling>
  </Currency>
  <Currency CrossOrder="9" Kod="EUR" CurrencyCode="EUR">
    <Unit>1</Unit><ForexBuying>48.9102</ForexBuying>
  </Currency>
</Tarih_Date>`

func TestParseTCMB_ReadsUSDForexBuyingAndTheBulletinDate(t *testing.T) {
	rate, err := exchangerate.ParseTCMB([]byte(bulletin))
	require.NoError(t, err)

	assert.Equal(t, "41.6512", rate.USDBuying.String())
	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), rate.RateDate)
	assert.Equal(t, exchangerate.SourceTCMB, rate.Source)
}

func TestParseTCMB_WithoutUSDIsAFetchFailure(t *testing.T) {
	_, err := exchangerate.ParseTCMB([]byte(`<Tarih_Date Date="10/01/2026"><Currency CurrencyCode="EUR"><ForexBuying>1</ForexBuying></Currency></Tarih_Date>`))
	assert.ErrorIs(t, err, exchangerate.ErrorFetchFailed)
}

func TestParseTCMB_MalformedIsAFetchFailure(t *testing.T) {
	_, err := exchangerate.ParseTCMB([]byte(`not xml`))
	assert.ErrorIs(t, err, exchangerate.ErrorFetchFailed)
}

func TestIsFallbackFor(t *testing.T) {
	rate := &exchangerate.Rate{RateDate: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	assert.False(t, rate.IsFallbackFor(time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)))
	assert.True(t, rate.IsFallbackFor(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)))
}
