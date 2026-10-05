// Package dns is the authoritative server of the zone Nodera's Sending
// domains point at (docs/adr/0011): the DKIM keys behind the Teams' CNAMEs,
// and the MX and SPF every return path shares.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	mdns "github.com/miekg/dns"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	keyTTL    = 300
	staticTTL = 3600
	// txtChunk is the most one TXT string may hold.
	txtChunk = 255
	serial   = 2026100501
)

type Config struct {
	Zone        string
	Nameservers []string
	Hostmaster  string
	MXHosts     []string
	SPF         string
	// Addresses are "host=ip" pairs for the zone's own hosts.
	Addresses []string
}

// KeySource is where the zone reads the published DKIM keys from.
type KeySource interface {
	ListPublishedKeys(ctx context.Context) ([]*sendingdomain.PublishedKey, error)
	FindPublishedKey(ctx context.Context, token string, selector sendingdomain.Selector) (*sendingdomain.PublishedKey, error)
}

type Server struct {
	cfg       Config
	zone      string
	dkimZone  string
	rp        string
	addresses map[string][]net.IP
	source    KeySource
	log       logger.ILog

	mu   sync.RWMutex
	keys map[string]string
}

func New(cfg Config, source KeySource, log logger.ILog) (*Server, error) {
	zone := fqdn(cfg.Zone)
	s := &Server{
		cfg: cfg, zone: zone, dkimZone: "dkim." + zone, rp: fqdn(sendingdomain.ReturnPathTarget(cfg.Zone)),
		addresses: map[string][]net.IP{}, source: source, log: log, keys: map[string]string{},
	}

	for _, pair := range cfg.Addresses {
		host, raw, ok := strings.Cut(pair, "=")
		ip := net.ParseIP(strings.TrimSpace(raw))
		if !ok || ip == nil {
			return nil, fmt.Errorf("dns address %q is not host=ip", pair)
		}
		name := fqdn(host)
		if !mdns.IsSubDomain(zone, name) {
			return nil, fmt.Errorf("dns address %q is outside the zone", pair)
		}
		s.addresses[name] = append(s.addresses[name], ip)
	}

	return s, nil
}

// Refresh reloads every published key; a key added since is still found by
// the lookup on a miss.
func (s *Server) Refresh(ctx context.Context) error {
	published, err := s.source.ListPublishedKeys(ctx)
	if err != nil {
		return err
	}

	keys := make(map[string]string, len(published))
	for _, k := range published {
		keys[string(k.Selector)+"."+k.Token] = k.PublicKey
	}

	s.mu.Lock()
	s.keys = keys
	s.mu.Unlock()

	return nil
}

func (s *Server) ServeDNS(w mdns.ResponseWriter, r *mdns.Msg) {
	if err := w.WriteMsg(s.Answer(context.Background(), r)); err != nil {
		s.log.Error(err)
	}
}

// Answer is the authoritative answer to one query: refused outside the zone,
// NXDOMAIN with the SOA for a name that does not exist, an empty answer with
// the SOA for a name without that type.
func (s *Server) Answer(ctx context.Context, r *mdns.Msg) *mdns.Msg {
	m := new(mdns.Msg)
	m.SetReply(r)
	if len(r.Question) != 1 {
		m.Rcode = mdns.RcodeFormatError

		return m
	}

	q := r.Question[0]
	name := strings.ToLower(q.Name)
	if !mdns.IsSubDomain(s.zone, name) {
		m.Rcode = mdns.RcodeRefused

		return m
	}

	m.Authoritative = true
	records, exists := s.records(ctx, name, q.Qtype)
	switch {
	case !exists:
		m.Rcode = mdns.RcodeNameError
		m.Ns = []mdns.RR{s.soa()}
	case len(records) == 0:
		m.Ns = []mdns.RR{s.soa()}
	default:
		m.Answer = records
	}

	return m
}

func (s *Server) records(ctx context.Context, name string, qtype uint16) ([]mdns.RR, bool) {
	switch {
	case name == s.zone:
		return s.apex(qtype), true
	case name == s.rp:
		return s.returnPath(qtype), true
	case name == s.dkimZone:
		return nil, true
	case mdns.IsSubDomain(s.dkimZone, name):
		return s.dkim(ctx, name, qtype)
	}

	if ips, ok := s.addresses[name]; ok {
		return hostRecords(name, ips, qtype), true
	}

	return nil, false
}

