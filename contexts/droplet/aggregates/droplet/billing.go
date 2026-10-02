package droplet

import "time"

// BillingEvent is a boundary of the meter: usage starts the first time a
// Droplet is active and stops when its deletion is requested or it fails
// after having run. The billing context sums the interval (docs/adr/0006).
type BillingEvent string

const (
	BillingStarted BillingEvent = "started"
	BillingStopped BillingEvent = "stopped"
)

// BillingStartedAt is when usage began; nil for a Droplet never delivered.
func (d *Droplet) BillingStartedAt() *time.Time {
	return d.ActiveAt
}

// BillingStoppedAt is when usage ended, nil while it runs. A Droplet that
// failed after running stopped at the failure even if it was deleted later;
// one that never ran has nothing to stop.
func (d *Droplet) BillingStoppedAt() *time.Time {
	if d.ActiveAt == nil {
		return nil
	}
	if d.FailedAt != nil {
		return d.FailedAt
	}

	return d.DeletionRequest
}

// BillingEvents answers every boundary the Droplet has crossed, in order,
// whether or not it was announced yet; the replay command uses it.
func (d *Droplet) BillingEvents() []BillingEvent {
	events := make([]BillingEvent, 0, 2)
	if d.BillingStartedAt() != nil {
		events = append(events, BillingStarted)
	}
	if d.BillingStoppedAt() != nil {
		events = append(events, BillingStopped)
	}

	return events
}

// ClaimBillingEvents answers the boundaries not announced yet and marks them
// announced at now. The caller persists the Droplet in the transaction that
// moved it and publishes after the commit, so each boundary goes out once.
func (d *Droplet) ClaimBillingEvents(now time.Time) []BillingEvent {
	events := make([]BillingEvent, 0, 2)
	if d.BillingStartedAt() != nil && d.BillingStartedPublishedAt == nil {
		at := now
		d.BillingStartedPublishedAt = &at
		events = append(events, BillingStarted)
	}
	if d.BillingStoppedAt() != nil && d.BillingStoppedPublishedAt == nil {
		at := now
		d.BillingStoppedPublishedAt = &at
		events = append(events, BillingStopped)
	}

	return events
}

// MarkBillingPublished records every crossed boundary as announced, for a
// replay that just published them all.
func (d *Droplet) MarkBillingPublished(now time.Time) {
	if d.BillingStartedAt() != nil && d.BillingStartedPublishedAt == nil {
		at := now
		d.BillingStartedPublishedAt = &at
	}
	if d.BillingStoppedAt() != nil && d.BillingStoppedPublishedAt == nil {
		at := now
		d.BillingStoppedPublishedAt = &at
	}
}
