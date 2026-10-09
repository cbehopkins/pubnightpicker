package admindelete

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/truths"

	"google.golang.org/api/iterator"
)

const (
	ListenerAdminDelete cellar.HandlerName = "AdminDeleteRequested"
	watchRetryDelay                        = 5 * time.Second
)

var ErrDisabled = errors.New("admin-delete request service is disabled")

type Config struct {
	Source     Source
	Store      cellar.Store
	Enabled    bool
	RealDelete bool
	Logger     *slog.Logger
}

type Listener struct {
	source     Source
	store      cellar.Store
	enabled    bool
	realDelete bool
	logger     *slog.Logger
	lifecycle.Controller
}

func New(cfg Config) (*Listener, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("admin-delete request source is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("admin-delete request store is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Listener{source: cfg.Source, store: cfg.Store, enabled: cfg.Enabled, realDelete: cfg.RealDelete, logger: cfg.Logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	if !l.enabled {
		return nil
	}
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	pauseDone := make(chan struct{})
	go func() {
		defer close(pauseDone)
		l.watchPause(ctx)
	}()
	defer func() { <-pauseDone }()
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx); err != nil {
			if ctx.Err() != nil || errors.Is(err, iterator.Done) {
				return
			}
			l.logger.Error("admin-delete request watch failed", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(watchRetryDelay):
			}
		}
	}
}

func (l *Listener) watchPause(ctx context.Context) {
	for ctx.Err() == nil {
		stream, err := l.source.WatchPause(ctx)
		if err == nil {
			err = func() error {
				defer stream.Stop()
				lastPaused := true
				known := false
				for {
					if err := stream.Next(); err != nil {
						return err
					}
					paused, err := l.source.IsPaused(ctx)
					if err != nil {
						return err
					}
					if !paused && (!known || lastPaused) {
						if _, err := l.RunOnce(ctx); err != nil && !errors.Is(err, ErrDisabled) {
							return err
						}
					}
					lastPaused = paused
					known = true
				}
			}()
		}
		if ctx.Err() != nil {
			return
		}
		if !errors.Is(err, iterator.Done) && !errors.Is(err, context.Canceled) {
			l.logger.Error("admin-delete kill-switch watch failed", "err", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(watchRetryDelay):
		}
	}
}

func (l *Listener) watchOnce(ctx context.Context) error {
	stream, err := l.source.Watch(ctx)
	if err != nil {
		return err
	}
	defer stream.Stop()

	for {
		documents, err := stream.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		for _, document := range documents {
			if status, ok := document.Data["status"].(string); !ok || !l.isEligible(status) {
				continue
			}
			paused, err := l.source.IsPaused(ctx)
			if err != nil {
				return err
			}
			if paused {
				continue
			}
			if err := l.enqueue(document); err != nil {
				return fmt.Errorf("enqueue admin-delete request %q: %w", document.ID, err)
			}
		}
	}
}

// RunOnce evaluates all eligible requests and durably enqueues their
// idempotent Truth dispatches. It is used by the command's one-shot mode.
func (l *Listener) RunOnce(ctx context.Context) ([]string, error) {
	if !l.enabled {
		return nil, ErrDisabled
	}
	paused, err := l.source.IsPaused(ctx)
	if err != nil {
		return nil, err
	}
	if paused {
		l.logger.Info("admin-delete evaluation skipped while paused")
		return nil, nil
	}
	documents, err := l.source.ListEligible(ctx, l.realDelete)
	if err != nil {
		return nil, err
	}
	requestIDs := make([]string, 0, len(documents))
	for _, document := range documents {
		if status, ok := document.Data["status"].(string); !ok || !l.isEligible(status) {
			continue
		}
		if err := l.enqueue(document); err != nil {
			return nil, fmt.Errorf("enqueue admin-delete request %q: %w", document.ID, err)
		}
		requestIDs = append(requestIDs, document.ID)
	}
	l.logger.Info("admin-delete eligible requests evaluated", "count", len(requestIDs), "real_delete", l.realDelete)
	return requestIDs, nil
}

func (l *Listener) WaitForTerminal(ctx context.Context, requestIDs []string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for _, requestID := range requestIDs {
		for {
			requestStatus, err := l.source.RequestStatus(ctx, requestID)
			if err != nil {
				return err
			}
			if isTerminalStatus(requestStatus) && !l.isEligible(requestStatus) {
				break
			}
			if !l.isEligible(requestStatus) {
				return fmt.Errorf("admin-delete request %q has unknown status %q", requestID, requestStatus)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for admin-delete request %q: %w", requestID, ctx.Err())
			case <-ticker.C:
			}
		}
	}
	return nil
}

func (l *Listener) enqueue(document Document) error {
	truth, err := decodeTruth(document)
	if err != nil {
		return err
	}
	envelope, err := truths.NewEnvelope(truths.AdminDeleteRequestedFanout, truth)
	if err != nil {
		return fmt.Errorf("marshal admin-delete Truth: %w", err)
	}
	listenerName := string(ListenerAdminDelete)
	if l.realDelete {
		listenerName += ".real_delete"
	}
	request, err := firebaseidempotency.NewCellRequest(listenerName, truth.Identity(), envelope)
	if err != nil {
		return fmt.Errorf("build admin-delete idempotency Cell: %w", err)
	}
	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
		return fmt.Errorf("add admin-delete idempotency Cell: %w", err)
	}
	l.logger.Info("admin-delete request Truth observed", "request_id", truth.Identity(), "target_uid", truth.Request.TargetUID)
	return nil
}

