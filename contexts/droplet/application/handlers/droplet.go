package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/access"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/application/dto"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
	"github.com/piggystats/nodera/pkg/ratelimit"
)

// Per-User budgets: creating is expensive supplier-side, power actions less so.
const (
	rateLimitWindow = 10 * time.Minute
	createRateLimit = 10
	actionRateLimit = 30
	scopeCreate     = "droplet:create"
	scopeAction     = "droplet:action"
	reasonDeleteAsk = "deletion requested"
	reasonNoServer  = "no server to release"
	reasonAbandoned = "released: deleted while being ordered"

	// provisionTimeout bounds the Provider calls made after the Droplet is
	// committed; they outlive the request so a client disconnect cannot
	// leave a server ordered but unrecorded.
	provisionTimeout = 2 * time.Minute
)

// ProviderRegistry is the slice of the adapter registry the handler consumes.
//
//go:generate mockgen -destination=../../mock/handlers/provider_registry.go -package=mockhandlers github.com/piggystats/nodera/contexts/droplet/application/handlers ProviderRegistry
type ProviderRegistry interface {
	Get(code string) (providers.Provider, error)
}

type Droplet struct {
	log          logger.ILog
	catalog      catalog.IService
	droplets     droplet.IService
	access       access.IService
	providers    provider.IService
	registry     ProviderRegistry
	transactions postgresql.ITransactionManager
	limiter      ratelimit.ILimiter
}

func NewDroplet(
	log logger.ILog,
	catalogService catalog.IService,
	droplets droplet.IService,
	accessService access.IService,
	providerService provider.IService,
	registry ProviderRegistry,
	transactions postgresql.ITransactionManager,
	limiter ratelimit.ILimiter,
) *Droplet {
	return &Droplet{
		log: log, catalog: catalogService, droplets: droplets, access: accessService, providers: providerService,
		registry: registry, transactions: transactions, limiter: limiter,
	}
}

// List answers a Project's Droplets that exist; the Project must be known and
// the caller in its Team, otherwise the Project does not exist for them.
func (h *Droplet) List(ctx context.Context, req *droplet.ListRequest) ([]*dto.Droplet, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.authorizeProject(ctx, projectID, authz.DropletRead); err != nil {
		return nil, err
	}

	droplets, err := h.droplets.FindByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return dto.ToDroplets(droplets), nil
}

// Create follows the spec's order: validate, authorize, check the catalog,
// ask the Provider live, insert and commit, and only then order the server.
// The answer is the provisioning Droplet whatever the Provider then says; the
// outcome arrives through list and detail.
func (h *Droplet) Create(ctx context.Context, req *droplet.CreateRequest) (*dto.Droplet, error) {
	userID := contextutil.CurrentUserID(ctx)
	if err := h.limiter.Allow(ctx, scopeCreate, userID.String(), ratelimit.Rule{Window: rateLimitWindow, Limit: createRateLimit}); err != nil {
		return nil, err
	}

	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.authorizeProject(ctx, projectID, authz.DropletWrite)
	if err != nil {
		return nil, err
	}

	plan, err := h.catalog.FindPlanForSale(ctx, req.Plan, req.Region)
	if err != nil {
		return nil, err
	}
	if plan.SoldOut {
		return nil, catalog.ErrorPlanSoldOut
	}

	image, err := h.catalog.FindImageMapping(ctx, req.Image, plan.Offer.ProviderID)
	if err != nil {
		return nil, err
	}

	keys, err := droplet.ParseSSHKeys(req.SSHKeys)
	if err != nil {
		return nil, err
	}

	adapter, err := h.registry.Get(plan.ProviderCode)
	if err != nil {
		return nil, err
	}

	if err := h.checkLiveOffer(ctx, adapter, plan); err != nil {
		return nil, err
	}

	d := &droplet.Droplet{
		ProjectID:       projectID,
		TeamID:          membership.TeamID,
		PlanID:          plan.ID,
		PlanCode:        plan.Code,
		RegionCode:      plan.RegionCode,
		ImageCode:       image.Code,
		Hostname:        req.Hostname,
		HourlyPrice:     plan.HourlyPrice,
		MonthlyCapHours: plan.MonthlyCapHours,
		Status:          droplet.StatusProvisioning,
		ProviderID:      plan.Offer.ProviderID,
		SSHKeys:         keys,
		CreatedBy:       userID,
	}

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.droplets.Create(txCtx, d); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}

	accepted := dto.ToDroplet(d)
	provisionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
	defer cancel()
	h.provision(provisionCtx, adapter, d, plan, image, req.SSHKeys)

	return accepted, nil
}

