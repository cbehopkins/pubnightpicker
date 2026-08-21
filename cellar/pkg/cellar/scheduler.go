package cellar

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	if workers <= 0 {
		workers = 1
	}
	return &Scheduler{
		store:      store,
		dispatcher: dispatcher,
		pollDelay:  pollDelay,
		workers:    workers,
	}
}

// Run repeatedly claims and dispatches work until the context is cancelled or execution fails.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.store == nil || s.dispatcher == nil {
		return errors.New("scheduler dependencies are nil")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	capacity := make(chan struct{}, s.workers)
	for range s.workers {
		capacity <- struct{}{}
	}
	dispatchErrors := make(chan error, 1)
	var dispatches sync.WaitGroup
	stop := func(err error) error {
		cancel()
		dispatches.Wait()
		if err != nil {
			return err
		}
		select {
		case err := <-dispatchErrors:
			return err
		default:
			return nil
		}
	}

	for {
		select {
		case <-runCtx.Done():
			return stop(nil)
		case err := <-dispatchErrors:
			return stop(err)
		case <-capacity:
		}

		select {
		case <-runCtx.Done():
			capacity <- struct{}{}
			return stop(nil)
		case err := <-dispatchErrors:
			capacity <- struct{}{}
			return stop(err)
		default:
		}

		cell, ok, err := s.store.ClaimNext(time.Now())
		if err != nil {
			capacity <- struct{}{}
			return stop(fmt.Errorf("claim next cell: %w", err))
		}
		if !ok {
			capacity <- struct{}{}
			timer := time.NewTimer(s.pollDelay)
			select {
			case <-runCtx.Done():
				timer.Stop()
				return stop(nil)
			case err := <-dispatchErrors:
				timer.Stop()
				return stop(err)
			case <-timer.C:
			}
			continue
		}

		dispatches.Add(1)
		go func() {
			defer dispatches.Done()
			defer func() { capacity <- struct{}{} }()
			if err := s.dispatcher.Dispatch(runCtx, cell); err != nil {
				select {
				case dispatchErrors <- fmt.Errorf("dispatch cell %s: %w", cell.ID, err):
				default:
				}
			}
		}()
	}
}
