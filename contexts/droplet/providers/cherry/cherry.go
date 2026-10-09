// Package cherry is the Cherry Servers adapter (docs/adr/0003) over the
// official SDK. Everything Cherry-specific stays here: the hourly cycle, the
// single project all Droplets are created in, the state table and the raw
// error messages, which are logged and never answered to a customer.
package cherry

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/cherryservers/cherrygo/v4"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	Code = "cherry"

	cycleHourly    = "hourly"
	pricingHourly  = "hourly"
	currencyUSD    = "usd"
	publicIPType   = "primary-ip"
	floatingIPType = "floating-ip"
	ipv4Family     = 4
	ipv6Family     = 6
	userAgent      = "nodera-droplet"

	// labelNameLength and labelSuffixLength keep an uploaded key's label at 64
	// characters: the name's start and a short fingerprint of the key.
	labelNameLength   = 55
	labelSuffixLength = 8
)

type Config struct {
	Token     string
	ProjectID int
	// Endpoint overrides the API base URL; empty means the SDK default.
	Endpoint string
}

// api is the slice of the SDK client the adapter drives; the SDK's own
// service interfaces let a test stand in for the wire.
type api struct {
	regions cherrygo.RegionsService
	plans   cherrygo.PlansService
	images  cherrygo.ImagesService
	sshKeys cherrygo.SSHKeysService
	servers cherrygo.ServersService
}

type Cherry struct {
	api       api
	projectID int
	log       logger.ILog
}

func New(cfg Config, log logger.ILog) (*Cherry, error) {
	if str.IsBlank(cfg.Token) || cfg.ProjectID == 0 {
		return nil, provider.ErrorNotConfigured.Copy().SetMessage("cherry token and project id are required")
	}

	opts := []cherrygo.ClientOpt{cherrygo.WithAPIKey(cfg.Token), cherrygo.WithUserAgent(userAgent)}
	if str.IsNotEmpty(cfg.Endpoint) {
		opts = append(opts, cherrygo.WithURL(cfg.Endpoint))
	}
	client, err := cherrygo.NewClient(opts...)
	if err != nil {
		return nil, provider.ErrorNotConfigured.Copy().SetError(err)
	}

	return &Cherry{
		api:       api{regions: client.Regions, plans: client.Plans, images: client.Images, sshKeys: client.SSHKeys, servers: client.Servers},
		projectID: cfg.ProjectID,
		log:       log,
	}, nil
}

func (c *Cherry) Code() string { return Code }

func (c *Cherry) ListRegions(ctx context.Context) ([]providers.Region, error) {
	regions, resp, err := c.api.regions.List(ctx, nil)
	if err != nil {
		return nil, c.wrap("list regions", resp, err)
	}

	out := make([]providers.Region, 0, len(regions))
	for _, r := range regions {
		out = append(out, providers.Region{Ref: r.Slug, Name: r.Location, Country: r.RegionISO2})
	}

	return out, nil
}

// ListPlans answers only plans with an hourly price: a plan Cherry sells by
// the month alone can never back a Nodera Plan (docs/adr/0003).
//
// The public plan list is asked in USD explicitly: the per-team list answers
// in the team's billing currency and ignores the parameter, and a Cherry team
// opened from Türkiye is billed in EUR. The public list carries stock as well,
// so one call is enough.
func (c *Cherry) ListPlans(ctx context.Context) ([]providers.Plan, error) {
	plans, resp, err := c.api.plans.List(ctx, 0, &cherrygo.GetOptions{QueryParams: map[string]string{"currency": "USD"}})
	if err != nil {
		return nil, c.wrap("list plans", resp, err)
	}

	out := make([]providers.Plan, 0, len(plans))
	for _, p := range plans {
		price, ok := hourlyPrice(p.Pricing)
		if !ok {
			c.log.WarnWithFields("plan skipped: no hourly usd price", logger.NewFields().
				Add("provider", Code).Add("plan", p.Slug).Add("pricing", describePricing(p.Pricing)))

			continue
		}

		regions := make([]providers.PlanRegion, 0, len(p.AvailableRegions))
		for _, ar := range p.AvailableRegions {
			if ar.Region == nil || str.IsEmpty(ar.Slug) {
				continue
			}
			regions = append(regions, providers.PlanRegion{RegionRef: ar.Slug, HourlyPrice: price, Stock: ar.StockQty})
		}

		out = append(out, providers.Plan{Ref: p.Slug, Name: p.Name, Hardware: hardware(p.Specs), Regions: regions})
	}

	return out, nil
}

