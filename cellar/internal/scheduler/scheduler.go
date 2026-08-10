// Package scheduler provides internal scheduling implementations for cellar.
package scheduler

import (
	"context"
	"time"

	"cellar/pkg/cellar"
)

// Dispatcher hands a claimed cell to the next execution stage.
type Dispatcher interface {
	Dispatch(ctx context.Context, cell cellar.Cell) error
}

// Scheduler repeatedly claims runnable cells and dispatches them.
type Scheduler struct {
	store      cellar.SchedulerStore
	dispatcher Dispatcher
	pollDelay  time.Duration
}

// NewScheduler creates a scheduler engine with the supplied dependencies.
func NewScheduler(store cellar.SchedulerStore, dispatcher Dispatcher, workers int, pollDelay time.Duration) *Scheduler {
	if pollDelay <= 0 {
		pollDelay = time.Second
	}
	return &Scheduler{
		store:      store,
		dispatcher: dispatcher,
		pollDelay:  pollDelay,
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
