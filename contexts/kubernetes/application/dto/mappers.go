package dto

import (
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
)

func ToVersions(list []*catalog.Version) []Version {
	out := make([]Version, 0, len(list))
	for _, v := range list {
		out = append(out, Version{Version: v.Version})
	}

	return out
}

func ToRegions(list []*catalog.Region) []Region {
	out := make([]Region, 0, len(list))
	for _, r := range list {
		out = append(out, Region{Code: r.Code, City: r.City, Country: r.Country})
	}

	return out
}

func ToPlans(list []*catalog.Plan) []Plan {
	out := make([]Plan, 0, len(list))
	for _, p := range list {
		out = append(out, Plan{
			Code: p.Code, RegionCode: p.RegionCode, CPUCount: p.CPUCount, RAMGB: p.RAMGB,
			HourlyPrice: p.HourlyPrice.StringFixed(catalog.HourlyScale), MonthlyPrice: p.MonthlyPrice.StringFixed(catalog.MonthlyScale), SoldOut: p.SoldOut,
		})
	}

	return out
}

func ToNodePool(p *cluster.NodePool) NodePool {
	taints := make([]Taint, 0, len(p.Taints))
	for _, t := range p.Taints {
		taints = append(taints, Taint{Key: t.Key, Value: t.Value, Effect: t.Effect})
	}
	labels := p.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	return NodePool{ID: p.ID, Name: p.Name, PlanCode: p.PlanCode, Count: p.Count, Labels: labels, Taints: taints, CreatedAt: p.CreatedAt}
}

func ToCluster(c *cluster.Cluster, pools []*cluster.NodePool, billingStatus string) *Cluster {
	out := &Cluster{
		ID: c.ID, ProjectID: c.ProjectID, Name: c.Name, Region: c.Region, Version: c.Version, Status: string(c.Status),
		Endpoint: c.Address(), BillingStatus: billingStatus, FailureReason: c.FailureReason, CreatedAt: c.CreatedAt, DeletedAt: c.DeletedAt,
		NodePools: make([]NodePool, 0, len(pools)),
	}
	if c.FailureCode != nil {
		code := string(*c.FailureCode)
		out.FailureCode = &code
	}
	for _, p := range pools {
		out.NodePools = append(out.NodePools, ToNodePool(p))
	}

	return out
}

func ToNodes(list []*cluster.Node) []Node {
	out := make([]Node, 0, len(list))
	for _, n := range list {
		node := Node{
			ID: n.ID, PoolID: n.PoolID, Hostname: n.Hostname, Status: string(n.Status), DropletID: n.DropletID, DropletStatus: n.DropletStatus,
			IPv4: n.IPv4, FailureReason: n.FailureReason, CreatedAt: n.CreatedAt, ReadyAt: n.ReadyAt,
		}
		if n.FailureCode != nil {
			code := string(*n.FailureCode)
			node.FailureCode = &code
		}
		out = append(out, node)
	}

	return out
}
