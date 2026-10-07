package ceph

import (
	"context"
	"sort"
	"time"

	"github.com/piggystats/nodera/contexts/storage/engine"
)

// Stats reads the bucket's index statistics through the admin API, which the
// bucket's own policy cannot refuse. Multipart parts in flight are counted
// too, as the quota counts them. A bucket the gateway does not know is an
// error: an open Storage's bucket is never empty by being missing.
func (c *Client) Stats(ctx context.Context, _ *engine.Account, bucket string) (*engine.Stats, error) {
	info, err := c.admin.getBucket(ctx, bucket)
	if err != nil {
		return nil, wrap("reading the bucket's statistics", err)
	}
	stats := &engine.Stats{}
	for _, category := range info.Usage {
		stats.Bytes += category.Size
		stats.Objects += category.NumObjects
	}

	return stats, nil
}

// Traffic reads the gateway's usage log for the bucket. The log keeps one
// entry per hour and operation category; the categories are summed into the
// hour. Only hours in [from, to) that saw a request are answered.
func (c *Client) Traffic(ctx context.Context, account *engine.Account, bucket string, from, to time.Time) ([]engine.Traffic, error) {
	usage, err := c.admin.accountUsage(ctx, account.ID, from.UTC(), to.UTC())
	if err != nil {
		return nil, wrap("reading the bucket's traffic", err)
	}
	hours := map[int64]*engine.Traffic{}
	for _, entry := range usage.Entries {
		for _, b := range entry.Buckets {
			if b.Bucket != bucket {
				continue
			}
			hour := time.Unix(b.Epoch, 0).UTC().Truncate(time.Hour)
			if hour.Before(from) || !hour.Before(to) {
				continue
			}
			t, ok := hours[hour.Unix()]
			if !ok {
				t = &engine.Traffic{Hour: hour}
				hours[hour.Unix()] = t
			}
			for _, cat := range b.Categories {
				t.BytesSent += cat.BytesSent
				t.BytesReceived += cat.BytesReceived
				t.Requests += cat.Ops
			}
		}
	}
	traffic := make([]engine.Traffic, 0, len(hours))
	for _, t := range hours {
		traffic = append(traffic, *t)
	}
	sort.Slice(traffic, func(i, j int) bool { return traffic[i].Hour.Before(traffic[j].Hour) })

	return traffic, nil
}
