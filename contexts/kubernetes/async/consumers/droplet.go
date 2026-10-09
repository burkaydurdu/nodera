package consumers

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/kubernetes/application/handlers"
	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
	"github.com/piggystats/nodera/contexts/kubernetes/async/events"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

// NodesHandler is the slice of the application the droplet-events consumer
// delegates to for the Nodes; it binds and validates, the handler decides.
//
//go:generate mockgen -destination=../../mock/consumers/nodes_handler.go -package=mockconsumers github.com/piggystats/nodera/contexts/kubernetes/async/consumers NodesHandler
type NodesHandler interface {
	DropletChanged(ctx context.Context, change handlers.DropletChange) error
	Rejected(ctx context.Context, nodeID uuid.UUID, reason string) error
}

// DropletEvents applies the droplet context's events (docs/adr/0014): the
// status of a Node's Droplet, a Node no Droplet could be opened for, and the
// catalog read model.
type DropletEvents struct {
	nodes   NodesHandler
	catalog catalog.IService
}

func NewDropletEvents(nodes NodesHandler, catalogService catalog.IService) *DropletEvents {
	return &DropletEvents{nodes: nodes, catalog: catalogService}
}

// Consume dispatches on the routing key; a key this context does not consume,
// and a Droplet status without a Node id, are acknowledged and ignored.
func (c *DropletEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()
	if correlationID := message.GetCorrelationID(); str.IsNotEmpty(correlationID) {
		ctx = context.WithValue(ctx, contextConstants.CorrelationID, correlationID)
	}

	switch message.RoutingKey {
	case constants.DropletStatusChanged:
		var event events.DropletStatusChangedEvent
		if err := decodeCatalog(message, &event); err != nil {
			return err
		}
		p := event.Payload
		if p.NodeID == nil || *p.NodeID == uuid.Nil {
			return nil
		}
		if p.DropletID == uuid.Nil || str.IsBlank(p.Status) || p.ChangedAt.IsZero() {
			return catalog.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the droplet id, the status or its time")
		}

		return c.nodes.DropletChanged(ctx, handlers.DropletChange{
			NodeID: *p.NodeID, DropletID: p.DropletID, Status: p.Status, IPv4: p.IPv4,
			FailureCode: p.FailureCode, FailureReason: p.FailureReason, ChangedAt: p.ChangedAt,
		})

	case constants.NodeRejected:
		var event events.NodeRejectedEvent
		if err := decodeCatalog(message, &event); err != nil {
			return err
		}
		if event.Payload.NodeID == uuid.Nil {
			return catalog.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the node id")
		}

		return c.nodes.Rejected(ctx, event.Payload.NodeID, event.Payload.Reason)

	case constants.CatalogRegionChanged:
		var event events.CatalogRegionChangedEvent
		if err := decodeCatalog(message, &event); err != nil {
			return err
		}
		p := event.Payload
		if str.IsBlank(p.Code) || p.SyncedAt.IsZero() {
			return catalog.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the region code or its time")
		}

		return c.catalog.UpsertRegion(ctx, &catalog.Region{Code: p.Code, City: p.City, Country: p.Country, Enabled: p.Enabled, SyncedAt: p.SyncedAt})

	case constants.CatalogPlanChanged:
		var event events.CatalogPlanChangedEvent
		if err := decodeCatalog(message, &event); err != nil {
			return err
		}
		p := event.Payload
		if str.IsBlank(p.Code) || str.IsBlank(p.RegionCode) || p.SyncedAt.IsZero() {
			return catalog.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the plan code, its region or its time")
		}
		hourly, err := decimal.NewFromString(p.HourlyPrice)
		if err != nil {
			return catalog.ErrorMalformedEvent.Copy().SetError(err)
		}
		monthly, err := decimal.NewFromString(p.MonthlyPrice)
		if err != nil {
			return catalog.ErrorMalformedEvent.Copy().SetError(err)
		}

		return c.catalog.UpsertPlan(ctx, &catalog.Plan{
			Code: p.Code, RegionCode: p.RegionCode, CPUCount: p.CPUCount, RAMGB: p.RAMGB, HourlyPrice: hourly, MonthlyPrice: monthly,
			SoldOut: p.SoldOut, Enabled: p.Enabled, SyncedAt: p.SyncedAt,
		})

	default:
		return nil
	}
}

func decodeCatalog(message *rabbitmq.Message, into any) error {
	if err := json.Unmarshal(message.Payload, into); err != nil {
		return catalog.ErrorMalformedEvent.Copy().SetError(err)
	}

	return nil
}
