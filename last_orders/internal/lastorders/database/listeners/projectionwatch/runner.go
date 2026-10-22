package projectionwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const defaultRetryDelay = 5 * time.Second

type Stream[T any] interface {
	Next() ([]T, error)
	Stop()
}

type OpenStream[T any] func(context.Context) (Stream[T], error)

type ApplyBatch[T any] func(ctx context.Context, changes []T, baseline bool) error

type Config[T any] struct {
	Name            string
	Open            OpenStream[T]
	Apply           ApplyBatch[T]
	BaselineApplied func()
	RetryDelay      time.Duration
	Logger          *slog.Logger
}

// Run repeatedly opens and consumes a source stream. Open/read errors retry;
// apply errors are returned to the owner because they may indicate a local
// persistence failure rather than a recoverable source interruption.
func Run[T any](ctx context.Context, cfg Config[T]) error {
	if cfg.Open == nil {
		return errors.New("projection watch stream opener is required")
	}
	if cfg.Apply == nil {
		return errors.New("projection watch batch applier is required")
	}
	if cfg.Name == "" {
		return errors.New("projection watch name is required")
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = defaultRetryDelay
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	for ctx.Err() == nil {
		stream, err := cfg.Open(ctx)
		if err == nil {
			err = consume(ctx, cfg, stream)
			if err == nil || ctx.Err() != nil {
				return nil
			}
			if !isApplyError(err) {
				cfg.Logger.Error("projection watch stream failed", "stream", cfg.Name, "err", err)
			} else {
				return err
			}
		} else if ctx.Err() == nil {
			cfg.Logger.Error("open projection watch stream failed", "stream", cfg.Name, "err", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.RetryDelay):
		}
	}
	return nil
}

type applyError struct {
	err error
}

func (e applyError) Error() string { return e.err.Error() }

func (e applyError) Unwrap() error { return e.err }

func isApplyError(err error) bool {
	var target applyError
	return errors.As(err, &target)
}

func consume[T any](ctx context.Context, cfg Config[T], stream Stream[T]) error {
	defer stream.Stop()

	baseline := true
	for ctx.Err() == nil {
		changes, err := stream.Next()
		if err != nil {
			return err
		}
		if err := cfg.Apply(ctx, changes, baseline); err != nil {
			return applyError{err: fmt.Errorf("apply projection batch: %w", err)}
		}
		if baseline && cfg.BaselineApplied != nil {
			cfg.BaselineApplied()
		}
		baseline = false
	}
	return ctx.Err()
}