func (s *Server) apex(qtype uint16) []mdns.RR {
	var out []mdns.RR
	if qtype == mdns.TypeSOA || qtype == mdns.TypeANY {
		out = append(out, s.soa())
	}
	if qtype == mdns.TypeNS || qtype == mdns.TypeANY {
		for _, ns := range s.cfg.Nameservers {
			out = append(out, &mdns.NS{Hdr: header(s.zone, mdns.TypeNS, staticTTL), Ns: fqdn(ns)})
		}
	}

	return out
}

func (s *Server) returnPath(qtype uint16) []mdns.RR {
	var out []mdns.RR
	if qtype == mdns.TypeMX || qtype == mdns.TypeANY {
		for i, host := range s.cfg.MXHosts {
			out = append(out, &mdns.MX{Hdr: header(s.rp, mdns.TypeMX, staticTTL), Preference: uint16(10 * (i + 1)), Mx: fqdn(host)})
		}
	}
	if (qtype == mdns.TypeTXT || qtype == mdns.TypeANY) && str.IsNotEmpty(s.cfg.SPF) {
		out = append(out, &mdns.TXT{Hdr: header(s.rp, mdns.TypeTXT, staticTTL), Txt: chunks(s.cfg.SPF)})
	}

	return out
}

// dkim answers "<selector>.<token>.dkim.<zone>"; the token alone is an empty
// name on the way to a key.
func (s *Server) dkim(ctx context.Context, name string, qtype uint16) ([]mdns.RR, bool) {
	labels := mdns.SplitDomainName(strings.TrimSuffix(name, "."+s.dkimZone))
	switch len(labels) {
	case 1:
		return nil, s.knowsToken(labels[0])
	case 2:
	default:
		return nil, false
	}

	selector, token := sendingdomain.Selector(labels[0]), labels[1]
	if !selector.IsValid() {
		return nil, false
	}

	key, ok := s.key(ctx, selector, token)
	if !ok {
		return nil, false
	}
	if qtype != mdns.TypeTXT && qtype != mdns.TypeANY {
		return nil, true
	}

	return []mdns.RR{&mdns.TXT{Hdr: header(name, mdns.TypeTXT, keyTTL), Txt: chunks(sendingdomain.DKIMRecord(key))}}, true
}

func (s *Server) key(ctx context.Context, selector sendingdomain.Selector, token string) (string, bool) {
	s.mu.RLock()
	key, ok := s.keys[string(selector)+"."+token]
	s.mu.RUnlock()
	if ok {
		return key, true
	}

	published, err := s.source.FindPublishedKey(ctx, token, selector)
	if err != nil {
		if !errors.Is(err, sendingdomain.ErrorKeyNotFound) {
			s.log.Error(err)
		}

		return "", false
	}

	s.mu.Lock()
	s.keys[string(selector)+"."+token] = published.PublicKey
	s.mu.Unlock()

	return published.PublicKey, true
}

func (s *Server) knowsToken(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, a := s.keys[string(sendingdomain.SelectorA)+"."+token]
	_, b := s.keys[string(sendingdomain.SelectorB)+"."+token]

	return a || b
}

func (s *Server) soa() mdns.RR {
	ns := s.zone
	if len(s.cfg.Nameservers) > 0 {
		ns = fqdn(s.cfg.Nameservers[0])
	}

	return &mdns.SOA{
		Hdr: header(s.zone, mdns.TypeSOA, staticTTL), Ns: ns, Mbox: fqdn(s.cfg.Hostmaster), Serial: serial,
		Refresh: 3600, Retry: 600, Expire: 604800, Minttl: keyTTL,
	}
}

func hostRecords(name string, ips []net.IP, qtype uint16) []mdns.RR {
	var out []mdns.RR
	for _, ip := range ips {
		switch {
		case ip.To4() != nil && (qtype == mdns.TypeA || qtype == mdns.TypeANY):
			out = append(out, &mdns.A{Hdr: header(name, mdns.TypeA, staticTTL), A: ip.To4()})
		case ip.To4() == nil && (qtype == mdns.TypeAAAA || qtype == mdns.TypeANY):
			out = append(out, &mdns.AAAA{Hdr: header(name, mdns.TypeAAAA, staticTTL), AAAA: ip})
		}
	}

	return out
}

func header(name string, rrtype uint16, ttl uint32) mdns.RR_Header {
	return mdns.RR_Header{Name: name, Rrtype: rrtype, Class: mdns.ClassINET, Ttl: ttl}
}

// chunks splits a TXT value into the strings DNS allows; a resolver joins
// them back.
func chunks(value string) []string {
	var out []string
	for len(value) > txtChunk {
		out = append(out, value[:txtChunk])
		value = value[txtChunk:]
	}

	return append(out, value)
}

func fqdn(name string) string {
	return mdns.Fqdn(strings.ToLower(strings.TrimSpace(name)))
}
