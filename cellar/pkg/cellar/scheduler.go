package cellar

import (
	"context"
	"time"
)

// SchedulerStore is the subset of Store operations required by a scheduler.
type SchedulerStore interface {
	ClaimNext(now time.Time) (Cell, bool, error)
}

// Dispatcher hands a claimed cell to the next execution stage.
type Dispatcher interface {
	Dispatch(ctx context.Context, cell Cell) error
}

// Scheduler repeatedly claims runnable cells and dispatches them.
type Scheduler struct {
	store      SchedulerStore
	dispatcher Dispatcher
	pollDelay  time.Duration
	workers    int
}

// NewScheduler creates a scheduler engine with the supplied dependencies.
func NewScheduler(store SchedulerStore, dispatcher Dispatcher, workers int, pollDelay time.Duration) *Scheduler {
	if pollDelay <= 0 {
		pollDelay = time.Second
	}
	return &Scheduler{
		store:      store,
		dispatcher: dispatcher,
		pollDelay:  pollDelay,
		workers:    workers,
	}
}

// Run repeatedly claims and dispatches work until the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if s.store == nil || s.dispatcher == nil {
			time.Sleep(s.pollDelay)
			continue
		}

		cell, ok, err := s.store.ClaimNext(time.Now())
		if err != nil || !ok {
			time.Sleep(s.pollDelay)
			continue
		}

		if err := s.dispatcher.Dispatch(ctx, cell); err != nil {
			continue
		}
	}
}
