package app

import (
	"context"
	"net"
	"strings"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
)

// VerifyAsPublished checks a Sending domain as if DNS answered with exactly
// the records the web app asked the Team to publish. Dev's zone is not
// public, so the real check never passes there; the daily check that follows
// reads DNS again and unverifies the domain a day later.
func VerifyAsPublished(ctx context.Context, d Deps, sendingDomainID uuid.UUID) (*sendingdomain.SendingDomain, error) {
	domain, err := NewServices(d.Pool).SendingDomains.FindByID(ctx, sendingDomainID)
	if err != nil {
		return nil, err
	}
	d.Resolver = publishedRecords(domain.Records(d.Conf.DNS.Zone))

	return NewHandlers(d).SendingDomains.Check(ctx, sendingDomainID)
}

// publishedRecords is DNS holding a Sending domain's records and nothing
// else, so DMARC reads as missing as it would without one.
type publishedRecords []sendingdomain.Record

func (p publishedRecords) LookupCNAME(_ context.Context, host string) (string, error) {
	for _, r := range p {
		if r.Type == "CNAME" && strings.EqualFold(r.Host, strings.TrimSuffix(host, ".")) {
			return r.Value + ".", nil
		}
	}

	return "", notFound(host)
}

func (p publishedRecords) LookupTXT(_ context.Context, name string) ([]string, error) {
	return nil, notFound(name)
}

func notFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}
