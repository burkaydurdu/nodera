// Package dto is what the droplet API answers. Money is a decimal string in
// USD excluding taxes; Provider names appear nowhere.
package dto

import (
	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
)

type Region struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	City      string    `json:"city"`
	Country   string    `json:"country"`
	PlanCount int       `json:"plan_count"`
}

func ToRegions(regions catalog.Regions) []*Region {
	out := make([]*Region, 0, len(regions))
	for _, r := range regions {
		out = append(out, &Region{ID: r.ID, Code: r.Code, City: r.City, Country: r.Country, PlanCount: r.PlanCount})
	}

	return out
}

type Plan struct {
	ID                   uuid.UUID `json:"id"`
	Code                 string    `json:"code"`
	RegionCode           string    `json:"region_code"`
	CPUCount             int       `json:"cpu_count"`
	CPUDescription       string    `json:"cpu_description"`
	RAMGB                int       `json:"ram_gb"`
	DiskDescription      string    `json:"disk_description"`
	BandwidthDescription string    `json:"bandwidth_description"`
	HourlyPrice          string    `json:"hourly_price" example:"0.4920"`
	MonthlyPrice         string    `json:"monthly_price" example:"330.63"`
	SoldOut              bool      `json:"sold_out"`
	Images               []string  `json:"images"`
}

func ToPlan(p *catalog.Plan) *Plan {
	images := p.Images
	if images == nil {
		images = []string{}
	}

	return &Plan{
		ID:                   p.ID,
		Code:                 p.Code,
		RegionCode:           p.RegionCode,
		CPUCount:             p.CPUCount,
		CPUDescription:       p.CPUDescription,
		RAMGB:                p.RAMGB,
		DiskDescription:      p.DiskDescription,
		BandwidthDescription: p.BandwidthDescription,
		HourlyPrice:          p.HourlyPrice.StringFixed(catalog.HourlyScale),
		MonthlyPrice:         p.MonthlyPrice().StringFixed(catalog.MonthlyScale),
		SoldOut:              p.SoldOut,
		Images:               images,
	}
}

func ToPlans(plans catalog.Plans) []*Plan {
	out := make([]*Plan, 0, len(plans))
	for _, p := range plans {
		out = append(out, ToPlan(p))
	}

	return out
}

type Image struct {
	ID      uuid.UUID `json:"id"`
	Code    string    `json:"code"`
	Name    string    `json:"name"`
	Family  string    `json:"family"`
	Version string    `json:"version"`
}

func ToImages(images catalog.Images) []*Image {
	out := make([]*Image, 0, len(images))
	for _, i := range images {
		out = append(out, &Image{ID: i.ID, Code: i.Code, Name: i.Name, Family: i.Family, Version: i.Version})
	}

	return out
}
