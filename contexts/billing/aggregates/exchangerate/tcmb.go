package exchangerate

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/utils/str"
)

const (
	// DefaultTCMBURL is the central bank's bulletin of the day.
	DefaultTCMBURL = "https://www.tcmb.gov.tr/kurlar/today.xml"
	usdCode        = "USD"
	tcmbDateLayout = "01/02/2006"
	fetchTimeout   = 30 * time.Second
	maxBody        = 1 << 20
)

// Fetcher answers today's published USD buying rate and the day it is for.
//
//go:generate mockgen -destination=../../mock/exchangerate/fetcher.go -package=mockexchangerate github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate Fetcher
type Fetcher interface {
	Fetch(ctx context.Context) (*Rate, error)
}

// TCMBFetcher reads the bulletin XML and keeps the USD ForexBuying value.
type TCMBFetcher struct {
	url    string
	client *http.Client
}

func NewTCMBFetcher(url string) *TCMBFetcher {
	if str.IsBlank(url) {
		url = DefaultTCMBURL
	}

	return &TCMBFetcher{url: url, client: &http.Client{Timeout: fetchTimeout}}
}

func (f *TCMBFetcher) Fetch(ctx context.Context) (*Rate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, ErrorFetchFailed.Copy().SetError(err)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return nil, ErrorFetchFailed.Copy().SetError(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, ErrorFetchFailed.Copy().SetMessagef("exchange rate source answered %d", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, ErrorFetchFailed.Copy().SetError(err)
	}

	return ParseTCMB(body)
}

type tcmbBulletin struct {
	Date       string `xml:"Date,attr"`
	Currencies []struct {
		Code        string `xml:"CurrencyCode,attr"`
		Unit        int    `xml:"Unit"`
		ForexBuying string `xml:"ForexBuying"`
	} `xml:"Currency"`
}

// ParseTCMB reads the USD ForexBuying rate out of a bulletin. The bulletin
// dates its own publication, so the rate is stored under that day whatever
// day it is fetched on.
func ParseTCMB(body []byte) (*Rate, error) {
	var bulletin tcmbBulletin
	if err := xml.Unmarshal(body, &bulletin); err != nil {
		return nil, ErrorFetchFailed.Copy().SetError(err)
	}
	date, err := time.Parse(tcmbDateLayout, bulletin.Date)
	if err != nil {
		return nil, ErrorFetchFailed.Copy().SetError(fmt.Errorf("bulletin date %q: %w", bulletin.Date, err))
	}

	for _, c := range bulletin.Currencies {
		if c.Code != usdCode {
			continue
		}
		buying, err := decimal.NewFromString(c.ForexBuying)
		if err != nil {
			return nil, ErrorFetchFailed.Copy().SetError(fmt.Errorf("usd forex buying %q: %w", c.ForexBuying, err))
		}
		if c.Unit > 1 {
			buying = buying.Div(decimal.NewFromInt(int64(c.Unit)))
		}
		if !buying.IsPositive() {
			return nil, ErrorFetchFailed.Copy().SetMessage("usd forex buying rate is not positive")
		}

		return &Rate{RateDate: Day(date), USDBuying: buying, Source: SourceTCMB}, nil
	}

	return nil, ErrorFetchFailed.Copy().SetMessage("bulletin carries no USD rate")
}
