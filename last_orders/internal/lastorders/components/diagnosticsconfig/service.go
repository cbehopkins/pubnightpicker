package diagnosticsconfig

// diagnosticsconfig provides a service for managing and observing diagnostics-related configuration settings.
// That is things like silencing notifications globally while we are in teaching/testing phases.

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

const (
	PurposePollOpened      = "poll-opened"
	PurposePollCompleted   = "poll-completed"
	PurposePollRescheduled = "poll-rescheduled"
	PurposeGlobalChat      = "global-chat"
	PurposeEventChat       = "event-chat"
)

type Settings struct {
	SilenceNotifications              bool
	NotifyPollActorWhenSilenced       bool
	KeepChatNotificationsWhenSilenced bool
}

func (s Settings) AllowsLive(purpose, actorUID, recipientUID string) bool {
	if !s.SilenceNotifications {
		return true
	}
	switch purpose {
	case PurposePollOpened, PurposePollCompleted:
		return s.NotifyPollActorWhenSilenced && actorUID != "" && actorUID == recipientUID
	case PurposeGlobalChat, PurposeEventChat:
		return s.KeepChatNotificationsWhenSilenced
	default:
		return false
	}
}

type Source func(context.Context, func(Settings)) error

type Service struct {
	source   Source
	logger   *slog.Logger
	mu       sync.RWMutex
	settings Settings
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

func (s *Service) Snapshot() (Settings, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings, s.known
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
		err := s.source(ctx, func(settings Settings) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.settings = settings
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

func Decode(data map[string]any) (Settings, error) {
	var settings Settings
	for field, target := range map[string]*bool{
		SilenceField:                        &settings.SilenceNotifications,
		"NotifyPollActorWhenSilenced":       &settings.NotifyPollActorWhenSilenced,
		"KeepChatNotificationsWhenSilenced": &settings.KeepChatNotificationsWhenSilenced,
	} {
		value, exists := data[field]
		if !exists {
			continue
		}
		decoded, ok := value.(bool)
		if !ok {
			return Settings{}, fmt.Errorf("%s.%s must be a boolean", DocumentPath, field)
		}
		*target = decoded
	}
	return settings, nil
}

func FirestoreSource(client *firestore.Client) Source {
	return func(ctx context.Context, update func(Settings)) error {
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
