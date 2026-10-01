// Package provider is the record of an integrated bare-metal supplier: its
// code, whether it is enabled and the Commission Nodera adds to its Offers by
// default. Credentials never live here; they come from the binary's config.
package provider

import (
	"time"

	"github.com/google/uuid"
)

type Provider struct {
	ID                   uuid.UUID
	Code                 string
	Name                 string
	Enabled              bool
	DefaultCommissionBps int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Providers []*Provider