func (c *Cherry) ListImages(ctx context.Context, planRef string) ([]providers.Image, error) {
	images, resp, err := c.api.images.List(ctx, planRef, nil)
	if err != nil {
		return nil, c.wrap("list images", resp, err)
	}

	out := make([]providers.Image, 0, len(images))
	for _, i := range images {
		out = append(out, providers.Image{Ref: i.Slug, Name: i.Name})
	}

	return out, nil
}

// EnsureSSHKey matches on the key material, not the label: the same key
// under two names is one Cherry key. Two orders uploading one key at the same
// moment both find nothing and both create; the second is refused for the
// label, and the key the first uploaded is the answer.
func (c *Cherry) EnsureSSHKey(ctx context.Context, name, publicKey string) (string, error) {
	want := keyMaterial(publicKey)

	ref, err := c.findSSHKey(ctx, want)
	if err != nil || str.IsNotEmpty(ref) {
		return ref, err
	}

	created, resp, err := c.api.sshKeys.Create(ctx, &cherrygo.CreateSSHKey{Label: keyLabel(name, want), Key: strings.TrimSpace(publicKey)})
	if err != nil {
		if ref, findErr := c.findSSHKey(ctx, want); findErr == nil && str.IsNotEmpty(ref) {
			return ref, nil
		}

		return "", c.wrap("create ssh key", resp, err)
	}

	return strconv.Itoa(created.ID), nil
}

// findSSHKey answers the reference of the key with this material, or empty.
func (c *Cherry) findSSHKey(ctx context.Context, material string) (string, error) {
	existing, resp, err := c.api.sshKeys.List(ctx, nil)
	if err != nil {
		return "", c.wrap("list ssh keys", resp, err)
	}
	for _, k := range existing {
		if keyMaterial(k.Key) == material {
			return strconv.Itoa(k.ID), nil
		}
	}

	return "", nil
}

// keyLabel is the label a key is uploaded under. Cherry wants a label unique
// across the account, and the names customers give their keys are not: two
// Teams each with a "laptop" key would refuse each other's orders. A short
// fingerprint of the material keeps one key on one label and two keys apart.
func keyLabel(name, material string) string {
	if len(name) > labelNameLength {
		name = name[:labelNameLength]
	}
	sum := sha256.Sum256([]byte(material))

	return name + "-" + hex.EncodeToString(sum[:])[:labelSuffixLength]
}

func (c *Cherry) CreateServer(ctx context.Context, spec providers.ServerSpec) (string, error) {
	server, resp, err := c.api.servers.Create(ctx, &cherrygo.CreateServer{
		ProjectID: c.projectID,
		Plan:      spec.PlanRef,
		Region:    spec.RegionRef,
		Image:     spec.ImageRef,
		Hostname:  spec.Hostname,
		SSHKeys:   spec.SSHKeyRefs,
		UserData:  userData(spec.UserData),
		Cycle:     cycleHourly,
	})
	if err != nil {
		return "", c.wrap("create server", resp, err)
	}

	return strconv.Itoa(server.ID), nil
}

func (c *Cherry) GetServer(ctx context.Context, ref string) (*providers.Server, error) {
	id, err := serverID(ref)
	if err != nil {
		return nil, err
	}

	server, resp, err := c.api.servers.Get(ctx, id, nil)
	if err != nil {
		return nil, c.wrap("get server", resp, err)
	}

	// Cherry reports a powered-off server as deployed and active; only the
	// power endpoint tells them apart.
	power := ""
	if strings.EqualFold(server.Status, statusDeployed) || strings.EqualFold(server.Status, statusAllocated) {
		if state, _, err := c.api.servers.PowerState(ctx, id); err == nil {
			power = state.Power
		}
	}

	out := &providers.Server{Ref: ref, Status: MapStatus(server.State, server.Status, power)}
	for _, ip := range server.IPAddresses {
		if ip.Type != publicIPType && ip.Type != floatingIPType {
			continue
		}
		switch {
		case ip.AddressFamily == ipv4Family && str.IsEmpty(out.IPv4):
			out.IPv4 = ip.Address
		case ip.AddressFamily == ipv6Family && str.IsEmpty(out.IPv6):
			out.IPv6 = ip.Address
		}
	}

	return out, nil
}

