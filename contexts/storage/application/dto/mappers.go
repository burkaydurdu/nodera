package dto

import (
	"github.com/piggystats/nodera/contexts/storage/aggregates/accesskey"
	"github.com/piggystats/nodera/contexts/storage/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/aggregates/usage"
	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/utils/str"
)

func ToRegions(list []*catalog.Region) []Region {
	out := make([]Region, 0, len(list))
	for _, r := range list {
		out = append(out, Region{Code: r.Code, City: r.City, Country: r.Country})
	}

	return out
}

func ToPackages(list []*catalog.Package) []Package {
	out := make([]Package, 0, len(list))
	for _, p := range list {
		out = append(out, Package{Code: p.Code, SizeBytes: p.SizeBytes, MonthlyPrice: p.MonthlyPrice.StringFixed(catalog.PriceScale)})
	}

	return out
}

// Addresses answers the S3 endpoint of a Region and the URL of a bucket in it.
type Addresses interface {
	Endpoint(region string) string
	BucketURL(region, bucket string) string
}

func ToStorage(s *storage.Storage, billingStatus string, addresses Addresses) *Storage {
	out := &Storage{
		ID: s.ID, ProjectID: s.ProjectID, BucketName: s.BucketName, Region: s.Region,
		Endpoint: addresses.Endpoint(s.Region), URL: addresses.BucketURL(s.Region, s.BucketName),
		Status: string(s.Status), State: string(s.State()),
		Package: HeldPackage{
			Code: s.Package.Code, SizeBytes: s.Package.SizeBytes,
			MonthlyPrice: s.Package.MonthlyPrice.StringFixed(storage.PriceScale), StartedAt: s.Package.StartedAt,
		},
		UsedBytes: s.UsedBytes, ObjectCount: s.ObjectCount, MeasuredAt: s.MeasuredAt,
		BillingStatus: billingStatus, CreatedAt: s.CreatedAt,
	}
	if s.Scheduled != nil {
		out.ScheduledChange = &ScheduledChange{
			Code: s.Scheduled.Code, SizeBytes: s.Scheduled.SizeBytes,
			MonthlyPrice: s.Scheduled.MonthlyPrice.StringFixed(storage.PriceScale), EffectiveAt: s.Scheduled.Due(),
		}
	}
	if s.Pause != nil {
		out.Pause = &Pause{Reason: string(s.Pause.Reason), PausedAt: s.Pause.At}
	}

	return out
}

func ToStorages(list []*storage.Storage, billingStatus string, addresses Addresses) []*Storage {
	out := make([]*Storage, 0, len(list))
	for _, s := range list {
		out = append(out, ToStorage(s, billingStatus, addresses))
	}

	return out
}

func ToAccessKey(k *accesskey.AccessKey) AccessKey {
	return AccessKey{ID: k.ID, Name: k.Name, AccessKeyID: k.AccessKeyID, Permission: string(k.Permission), CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt}
}

func ToAccessKeys(list []*accesskey.AccessKey) []AccessKey {
	out := make([]AccessKey, 0, len(list))
	for _, k := range list {
		out = append(out, ToAccessKey(k))
	}

	return out
}

func ToListing(prefix string, l *engine.Listing) *Listing {
	out := &Listing{Prefix: prefix, Folders: l.Folders, Objects: make([]Object, 0, len(l.Objects))}
	if out.Folders == nil {
		out.Folders = []string{}
	}
	for _, o := range l.Objects {
		out.Objects = append(out.Objects, Object{Key: o.Key, Size: o.Size, ModifiedAt: o.ModifiedAt})
	}
	if str.IsNotEmpty(l.Next) {
		next := l.Next
		out.NextCursor = &next
	}

	return out
}

func ToUsage(s *storage.Storage, days []*usage.Day) *Usage {
	out := &Usage{StorageID: s.ID, Days: make([]UsageDay, 0, len(days))}
	for _, d := range days {
		out.Days = append(out.Days, UsageDay{
			Day: d.Day.Format("2006-01-02"), UsedBytes: d.UsedBytes, ObjectCount: d.ObjectCount,
			BytesSent: d.BytesSent, BytesReceived: d.BytesReceived, Requests: d.Requests,
		})
	}

	return out
}
