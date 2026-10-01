// Package catalog is what the customer buys from: Regions, Images and Plans,
// each Plan backed by exactly one Provider Offer per Region and priced as the
// Offer plus Nodera's Commission.
package catalog

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Region struct {
	ID        uuid.UUID
	Code      string
	City      string
	Country   string
	Enabled   bool
	PlanCount int
}

type Regions []*Region

type Image struct {
	ID      uuid.UUID
	Code    string
	Name    string
	Family  string
	Version string
	Enabled bool
}

type Images []*Image

// ImageMapping is an Image together with the reference one Provider knows it by.
type ImageMapping struct {
	Image
	ProviderID  uuid.UUID
	ProviderRef string
}

// Offer is the Provider's listing exactly as its API reports it.
type Offer struct {
	ID          uuid.UUID
	ProviderID  uuid.UUID
	PlanRef     string
	RegionRef   string
	HourlyPrice decimal.Decimal
	Stock       int
	Hardware    map[string]any
	SyncedAt    time.Time
}

type Offers []*Offer

type Plan struct {
	ID                    uuid.UUID
	Code                  string
	RegionID              uuid.UUID
	RegionCode            string
	CPUCount              int
	CPUDescription        string
	RAMGB                 int
	DiskDescription       string
	BandwidthDescription  string
	CommissionBpsOverride *int
	HourlyPrice           decimal.Decimal
	MonthlyCapHours       int
	SoldOut               bool
	Enabled               bool

	Offer              Offer
	ProviderCode       string
	ProviderDefaultBps int
	// Images are the codes of the enabled Images mapped for the Plan's Provider.
	Images []string
}

type Plans []*Plan

func (p *Plan) CommissionBps() int {
	return EffectiveCommissionBps(p.CommissionBpsOverride, p.ProviderDefaultBps)
}

func (p *Plan) MonthlyPrice() decimal.Decimal {
	return MonthlyPrice(p.HourlyPrice, p.MonthlyCapHours)
}