// checkLiveOffer compares what the Provider says now with the Offer the Plan
// was priced from. No stock or a vanished Offer flags the Plan sold out; a
// higher price reprices the Plan and refuses; a lower one is Nodera's gain
// and the promise "never more than shown" holds one-way.
func (h *Droplet) checkLiveOffer(ctx context.Context, adapter providers.Provider, plan *catalog.Plan) error {
	livePlans, err := adapter.ListPlans(ctx)
	if err != nil {
		return err
	}

	var live *providers.PlanRegion
	for i := range livePlans {
		if livePlans[i].Ref != plan.Offer.PlanRef {
			continue
		}
		if region, ok := livePlans[i].Region(plan.Offer.RegionRef); ok {
			live = &region
		}

		break
	}

	if live == nil || live.Stock == 0 {
		if err := h.catalog.MarkPlanSoldOut(ctx, plan.ID); err != nil {
			h.log.Error(err)
		}

		return catalog.ErrorPlanSoldOut
	}

	if live.HourlyPrice.GreaterThan(plan.Offer.HourlyPrice) {
		repriced := catalog.HourlyPrice(live.HourlyPrice, plan.CommissionBps())
		h.log.WarnWithFields("plan price changed at creation", logger.NewFields().
			Add("plan", plan.Code).Add("region", plan.RegionCode).
			Add("old_hourly", plan.HourlyPrice.String()).Add("new_hourly", repriced.String()))
		if err := h.catalog.UpdateOffer(ctx, plan.Offer.ID, live.HourlyPrice, live.Stock); err != nil {
			h.log.Error(err)
		}
		if err := h.catalog.UpdatePlanPricing(ctx, plan.ID, repriced, false); err != nil {
			h.log.Error(err)
		}

		return catalog.ErrorPlanPriceChanged
	}

	return nil
}

// provision orders the server after the Droplet is committed. A Provider
// failure fails the Droplet with a customer-safe reason; the raw error is
// logged for Nodera. It is synchronous so the outcome is recorded before the
// request returns.
func (h *Droplet) provision(ctx context.Context, adapter providers.Provider, d *droplet.Droplet, plan *catalog.Plan, image *catalog.ImageMapping, keys []droplet.SSHKeyInput) {
	refs := make([]string, 0, len(keys))
	for _, k := range keys {
		ref, err := adapter.EnsureSSHKey(ctx, k.Name, k.PublicKey)
		if err != nil {
			h.fail(ctx, d.ID, err)

			return
		}
		refs = append(refs, ref)
	}

	ref, err := adapter.CreateServer(ctx, providers.ServerSpec{
		PlanRef:    plan.Offer.PlanRef,
		RegionRef:  plan.Offer.RegionRef,
		ImageRef:   image.ProviderRef,
		Hostname:   d.Hostname,
		SSHKeyRefs: refs,
	})
	if err != nil {
		h.fail(ctx, d.ID, err)

		return
	}

	err = h.update(ctx, d.ID, func(ctx context.Context, current *droplet.Droplet) error {
		current.ProviderRef = &ref
		// The customer abandoned it while the Provider was still answering:
		// the reference is kept for the books and the server released.
		if current.Status == droplet.StatusDeleted {
			if err := adapter.DeleteServer(ctx, ref); err != nil && !errors.Is(err, provider.ErrorServerNotFound) {
				h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("provider_ref", ref).Add("outcome", reasonAbandoned))
			}
		}

		return h.droplets.Save(ctx, current)
	})
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("provider_ref", ref))
	}
}

func (h *Droplet) fail(ctx context.Context, id uuid.UUID, cause error) {
	h.log.ErrorWithFields(cause, logger.NewFields().Add("droplet", id.String()))

	err := h.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		// Deleted in the meantime: the customer's word stands.
		if !current.Status.CanTransitionTo(droplet.StatusFailed) {
			return nil
		}

		return h.droplets.Transition(ctx, current, droplet.StatusFailed, customerSafeReason(cause))
	})
	if err != nil {
		h.log.Error(err)
	}
}

// update is the one read-modify-write on a Droplet: the row is locked for the
// transaction, mutate sees the current row and persists what it decides, and
// the commit releases the lock. The poller does the same, so neither side can
// overwrite the other's change.
func (h *Droplet) update(ctx context.Context, id uuid.UUID, mutate func(ctx context.Context, current *droplet.Droplet) error) error {
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}

	current, err := h.droplets.FindByIDForUpdate(txCtx, id)
	if err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}
	if err := mutate(txCtx, current); err != nil {
		_ = h.transactions.Rollback(txCtx)

		return err
	}

	return h.transactions.Commit(txCtx)
}

// customerSafeReason keeps only the message of a provider-domain rerror;
// anything else is a generic unavailability.
func customerSafeReason(err error) string {
	var rerr rerror.Error
	if errors.As(err, &rerr) && rerr.Domain == provider.Domain {
		return rerr.Message
	}

	return provider.ErrorUnavailable.Message
}

