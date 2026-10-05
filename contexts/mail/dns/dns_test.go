package dns_test

import (
	"context"
	"strings"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	maildns "github.com/piggystats/nodera/contexts/mail/dns"
	"github.com/piggystats/nodera/logger"
)

type keys map[string]string

func (k keys) ListPublishedKeys(context.Context) ([]*sendingdomain.PublishedKey, error) {
	var out []*sendingdomain.PublishedKey
	for id, key := range k {
		selector, token, _ := strings.Cut(id, ".")
		out = append(out, &sendingdomain.PublishedKey{Token: token, Selector: sendingdomain.Selector(selector), PublicKey: key})
	}

	return out, nil
}

func (k keys) FindPublishedKey(_ context.Context, token string, selector sendingdomain.Selector) (*sendingdomain.PublishedKey, error) {
	key, ok := k[string(selector)+"."+token]
	if !ok {
		return nil, sendingdomain.ErrorKeyNotFound
	}

	return &sendingdomain.PublishedKey{Token: token, Selector: selector, PublicKey: key}, nil
}

func server(t *testing.T, published keys) *maildns.Server {
	t.Helper()
	s, err := maildns.New(maildns.Config{
		Zone:        "mta.nodera.io",
		Nameservers: []string{"ns1.mta.nodera.io", "ns2.mta.nodera.io"},
		Hostmaster:  "hostmaster.nodera.io",
		MXHosts:     []string{"mx.mta.nodera.io"},
		SPF:         "v=spf1 ip4:203.0.113.0/28 -all",
		Addresses:   []string{"mx.mta.nodera.io=203.0.113.10", "ns1.mta.nodera.io=203.0.113.11", "ns1.mta.nodera.io=2001:db8::11"},
	}, published, logger.Noop())
	require.NoError(t, err)
	require.NoError(t, s.Refresh(context.Background()))

	return s
}

func ask(s *maildns.Server, name string, qtype uint16) *mdns.Msg {
	q := new(mdns.Msg)
	q.SetQuestion(mdns.Fqdn(name), qtype)

	return s.Answer(context.Background(), q)
}

func TestAnswer_DKIMKeyIsAuthoritativeTXT(t *testing.T) {
	long := strings.Repeat("A", 392)
	s := server(t, keys{"a.4k2x9q1z": long})

	m := ask(s, "a.4k2x9q1z.dkim.mta.nodera.io", mdns.TypeTXT)
	require.Equal(t, mdns.RcodeSuccess, m.Rcode)
	assert.True(t, m.Authoritative)
	require.Len(t, m.Answer, 1)
	txt := m.Answer[0].(*mdns.TXT)
	assert.Equal(t, "v=DKIM1; k=rsa; p="+long, strings.Join(txt.Txt, ""), "a long key is split into strings of at most 255")
	for _, part := range txt.Txt {
		assert.LessOrEqual(t, len(part), 255)
	}
	assert.Equal(t, uint32(300), txt.Hdr.Ttl)
}

func TestAnswer_AKeyAddedAfterTheLastRefreshIsFound(t *testing.T) {
	published := keys{}
	s := server(t, published)
	published["b.newtoken"] = "KEY"

	m := ask(s, "b.newtoken.dkim.mta.nodera.io", mdns.TypeTXT)
	require.Len(t, m.Answer, 1)
}

func TestAnswer_ReturnPathHasMXAndSPF(t *testing.T) {
	s := server(t, keys{})

	mx := ask(s, "rp.mta.nodera.io", mdns.TypeMX)
	require.Len(t, mx.Answer, 1)
	assert.Equal(t, "mx.mta.nodera.io.", mx.Answer[0].(*mdns.MX).Mx)

	spf := ask(s, "RP.MTA.NODERA.IO", mdns.TypeTXT)
	require.Len(t, spf.Answer, 1)
	assert.Equal(t, []string{"v=spf1 ip4:203.0.113.0/28 -all"}, spf.Answer[0].(*mdns.TXT).Txt)
}

func TestAnswer_ApexHasItsSOAAndNameservers(t *testing.T) {
	s := server(t, keys{})

	soa := ask(s, "mta.nodera.io", mdns.TypeSOA)
	require.Len(t, soa.Answer, 1)
	ns := ask(s, "mta.nodera.io", mdns.TypeNS)
	assert.Len(t, ns.Answer, 2)
}

func TestAnswer_TheZonesOwnHostsHaveAddresses(t *testing.T) {
	s := server(t, keys{})

	a := ask(s, "ns1.mta.nodera.io", mdns.TypeA)
	require.Len(t, a.Answer, 1)
	assert.Equal(t, "203.0.113.11", a.Answer[0].(*mdns.A).A.String())
	aaaa := ask(s, "ns1.mta.nodera.io", mdns.TypeAAAA)
	require.Len(t, aaaa.Answer, 1)
}

func TestAnswer_AnUnknownNameIsNXDOMAINWithTheSOA(t *testing.T) {
	missing := ask(server(t, keys{}), "a.unknown.dkim.mta.nodera.io", mdns.TypeTXT)

	assert.Equal(t, mdns.RcodeNameError, missing.Rcode)
	require.Len(t, missing.Ns, 1, "a negative answer carries the SOA")
}

func TestAnswer_AKnownNameWithoutTheTypeIsNoData(t *testing.T) {
	nodata := ask(server(t, keys{}), "rp.mta.nodera.io", mdns.TypeA)

	assert.Equal(t, mdns.RcodeSuccess, nodata.Rcode)
	assert.Empty(t, nodata.Answer)
}

func TestAnswer_AnotherZoneIsRefused(t *testing.T) {
	other := ask(server(t, keys{}), "example.com", mdns.TypeA)

	assert.Equal(t, mdns.RcodeRefused, other.Rcode)
}
