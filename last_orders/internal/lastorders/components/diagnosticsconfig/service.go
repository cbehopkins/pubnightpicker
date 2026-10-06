package diagnosticsconfig

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
)

const DocumentPath = "config/diagnostics"
const SilenceField = "SilenceNotifications"

type Source func(context.Context, func(bool)) error

type Service struct {
	source   Source
	logger   *slog.Logger
	mu       sync.RWMutex
	silenced bool
	known    bool
	ready    chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
}

func New(source Source, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{source: source, logger: logger, ready: make(chan struct{})}
}

func (s *Service) Snapshot() (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.silenced, s.known
}

func (s *Service) Ready() <-chan struct{} { return s.ready }

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return fmt.Errorf("diagnostics configuration watcher already started")
	}
	if s.source == nil {
		return fmt.Errorf("diagnostics configuration source is required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	go s.run(runCtx)
	return nil
}

func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	for ctx.Err() == nil {
		err := s.source(ctx, func(silenced bool) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.silenced = silenced
			if !s.known {
				s.known = true
				close(s.ready)
			}
		})
		if ctx.Err() != nil {
			return
		}
		s.logger.Warn("diagnostics configuration watch interrupted; retaining last known value", "err", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) Close() error {
	s.mu.RLock()
	cancel, done := s.cancel, s.done
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}

func Decode(data map[string]any) (bool, error) {
	value, exists := data[SilenceField]
	if !exists {
		return false, nil
	}
	silenced, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s.%s must be a boolean", DocumentPath, SilenceField)
	}
	return silenced, nil
}

func FirestoreSource(client *firestore.Client) Source {
	return func(ctx context.Context, update func(bool)) error {
		iterator := client.Doc(DocumentPath).Snapshots(ctx)
		defer iterator.Stop()
		for {
			snapshot, err := iterator.Next()
			if err != nil {
				return err
			}
			silenced, err := Decode(snapshot.Data())
			if err != nil {
				return err
			}
			update(silenced)
		}
	}
}
