package invoice

import "time"

// Dunning steps run from the first failed Payment of an Invoice: retries on
// days 1, 3 and 7, suspension on the day-7 failure, deletion warnings on days
// 14 and 20 and the deletion request on day 21. An Invoice records the last
// step carried out.
const (
	StepNone = iota
	StepRetryDay1
	StepRetryDay3
	StepRetryDay7
	StepWarnDay14
	StepWarnDay20
	StepDeleteDay21
)

// StepSuspend is the retry whose failure suspends the Team.
const StepSuspend = StepRetryDay7

var stepDays = [...]int{StepNone: 0, StepRetryDay1: 1, StepRetryDay3: 3, StepRetryDay7: 7, StepWarnDay14: 14, StepWarnDay20: 20, StepDeleteDay21: 21}

// StepDay is how many days after the first failure a step falls due.
func StepDay(step int) int {
	return stepDays[step]
}

// StepAt is when a step falls due.
func StepAt(failedAt time.Time, step int) time.Time {
	return failedAt.AddDate(0, 0, stepDays[step])
}

// DueStep is the latest step due at now; StepNone before the first retry.
func DueStep(failedAt, now time.Time) int {
	due := StepNone
	for step := StepRetryDay1; step <= StepDeleteDay21; step++ {
		if !now.Before(StepAt(failedAt, step)) {
			due = step
		}
	}

	return due
}

// IsRetry reports whether a step charges the card again.
func IsRetry(step int) bool {
	return step >= StepRetryDay1 && step <= StepRetryDay7
}

// NextRetryAt is when the retry after `step` runs; nil once the day-7 retry
// has been made.
func NextRetryAt(failedAt time.Time, step int) *time.Time {
	if step >= StepRetryDay7 {
		return nil
	}
	next := StepAt(failedAt, step+1)

	return &next
}

// DeletionAt is when a suspended Team's Droplets are deleted.
func DeletionAt(failedAt time.Time) time.Time {
	return StepAt(failedAt, StepDeleteDay21)
}
