// Package fake is an in-memory Provider scripted by tests: stock and prices
// per plan and region, failures to inject, and servers that move only when
// the test calls Advance.
package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

const DefaultCode = "cherry"

type server struct {
	spec    providers.ServerSpec
	status  providers.Status
	ipv4    string
	ipv6    string
	pending *providers.Action
}

type Fake struct {
	mu      sync.Mutex
	code    string
	regions []providers.Region
	plans   map[string]*providers.Plan
	images  []providers.Image
	keys    map[string]string
	servers map[string]*server
	nextID  int

	listErr   error
	createErr error
	getErr    error
	deleteErr error
	actionErr error

	// Specs records every CreateServer call so a test can assert what the
	// supplier was asked for.
	Specs []providers.ServerSpec
}

// New answers an empty Fake registered under the given Provider code; tests
// script it before the first call.
func New(code string) *Fake {
	if str.IsBlank(code) {
		code = DefaultCode
	}

	return &Fake{
		code:    code,
		plans:   make(map[string]*providers.Plan),
		keys:    make(map[string]string),
		servers: make(map[string]*server),
	}
}

// ---------------------------------------------------------------------------
// Scripting
// ---------------------------------------------------------------------------

func (f *Fake) AddRegion(ref, name, country string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.regions = append(f.regions, providers.Region{Ref: ref, Name: name, Country: country})
}

func (f *Fake) AddImage(ref, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.images = append(f.images, providers.Image{Ref: ref, Name: name})
}

// SetOffer sets the price and stock of a plan in a region, creating the plan
// when it is new.
func (f *Fake) SetOffer(planRef, regionRef, hourlyPrice string, stock int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.plans[planRef]
	if !ok {
		p = &providers.Plan{Ref: planRef, Name: planRef, Hardware: map[string]any{"cpu": planRef}}
		f.plans[planRef] = p
	}
	price := decimal.RequireFromString(hourlyPrice)
	for i := range p.Regions {
		if p.Regions[i].RegionRef == regionRef {
			p.Regions[i].HourlyPrice = price
			p.Regions[i].Stock = stock

			return
		}
	}
	p.Regions = append(p.Regions, providers.PlanRegion{RegionRef: regionRef, HourlyPrice: price, Stock: stock})
}

func (f *Fake) SetStock(planRef, regionRef string, stock int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.plans[planRef]; ok {
		for i := range p.Regions {
			if p.Regions[i].RegionRef == regionRef {
				p.Regions[i].Stock = stock
			}
		}
	}
}

func (f *Fake) SetPrice(planRef, regionRef, hourlyPrice string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.plans[planRef]; ok {
		for i := range p.Regions {
			if p.Regions[i].RegionRef == regionRef {
				p.Regions[i].HourlyPrice = decimal.RequireFromString(hourlyPrice)
			}
		}
	}
}

// RemovePlan makes the supplier stop listing a plan, as if it vanished.
func (f *Fake) RemovePlan(planRef string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.plans, planRef)
}

func (f *Fake) FailList(err error)   { f.mu.Lock(); f.listErr = err; f.mu.Unlock() }
func (f *Fake) FailCreate(err error) { f.mu.Lock(); f.createErr = err; f.mu.Unlock() }
func (f *Fake) FailGet(err error)    { f.mu.Lock(); f.getErr = err; f.mu.Unlock() }
func (f *Fake) FailDelete(err error) { f.mu.Lock(); f.deleteErr = err; f.mu.Unlock() }
func (f *Fake) FailAction(err error) { f.mu.Lock(); f.actionErr = err; f.mu.Unlock() }

// Advance moves every server one step: provisioning becomes active with
// addresses, a pending power action takes effect.
func (f *Fake) Advance() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.servers {
		switch {
		case s.status == providers.StatusProvisioning:
			s.status = providers.StatusActive
		case s.pending != nil:
			switch *s.pending {
			case providers.ActionPowerOff:
				s.status = providers.StatusOff
			case providers.ActionPowerOn, providers.ActionReboot:
				s.status = providers.StatusActive
			}
			s.pending = nil
		}
	}
}

