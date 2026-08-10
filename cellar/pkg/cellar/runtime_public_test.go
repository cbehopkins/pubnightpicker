package cellar

import (
	"context"
	"testing"
	"time"
)

type publicStore struct{}

func (s *publicStore) ClaimNext(now time.Time) (Cell, bool, error) {
	return Cell{}, false, nil
}

type publicDispatcher struct{}

func (d *publicDispatcher) Dispatch(ctx context.Context, cell Cell) error {
	return nil
}

func TestNewWorkerIsAvailableFromPublicPackage(t *testing.T) {
	worker := NewWorker(NewMemoryRegistry())
	if worker == nil {
		t.Fatal("NewWorker() returned nil")
	}
}

func TestNewSchedulerIsAvailableFromPublicPackage(t *testing.T) {
	scheduler := NewScheduler(&publicStore{}, &publicDispatcher{}, 1, time.Millisecond)
	if scheduler == nil {
		t.Fatal("NewScheduler() returned nil")
	}
}