func (c *Cherry) PowerAction(ctx context.Context, ref string, action providers.Action) error {
	id, err := serverID(ref)
	if err != nil {
		return err
	}

	var resp *cherrygo.Response
	switch action {
	case providers.ActionPowerOn:
		_, resp, err = c.api.servers.PowerOn(ctx, id)
	case providers.ActionPowerOff:
		_, resp, err = c.api.servers.PowerOff(ctx, id)
	case providers.ActionReboot:
		_, resp, err = c.api.servers.Reboot(ctx, id)
	default:
		return provider.ErrorRejected.Copy().SetMessage("unknown power action " + string(action))
	}
	if err != nil {
		return c.wrap("power "+string(action), resp, err)
	}

	return nil
}

func (c *Cherry) DeleteServer(ctx context.Context, ref string) error {
	id, err := serverID(ref)
	if err != nil {
		return err
	}

	if resp, err := c.api.servers.Delete(ctx, id); err != nil {
		return c.wrap("delete server", resp, err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// hourlyPrice picks the hourly USD price and nothing else: a Plan priced from
// another currency or unit would be sold at the wrong number. Of cherrygo's
// Pricing struct (teams.go: Price, UnitPrice, Taxed, Currency, Unit) the
// per-unit amount is Price; the SDK's own plan fixture (plans_test.go) lists
// {"price":0.015,"currency":"EUR","unit":"Hourly"} with no unit_price at all,
// and the team fixture carries unit_price 0 next to a non-zero price.
func hourlyPrice(pricing []cherrygo.Pricing) (decimal.Decimal, bool) {
	for _, p := range pricing {
		if strings.EqualFold(p.Unit, pricingHourly) && strings.EqualFold(p.Currency, currencyUSD) && p.Price > 0 {
			return decimal.NewFromFloat32(p.Price), true
		}
	}

	return decimal.Zero, false
}

// describePricing is the "unit/currency" list of what a plan was priced in,
// for the log line that explains why it was skipped.
func describePricing(pricing []cherrygo.Pricing) []string {
	seen := make([]string, 0, len(pricing))
	for _, p := range pricing {
		seen = append(seen, p.Unit+"/"+p.Currency)
	}

	return seen
}

func hardware(s cherrygo.Specs) map[string]any {
	disks := make([]string, 0, len(s.Storage))
	for _, d := range s.Storage {
		disks = append(disks, strconv.Itoa(d.Count)+"x "+d.Name)
	}

	return map[string]any{
		"cpu":       s.CPUs.Name,
		"cpu_count": s.CPUs.Count,
		"cores":     s.CPUs.Cores,
		"ram":       s.Memory.Name,
		"ram_gb":    s.Memory.Total,
		"disks":     disks,
		"bandwidth": s.Bandwidth.Name,
		"nic":       s.NICs.Name,
	}
}

// keyMaterial is "type base64" of an authorized_keys line: the comment is
// not part of the key.
func keyMaterial(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return strings.TrimSpace(line)
	}

	return fields[0] + " " + fields[1]
}

// userData is the cloud-init as Cherry takes it: base64, or nothing.
func userData(script string) string {
	if str.IsEmpty(script) {
		return ""
	}

	return base64.StdEncoding.EncodeToString([]byte(script))
}

func serverID(ref string) (int, error) {
	id, err := strconv.Atoi(ref)
	if err != nil || id <= 0 {
		return 0, provider.ErrorInvalidRef.Copy().SetMessage("server reference must be a positive integer").Add("ref", ref)
	}

	return id, nil
}

// wrap turns an SDK error into a provider rerror with a customer-safe message
// and logs the raw one, which may name balances, plans or Cherry itself. The
// SDK answers an API refusal as a plain error next to the response, so the
// status code is read off the response.
func (c *Cherry) wrap(op string, resp *cherrygo.Response, err error) error {
	fields := logger.NewFields().Add("provider", Code).Add("operation", op).Add("raw", err.Error())

	if resp != nil && resp.Response != nil && resp.StatusCode >= http.StatusMultipleChoices {
		fields.Add("status", resp.StatusCode)
		c.log.WarnWithFields("cherry api refused the request", fields)

		if resp.StatusCode == http.StatusNotFound {
			return provider.ErrorServerNotFound.Copy().SetError(err)
		}

		return provider.ErrorRejected.Copy().SetError(err)
	}

	c.log.ErrorWithFields(err, fields)

	return provider.ErrorUnavailable.Copy().SetError(err)
}
