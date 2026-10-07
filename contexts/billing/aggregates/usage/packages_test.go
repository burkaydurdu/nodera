package usage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/utils/str"
)

var (
	assets  = uuid.MustParse("3a9d2f10-7b4e-4c6a-9f1d-5e8b2c7a0001")
	backups = uuid.MustParse("3a9d2f10-7b4e-4c6a-9f1d-5e8b2c7a0002")
)

func stored(bucket, code, monthly string, start time.Time, stop *time.Time) *usage.StorageInterval {
	id := assets
	if bucket == "shop-backups" {
		id = backups
	}

	return &usage.StorageInterval{
		StorageID: id, TeamID: mailTeam, ProjectID: shop, ProjectName: "Shop", BucketName: bucket, Region: "eu-central-1",
		PackageCode: code, SizeBytes: 250_000_000_000, MonthlyPrice: price(monthly), StartedAt: start, StoppedAt: stop,
	}
}

// The one rule both products are priced by, exercised through Storage's
// intervals; Mail's own tests exercise it through MailLines.
func TestPackageCharges_HighestPriceInThePeriodLessWhatWasBilled(t *testing.T) {
	cases := []struct {
		name      string
		intervals []*usage.StorageInterval
		from, to  time.Time
		billed    string
		// wantCode empty: the Storage owes nothing more for the Period.
		wantCode, wantAmount string
	}{
		{
			name:      "held the whole Period",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", month(9), nil)},
			from:      month(10), to: month(11),
			wantCode: "250gb", wantAmount: "10.00",
		},
		{
			name:      "opened on the 25th is not prorated",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", at(25, 18, 0), nil)},
			from:      month(10), to: month(11),
			wantCode: "250gb", wantAmount: "10.00",
		},
		{
			name:      "deleted on the 3rd is not prorated",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", month(9), ptr(at(3, 9, 0)))},
			from:      month(10), to: month(11),
			wantCode: "250gb", wantAmount: "10.00",
		},
		{
			name: "a move to a larger Package pays only the larger price",
			intervals: []*usage.StorageInterval{
				stored("shop-assets", "10gb", "1.00", day(1), ptr(day(15))),
				stored("shop-assets", "1tb", "35.00", day(15), nil),
			},
			from: month(10), to: month(11),
			wantCode: "1tb", wantAmount: "35.00",
		},
		{
			name: "what an earlier Invoice of the Period charged is deducted",
			intervals: []*usage.StorageInterval{
				stored("shop-assets", "250gb", "10.00", day(1), ptr(day(15))),
				stored("shop-assets", "1tb", "35.00", day(15), nil),
			},
			from: month(10), to: month(11), billed: "10.00",
			wantCode: "1tb", wantAmount: "25.00",
		},
		{
			name:      "nothing more once the highest price was charged",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", day(1), nil)},
			from:      month(10), to: month(11), billed: "10.00",
		},
		{
			name:      "a free Package owes nothing",
			intervals: []*usage.StorageInterval{stored("shop-assets", "trial", "0.00", day(1), nil)},
			from:      month(10), to: month(11),
		},
		{
			name:      "stopped before the Period is not held in it",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", month(9), ptr(time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)))},
			from:      month(10), to: month(11),
		},
		{
			name:      "stopped exactly at the Period start is not held in it",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", month(9), ptr(month(10)))},
			from:      month(10), to: month(11),
		},
		{
			name:      "an empty window holds nothing",
			intervals: []*usage.StorageInterval{stored("shop-assets", "250gb", "10.00", month(9), nil)},
			from:      month(11), to: month(11),
		},
		{
			name: "on a tie the latest interval names the charge",
			intervals: []*usage.StorageInterval{
				stored("shop-assets", "250gb", "10.00", day(1), ptr(day(5))),
				stored("shop-assets", "250gb-v2", "10.00", day(10), nil),
			},
			from: month(10), to: month(11),
			wantCode: "250gb-v2", wantAmount: "10.00",
		},
		{
			name: "intervals outside the Period never raise the price",
			intervals: []*usage.StorageInterval{
				stored("shop-assets", "1tb", "35.00", month(9), ptr(month(10))),
				stored("shop-assets", "50gb", "3.00", month(10), ptr(month(11))),
				stored("shop-assets", "250gb", "10.00", month(11), nil),
			},
			from: month(10), to: month(11),
			wantCode: "50gb", wantAmount: "3.00",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			billed := map[uuid.UUID]decimal.Decimal{}
			if str.IsNotEmpty(c.billed) {
				billed[assets] = price(c.billed)
			}

			charges := usage.PackageCharges(c.intervals, c.from, c.to, billed)

			if str.IsEmpty(c.wantCode) {
				assert.Empty(t, charges)

				return
			}
			require.Len(t, charges, 1)
			assert.Equal(t, assets, charges[0].Interval.StorageID)
			assert.Equal(t, c.wantCode, charges[0].Interval.PackageCode)
			assert.Equal(t, c.wantAmount, charges[0].Amount.StringFixed(2))
		})
	}
}

// Storage's intervals are keyed by the Storage, so two Storages of one
// Project are two charges, in the order their Packages started.
func TestStorageLines_OneLinePerStorageNotPerProject(t *testing.T) {
	intervals := []*usage.StorageInterval{
		stored("shop-assets", "250gb", "10.00", day(7), nil),
		stored("shop-backups", "50gb", "3.00", day(3), nil),
		stored("shop-assets", "10gb", "1.00", day(4), ptr(day(7))),
	}

	lines := usage.StorageLines(intervals, month(10), month(11), nil)

	require.Len(t, lines, 2)
	assert.Equal(t, usage.StorageLine{StorageID: backups, BucketName: "shop-backups", ProjectID: shop, ProjectName: "Shop", PackageCode: "50gb", Amount: price("3.00")}, lines[0])
	assert.Equal(t, usage.StorageLine{StorageID: assets, BucketName: "shop-assets", ProjectID: shop, ProjectName: "Shop", PackageCode: "250gb", Amount: price("10.00")}, lines[1])

	lines = usage.StorageLines(intervals, month(10), month(11), map[uuid.UUID]decimal.Decimal{backups: price("3.00")})

	require.Len(t, lines, 1, "the backups were charged in full already")
	assert.Equal(t, assets, lines[0].StorageID)
}

func TestStorageLines_NoIntervalsNoLines(t *testing.T) {
	assert.Empty(t, usage.StorageLines(nil, month(10), month(11), nil))
}
