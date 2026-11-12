package projectionwatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type scriptedStream[T any] struct {
	batch    []T
	err      error
	returned bool
	stopped  atomic.Bool
}

func (s *scriptedStream[T]) Next() ([]T, error) {
	if !s.returned {
		s.returned = true
		return s.batch, nil
	}
	return nil, s.err
}

func (s *scriptedStream[T]) Stop() {
	s.stopped.Store(true)
}

func TestRunRetriesSourceFailureAndMarksEachStreamBaseline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := &scriptedStream[int]{batch: []int{1}, err: io.ErrUnexpectedEOF}
	second := &scriptedStream[int]{batch: nil, err: io.ErrUnexpectedEOF}
	openCount := 0
	var applied []struct {
		baseline bool
		changes  []int
	}

	err := Run(ctx, Config[int]{
		Name:       "test projection",
		RetryDelay: time.Millisecond,
		Logger:     slog.New(slog.DiscardHandler),
		Open: func(streamContext context.Context) (Stream[int], error) {
			openCount++
			if openCount == 1 {
				return first, nil
			}
			if openCount == 2 {
				return second, nil
			}
			return nil, errors.New("unexpected extra stream open")
		},
		Apply: func(_ context.Context, changes []int, baseline bool) error {
			applied = append(applied, struct {
				baseline bool
				changes  []int
			}{baseline: baseline, changes: changes})
			if len(applied) == 2 {
				cancel()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if openCount != 2 {
		t.Fatalf("stream opens = %d, want 2 after source failure", openCount)
	}
	if len(applied) != 2 || !applied[0].baseline || !applied[1].baseline ||
		len(applied[0].changes) != 1 || applied[0].changes[0] != 1 || len(applied[1].changes) != 0 {
		t.Fatalf("applied batches = %#v, want [1] then an empty baseline", applied)
	}
	if !first.stopped.Load() {
		t.Fatal("failed stream was not stopped")
	}
	if !second.stopped.Load() {
		t.Fatal("reconnected stream was not stopped after cancellation")
	}
}

func TestRunReturnsApplierErrorAndStopsStream(t *testing.T) {
	wantErr := errors.New("database unavailable")
	stream := &scriptedStream[int]{batch: []int{1}, err: io.EOF}
	openCount := 0

	err := Run(context.Background(), Config[int]{
		Name:   "test projection",
		Logger: slog.New(slog.DiscardHandler),
		Open: func(context.Context) (Stream[int], error) {
			openCount++
			return stream, nil
		},
		Apply: func(context.Context, []int, bool) error { return wantErr },
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want %v", err, wantErr)
	}
	if openCount != 1 {
		t.Fatalf("stream opens = %d, want 1", openCount)
	}
	if !stream.stopped.Load() {
		t.Fatal("stream was not stopped after applier failure")
	}
}
