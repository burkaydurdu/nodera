// Package providers is the adapter seam between the droplet context and the
// suppliers it rents servers from. The interface is drawn against Cherry
// Servers and Zenlayer's pay-as-you-go API (docs/adr/0003); each adapter keeps
// its supplier's vocabulary inside and answers in Nodera's.
package providers

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
)

// Status is the Droplet status an adapter maps its supplier's state onto; the
// string values equal the droplet aggregate's so the mapping is a cast.
type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusActive       Status = "active"
	StatusOff          Status = "off"
	StatusFailed       Status = "failed"
	StatusDeleting     Status = "deleting"
	StatusDeleted      Status = "deleted"
)

type Action string

const (
	ActionPowerOn  Action = "power_on"
	ActionPowerOff Action = "power_off"
	ActionReboot   Action = "reboot"
)

type Region struct {
	Ref     string
	Name    string
	Country string
}

// PlanRegion is one Offer: what the supplier charges per hour in USD and how
// many it has in that location.
type PlanRegion struct {
	RegionRef   string
	HourlyPrice decimal.Decimal
	Stock       int
}

type Plan struct {
	Ref      string
	Name     string
	Hardware map[string]any
	Regions  []PlanRegion
}

// Region finds the Offer of this Plan in one supplier location.
func (p Plan) Region(ref string) (PlanRegion, bool) {
	for _, r := range p.Regions {
		if r.RegionRef == ref {
			return r, true
		}
	}

	return PlanRegion{}, false
}

type Image struct {
	Ref  string
	Name string
}

type ServerSpec struct {
	PlanRef    string
	RegionRef  string
	ImageRef   string
	Hostname   string
	SSHKeyRefs []string
}

type Server struct {
	Ref    string
	Status Status
	IPv4   string
	IPv6   string
}

//go:generate mockgen -destination=../mock/providers/provider.go -package=mockproviders github.com/piggystats/nodera/contexts/droplet/providers Provider
type Provider interface {
	Code() string
	ListRegions(ctx context.Context) ([]Region, error)
	// ListPlans answers every plan with its per-region hourly USD price and stock.
	ListPlans(ctx context.Context) ([]Plan, error)
	// ListImages answers the images a plan can be installed with.
	ListImages(ctx context.Context, planRef string) ([]Image, error)
	// EnsureSSHKey answers the supplier-side reference of a public key,
	// creating it when the supplier does not have it yet.
	EnsureSSHKey(ctx context.Context, name, publicKey string) (string, error)
	CreateServer(ctx context.Context, spec ServerSpec) (string, error)
	GetServer(ctx context.Context, ref string) (*Server, error)
	PowerAction(ctx context.Context, ref string, action Action) error
	DeleteServer(ctx context.Context, ref string) error
}

// Registry holds the configured adapters by Provider code.
type Registry struct {
	byCode map[string]Provider
}

func NewRegistry(adapters ...Provider) *Registry {
	r := &Registry{byCode: make(map[string]Provider, len(adapters))}
	for _, a := range adapters {
		r.byCode[a.Code()] = a
	}

	return r
}

func (r *Registry) Register(p Provider) {
	r.byCode[p.Code()] = p
}

// Get answers the adapter for a Provider code; a Provider the catalog knows
// but the binary has no adapter for is a configuration error.
func (r *Registry) Get(code string) (Provider, error) {
	p, ok := r.byCode[code]
	if !ok {
		return nil, provider.ErrorNotConfigured.Copy().SetMessage("no adapter configured for this provider").Add("provider", code)
	}

	return p, nil
}