func (h *Droplet) Get(ctx context.Context, req *droplet.GetRequest) (*dto.Droplet, error) {
	d, err := h.findAuthorized(ctx, req.DropletID, authz.DropletRead)
	if err != nil {
		return nil, err
	}

	history, err := h.droplets.ListHistory(ctx, d.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToDropletWithHistory(d, history), nil
}

// Action records a power request as pending and then forwards it; the
// resulting status comes from the poller. Allowed from active and off only,
// one at a time. The marker is committed before the Provider is asked, so a
// poll landing in between sees it; a refused request clears it again.
func (h *Droplet) Action(ctx context.Context, req *droplet.ActionRequest) (*dto.Droplet, error) {
	userID := contextutil.CurrentUserID(ctx)
	if err := h.limiter.Allow(ctx, scopeAction, userID.String(), ratelimit.Rule{Window: rateLimitWindow, Limit: actionRateLimit}); err != nil {
		return nil, err
	}

	authorized, err := h.findAuthorized(ctx, req.DropletID, authz.DropletWrite)
	if err != nil {
		return nil, err
	}

	action := droplet.Action(req.Type)
	var d *droplet.Droplet
	err = h.update(ctx, authorized.ID, func(ctx context.Context, current *droplet.Droplet) error {
		if !current.Status.AllowsPowerAction() || current.PendingAction != nil || current.ProviderRef == nil {
			return droplet.ErrorActionNotAllowed
		}
		current.PendingAction = &action
		d = current

		return h.droplets.Save(ctx, current)
	})
	if err != nil {
		return nil, err
	}

	adapter, err := h.adapterFor(ctx, d)
	if err != nil {
		h.clearPending(ctx, d.ID, action)

		return nil, err
	}
	if err := adapter.PowerAction(ctx, *d.ProviderRef, providers.Action(action)); err != nil {
		h.clearPending(ctx, d.ID, action)

		return nil, err
	}

	return dto.ToDroplet(d), nil
}

// clearPending takes back the marker of a request the Provider never took;
// only that request's own marker, should the poller have moved on already.
func (h *Droplet) clearPending(ctx context.Context, id uuid.UUID, action droplet.Action) {
	err := h.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		if current.PendingAction == nil || *current.PendingAction != action {
			return nil
		}
		current.PendingAction = nil

		return h.droplets.Save(ctx, current)
	})
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", id.String()).Add("action", string(action)))
	}
}

// Delete stops the meter at once and asks the Provider to release the server;
// the poller (or the absence of a server) finishes the job. Allowed from every
// status but deleting and deleted: a customer may abandon a Droplet still
// being ordered. Never a hard delete.
func (h *Droplet) Delete(ctx context.Context, req *droplet.DeleteRequest) (*dto.Droplet, error) {
	authorized, err := h.findAuthorized(ctx, req.DropletID, authz.DropletWrite)
	if err != nil {
		return nil, err
	}

	var d *droplet.Droplet
	err = h.update(ctx, authorized.ID, func(ctx context.Context, current *droplet.Droplet) error {
		if !current.Status.AllowsDelete() {
			return droplet.ErrorActionNotAllowed
		}
		if err := h.droplets.Transition(ctx, current, droplet.StatusDeleting, reasonDeleteAsk); err != nil {
			return err
		}
		d = current
		if current.ProviderRef != nil {
			return nil
		}

		return h.droplets.Transition(ctx, current, droplet.StatusDeleted, reasonNoServer)
	})
	if err != nil {
		return nil, err
	}
	if d.ProviderRef == nil {
		return dto.ToDroplet(d), nil
	}

	adapter, err := h.adapterFor(ctx, d)
	if err != nil {
		return nil, err
	}
	if err := adapter.DeleteServer(ctx, *d.ProviderRef); err != nil {
		// The Droplet stays deleting; the poller asks the Provider again on
		// every run until the server is gone.
		h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()))
	}

	return dto.ToDroplet(d), nil
}

// authorizeProject answers the caller's membership in the Project's Team when
// their Role holds the Permission (docs/adr/0005). A stranger and an unknown
// Project are the same 404; a member without the Permission learns the
// Project exists and gets a 403.
func (h *Droplet) authorizeProject(ctx context.Context, projectID uuid.UUID, permission authz.Permission) (*access.Membership, error) {
	membership, err := h.access.FindProjectMembership(ctx, projectID, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if !membership.Allows(permission) {
		return nil, droplet.ErrorForbidden
	}

	return membership, nil
}

// findAuthorized answers the Droplet when the caller is in its Team with a
// Role holding the Permission; a stranger's Droplet does not exist for them.
func (h *Droplet) findAuthorized(ctx context.Context, rawID string, permission authz.Permission) (*droplet.Droplet, error) {
	d, err := h.droplets.FindByID(ctx, uuid.MustParse(rawID))
	if err != nil {
		return nil, err
	}

	membership, err := h.access.FindTeamMembership(ctx, d.TeamID, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if membership == nil {
		return nil, droplet.ErrorNotFound
	}
	if !membership.Allows(permission) {
		return nil, droplet.ErrorForbidden
	}

	return d, nil
}

func (h *Droplet) adapterFor(ctx context.Context, d *droplet.Droplet) (providers.Provider, error) {
	p, err := h.providers.FindByID(ctx, d.ProviderID)
	if err != nil {
		return nil, err
	}

	return h.registry.Get(p.Code)
}
