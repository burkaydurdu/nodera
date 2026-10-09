package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	reasonReleasedByCluster = "released by its cluster"
	// opsKeyName is what the Provider knows Nodera's operational key as.
	opsKeyName = "nodera-ops"
)

// The two refusals of this context's own: the join secret the kubernetes
// context left in Redis is gone, or no operational key is configured.
var (
	errJoinSecretExpired = errors.New("the node's join secret expired before the server was ordered")
	errNoOpsKey          = errors.New("no operational ssh key is configured for nodes")
)

// NodeOrder is what the kubernetes context asks for (docs/adr/0014): one
// Droplet for one Node, named by catalog codes like a customer's request.
type NodeOrder struct {
	NodeID    uuid.UUID
	ClusterID uuid.UUID
	Pool      string
	ProjectID uuid.UUID
	TeamID    uuid.UUID
	Plan      string
	Region    string
	Image     string
	Hostname  string
}

// UserDataStore is where the kubernetes context leaves a Node's cloud-init
// for one hour (docs/adr/0014).
//
//go:generate mockgen -destination=../../mock/handlers/user_data_store.go -package=mockhandlers github.com/piggystats/nodera/contexts/droplet/application/handlers UserDataStore
type UserDataStore interface {
	GetString(ctx context.Context, key string) (string, error)
}

//go:generate mockgen -destination=../../mock/producers/node_rejected.go -package=mockproducers github.com/piggystats/nodera/contexts/droplet/application/handlers INodeRejectedProducer
type INodeRejectedProducer interface {
	Publish(ctx context.Context, nodeID, clusterID uuid.UUID, reason string, at time.Time) error
}

// Nodes opens and releases Node Droplets at the kubernetes context's request.
// A request that cannot become a Droplet at all is answered with a rejection
// and acknowledged: asking again would not help, and the kubernetes context
// fails the Node from it. Once a Droplet exists, its status events tell the
// rest, so a redelivered request opens nothing twice.
type Nodes struct {
	*Droplet
	userData UserDataStore
	rejected INodeRejectedProducer
	opsKey   string
}

func NewNodes(droplets *Droplet, userData UserDataStore, rejected INodeRejectedProducer, opsPublicKey string) *Nodes {
	return &Nodes{Droplet: droplets, userData: userData, rejected: rejected, opsKey: opsPublicKey}
}

// Request opens the Node's Droplet: the catalog and the Team's Billing status
// are checked as for a customer, the cloud-init is taken from Redis, and the
// server is ordered with Nodera's operational key only.
func (h *Nodes) Request(ctx context.Context, order NodeOrder) error {
	if _, err := h.droplets.FindByNodeID(ctx, order.NodeID); err == nil {
		return nil
	} else if !errors.Is(err, droplet.ErrorNotFound) {
		return err
	}

	plan, image, adapter, err := h.prepare(ctx, order)
	if err != nil {
		return h.reject(ctx, order, err)
	}

	userData, err := h.userData.GetString(ctx, fmt.Sprintf(constants.UserDataKeyFormat, order.NodeID))
	if err != nil || str.IsBlank(userData) {
		return h.reject(ctx, order, errJoinSecretExpired)
	}
	if str.IsBlank(h.opsKey) {
		return h.reject(ctx, order, errNoOpsKey)
	}

	d := &droplet.Droplet{
		ProjectID:       order.ProjectID,
		TeamID:          order.TeamID,
		PlanID:          plan.ID,
		PlanCode:        plan.Code,
		RegionCode:      plan.RegionCode,
		ImageCode:       image.Code,
		Hostname:        order.Hostname,
		HourlyPrice:     plan.HourlyPrice,
		MonthlyCapHours: plan.MonthlyCapHours,
		Status:          droplet.StatusProvisioning,
		ProviderID:      plan.Offer.ProviderID,
		SSHKeys:         []droplet.SSHKey{},
		Node:            &droplet.Node{NodeID: order.NodeID, ClusterID: order.ClusterID, Pool: order.Pool},
		CreatedBy:       uuid.Nil,
	}

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	if err := h.droplets.Create(txCtx, d); err != nil {
		_ = h.transactions.Rollback(txCtx)
		// Two deliveries raced past the first lookup: the first Droplet stands.
		if errors.Is(err, droplet.ErrNodeTaken) {
			return nil
		}

		return err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	h.status.Announce(ctx, d, nil)

	provisionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
	defer cancel()
	h.provision(provisionCtx, adapter, d, plan, image, []droplet.SSHKeyInput{{Name: opsKeyName, PublicKey: h.opsKey}}, userData)

	return nil
}

// prepare answers everything the order needs from the catalog, the Team's
// Billing status and the Provider, or the first reason it cannot be met.
func (h *Nodes) prepare(ctx context.Context, order NodeOrder) (*catalog.Plan, *catalog.ImageMapping, providers.Provider, error) {
	if err := h.requireBilling(ctx, order.TeamID, true); err != nil {
		return nil, nil, nil, err
	}
	plan, err := h.catalog.FindPlanForSale(ctx, order.Plan, order.Region)
	if err != nil {
		return nil, nil, nil, err
	}
	if plan.SoldOut {
		return nil, nil, nil, catalog.ErrorPlanSoldOut
	}
	image, err := h.catalog.FindImageMapping(ctx, order.Image, plan.Offer.ProviderID)
	if err != nil {
		return nil, nil, nil, err
	}
	adapter, err := h.registry.Get(plan.ProviderCode)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := h.checkLiveOffer(ctx, adapter, plan); err != nil {
		return nil, nil, nil, err
	}

	return plan, image, adapter, nil
}

// reject answers the kubernetes context with the customer-safe reason and
// acknowledges the request; the raw cause is logged for Nodera.
func (h *Nodes) reject(ctx context.Context, order NodeOrder, cause error) error {
	h.log.ErrorWithFields(cause, logger.NewFields().Add("node", order.NodeID.String()).Add("cluster", order.ClusterID.String()))

	return h.rejected.Publish(ctx, order.NodeID, order.ClusterID, rejectionReason(cause), droplet.Now())
}

// rejectionReason is the sentence the kubernetes context may show. Every
// domain error of this context carries a customer-safe message by rule, and
// so do this context's own two refusals; anything else is the Provider being
// unavailable, worded the way a Droplet's failure is.
func rejectionReason(cause error) string {
	if errors.Is(cause, errJoinSecretExpired) || errors.Is(cause, errNoOpsKey) {
		return cause.Error()
	}
	var rerr rerror.Error
	if errors.As(cause, &rerr) {
		return rerr.Message
	}
	_, reason := customerSafeFailure(cause)

	return reason
}

// Release gives a Node's Droplet back the way a customer's delete would; a
// Droplet already on its way out, or one never opened, needs nothing.
func (h *Nodes) Release(ctx context.Context, nodeID uuid.UUID) error {
	d, err := h.droplets.FindByNodeID(ctx, nodeID)
	if errors.Is(err, droplet.ErrorNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !d.Status.AllowsDelete() {
		return nil
	}

	_, err = h.release(ctx, d.ID, reasonReleasedByCluster)
	if errors.Is(err, droplet.ErrorActionNotAllowed) {
		return nil
	}

	return err
}