func decodeTruth(document Document) (truths.AdminDeleteRequested, error) {
	if document.ID == "" {
		return truths.AdminDeleteRequested{}, fmt.Errorf("request document ID is required")
	}
	data := document.Data
	stringField := func(name string, nullable bool) (string, error) {
		value, exists := data[name]
		if !exists {
			return "", fmt.Errorf("request field %s is missing", name)
		}
		if value == nil && nullable {
			return "", nil
		}
		decoded, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("request field %s must be a string", name)
		}
		return decoded, nil
	}
	targetUID, err := stringField("targetUid", false)
	if err != nil {
		return truths.AdminDeleteRequested{}, err
	}
	targetEmail, err := stringField("targetEmail", true)
	if err != nil {
		return truths.AdminDeleteRequested{}, err
	}
	requestedByUID, err := stringField("requestedByUid", false)
	if err != nil {
		return truths.AdminDeleteRequested{}, err
	}
	requestedByEmail, err := stringField("requestedByEmail", true)
	if err != nil {
		return truths.AdminDeleteRequested{}, err
	}
	reason, err := stringField("reason", false)
	if err != nil {
		return truths.AdminDeleteRequested{}, err
	}
	schemaVersion, ok := integerValue(data["schemaVersion"])
	if !ok {
		return truths.AdminDeleteRequested{}, fmt.Errorf("request field schemaVersion must be an integer")
	}
	createdAt, ok := data["createdAt"].(time.Time)
	if !ok {
		return truths.AdminDeleteRequested{}, fmt.Errorf("request field createdAt must be a Firestore timestamp")
	}
	return truths.AdminDeleteRequested{Request: truths.AdminDeleteRequestSnapshot{
		RequestID:        document.ID,
		TargetUID:        targetUID,
		TargetEmail:      targetEmail,
		RequestedByUID:   requestedByUID,
		RequestedByEmail: requestedByEmail,
		Reason:           reason,
		SchemaVersion:    schemaVersion,
		CreatedAt:        createdAt.UTC(),
	}}, nil
}

func integerValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), int64(int(typed)) == typed
	default:
		return 0, false
	}
}

func isTerminalStatus(status string) bool {
	switch status {
	case "invalid_request", "failed_precondition", "auth_delete_blocked", "dry_run_validated", "auth_deleted", "auth_delete_failed":
		return true
	default:
		return false
	}

}

func (l *Listener) isEligible(status string) bool {
	return status == "pending" || (l.realDelete && status == "dry_run_validated")
}
