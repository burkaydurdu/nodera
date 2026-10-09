// Package catalog is what the kubernetes context knows of the droplet
// context's Plans and Regions, learned from its catalog events and never
// queried (docs/adr/0014), and the Kubernetes versions Nodera offers.
package catalog

import (
	"time"

	"github.com/shopspring/decimal"
)

// A Node needs room for the kubelet, the container runtime and the CNI before
// a workload: smaller Plans are not offered as Nodes.
const (
	MinNodeCPUs  = 2
	MinNodeRAMGB = 4
)

// PriceScale is the decimals a price carries on the wire.
const (
	HourlyScale  = 4
	MonthlyScale = 2
)

// Region is a Region as the droplet catalog last announced it. SyncedAt is the
// time of the event that shaped the row, so an older copy arriving later
// changes nothing.
type Region struct {
	Code     string
	City     string
	Country  string
	Enabled  bool
	SyncedAt time.Time
}

// Plan is a Plan as the droplet catalog last announced it, by code and Region.
type Plan struct {
	Code         string
	RegionCode   string
	CPUCount     int
	RAMGB        int
	HourlyPrice  decimal.Decimal
	MonthlyPrice decimal.Decimal
	SoldOut      bool
	Enabled      bool
	SyncedAt     time.Time
}

// FitsNode is whether the Plan is large enough to be a Node.
func (p *Plan) FitsNode() bool {
	return p.CPUCount >= MinNodeCPUs && p.RAMGB >= MinNodeRAMGB
}

// Version is one Kubernetes release Nodera offers or offered. Offered false
// opens no new Cluster and keeps running the ones it has.
type Version struct {
	Version   string
	Offered   bool
	CreatedAt time.Time
}
