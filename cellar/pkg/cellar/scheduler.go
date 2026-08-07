package cellar

import "time"

// SchedulerStore is the subset of Store operations required by a scheduler.
type SchedulerStore interface {
	ClaimNext(now time.Time) (Cell, bool, error)
}
