package droplet

import "time"

// transitions is the one table of allowed status moves. A Provider may report
// failure or power-off from any live status; a customer may abandon a Droplet
// still provisioning; deletion is terminal.
var transitions = map[Status][]Status{
	StatusProvisioning: {StatusActive, StatusOff, StatusFailed, StatusDeleting},
	StatusActive:       {StatusOff, StatusFailed, StatusDeleting},
	StatusOff:          {StatusActive, StatusFailed, StatusDeleting},
	StatusFailed:       {StatusDeleting, StatusDeleted},
	StatusDeleting:     {StatusDeleted},
	StatusDeleted:      {},
}

func (s Status) CanTransitionTo(to Status) bool {
	for _, allowed := range transitions[s] {
		if allowed == to {
			return true
		}
	}

	return false
}

// IsTransitional says whether the poller should ask the Provider about it.
func (s Status) IsTransitional() bool {
	return s == StatusProvisioning || s == StatusDeleting
}

// AllowsPowerAction: only a Droplet the Provider has delivered can be powered.
func (s Status) AllowsPowerAction() bool {
	return s == StatusActive || s == StatusOff
}

// AllowsDelete: every Droplet may be released, including one still being
// ordered; one being released or already released may not be acted on again.
func (s Status) AllowsDelete() bool {
	return s != StatusDeleting && s != StatusDeleted
}

func IsValidAction(raw string) bool {
	switch Action(raw) {
	case ActionPowerOn, ActionPowerOff, ActionReboot:
		return true
	default:
		return false
	}
}

// TargetStatus is where the Provider should report the Droplet once the
// action has taken effect.
func (a Action) TargetStatus() Status {
	if a == ActionPowerOff {
		return StatusOff
	}

	return StatusActive
}

// Fail moves the Droplet to failed and records why, as a code and as the
// customer-safe sentence.
func (d *Droplet) Fail(code FailureCode, reason string, now time.Time) (*HistoryEntry, error) {
	entry, err := d.Transition(StatusFailed, reason, now)
	if err != nil {
		return nil, err
	}
	d.FailureCode = &code

	return entry, nil
}

// Transition moves the Droplet and stamps the timestamps the status implies.
// It answers the history entry to record; the caller persists both.
func (d *Droplet) Transition(to Status, reason string, now time.Time) (*HistoryEntry, error) {
	if !d.Status.CanTransitionTo(to) {
		return nil, ErrorInvalidTransition.Copy().SetMessage("cannot move a " + string(d.Status) + " droplet to " + string(to))
	}

	from := d.Status
	d.Status = to
	d.UpdatedAt = now
	d.PendingAction = nil

	switch to {
	case StatusActive:
		if d.ActiveAt == nil {
			at := now
			d.ActiveAt = &at
		}
	case StatusDeleting:
		at := now
		d.DeletionRequest = &at
	case StatusDeleted:
		at := now
		d.DeletedAt = &at
		if d.DeletionRequest == nil {
			d.DeletionRequest = &at
		}
	case StatusFailed:
		r := reason
		d.FailureReason = &r
		at := now
		d.FailedAt = &at
	case StatusProvisioning, StatusOff:
	}

	return &HistoryEntry{From: &from, To: to, At: now, Reason: reason}, nil
}
