package autocomplete

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/truths"
)

const TimerName cellar.HandlerName = "autocomplete.daily"

const timerComponent = "autocomplete.daily"

type Listener struct {
	store    cellar.Store
	logger   *slog.Logger
	location *time.Location
}

func New(store cellar.Store, logger *slog.Logger) (*Listener, error) {
	if store == nil {
		return nil, fmt.Errorf("cellar store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, fmt.Errorf("load London location: %w", err)
	}
	return &Listener{store: store, logger: logger, location: location}, nil
}

func (listener *Listener) RunOnce(ctx context.Context) error {
	truth := truths.DailyPollAutoCompleteDue{ObservedOn: time.Now().In(listener.location).Format(time.DateOnly)}
	envelope, err := truths.NewEnvelope(truths.DailyPollAutoCompleteDueFanout, truth)
	if err != nil {
		return fmt.Errorf("marshal daily auto-completion truth: %w", err)
	}
	request, err := idempotency.NewCellRequest(timerComponent, truth.Identity(), envelope)
	if err != nil {
		return fmt.Errorf("build daily auto-completion cell: %w", err)
	}
	if _, err := listener.store.Add([]cellar.CellRequest{request}); err != nil {
		return fmt.Errorf("add daily auto-completion cell: %w", err)
	}
	listener.logger.Info("daily auto-completion truth observed", "observed_on", truth.ObservedOn)
	return nil
}