// SetStatus puts a server in a status, as the supplier would report it on its
// own: powered off at the end of an install, failed, gone.
func (f *Fake) SetStatus(ref string, status providers.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.servers[ref]; ok {
		s.status = status
	}
}

// Server exposes a server's supplier-side state to assertions.
func (f *Fake) Server(ref string) (providers.Server, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[ref]
	if !ok {
		return providers.Server{}, false
	}

	return providers.Server{Ref: ref, Status: s.status, IPv4: s.ipv4, IPv6: s.ipv6}, true
}

// Reset forgets servers, keys and failures but keeps the catalog script.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers = make(map[string]*server)
	f.keys = make(map[string]string)
	f.Specs = nil
	f.listErr, f.createErr, f.getErr, f.deleteErr, f.actionErr = nil, nil, nil, nil, nil
}

// ---------------------------------------------------------------------------
// providers.Provider
// ---------------------------------------------------------------------------

func (f *Fake) Code() string { return f.code }

func (f *Fake) ListRegions(_ context.Context) ([]providers.Region, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}

	return append([]providers.Region(nil), f.regions...), nil
}

func (f *Fake) ListPlans(_ context.Context) ([]providers.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}

	refs := make([]string, 0, len(f.plans))
	for ref := range f.plans {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	plans := make([]providers.Plan, 0, len(refs))
	for _, ref := range refs {
		p := f.plans[ref]
		plans = append(plans, providers.Plan{Ref: p.Ref, Name: p.Name, Hardware: p.Hardware, Regions: append([]providers.PlanRegion(nil), p.Regions...)})
	}

	return plans, nil
}

func (f *Fake) ListImages(_ context.Context, _ string) ([]providers.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}

	return append([]providers.Image(nil), f.images...), nil
}

func (f *Fake) EnsureSSHKey(_ context.Context, _, publicKey string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fingerprint := hash.SHA256Hex(strings.TrimSpace(publicKey))
	if ref, ok := f.keys[fingerprint]; ok {
		return ref, nil
	}
	f.nextID++
	ref := fmt.Sprintf("key-%d", f.nextID)
	f.keys[fingerprint] = ref

	return ref, nil
}

func (f *Fake) CreateServer(_ context.Context, spec providers.ServerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Specs = append(f.Specs, spec)
	if f.createErr != nil {
		return "", f.createErr
	}

	f.nextID++
	ref := fmt.Sprintf("srv-%d", f.nextID)
	f.servers[ref] = &server{
		spec:   spec,
		status: providers.StatusProvisioning,
		ipv4:   fmt.Sprintf("203.0.113.%d", f.nextID%250+1),
		ipv6:   fmt.Sprintf("2001:db8::%x", f.nextID),
	}

	return ref, nil
}

func (f *Fake) GetServer(_ context.Context, ref string) (*providers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	s, ok := f.servers[ref]
	if !ok {
		return nil, provider.ErrorServerNotFound
	}

	out := &providers.Server{Ref: ref, Status: s.status}
	if s.status == providers.StatusActive || s.status == providers.StatusOff {
		out.IPv4, out.IPv6 = s.ipv4, s.ipv6
	}

	return out, nil
}

func (f *Fake) PowerAction(_ context.Context, ref string, action providers.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.actionErr != nil {
		return f.actionErr
	}
	s, ok := f.servers[ref]
	if !ok {
		return provider.ErrorServerNotFound
	}
	a := action
	s.pending = &a

	return nil
}

// DeleteServer releases the server at once; GetServer then reports deleted.
func (f *Fake) DeleteServer(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	s, ok := f.servers[ref]
	if !ok {
		return provider.ErrorServerNotFound
	}
	s.status = providers.StatusDeleted
	s.pending = nil

	return nil
}
