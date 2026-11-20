package meetcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	entryLifetime = 6 * time.Hour
	retryDelay    = 5 * time.Second
	initialWait   = 5 * time.Second
)

type Service struct {
	store       *Store
	source      Source
	logger      *slog.Logger
	now         func() time.Time
	diagnostics bool
	mu          sync.Mutex
	entries     map[string]*entry
	closed      bool
}

type entry struct {
	id            string
	owner         string
	validUntil    time.Time
	ctx           context.Context
	cancel        context.CancelFunc
	changed       chan struct{}
	ready         bool
	available     bool
	missing       bool
	retired       bool
	failureLogged bool
	watch         *sourceWatch
}

type sourceWatch struct {
	stream Stream
	cancel context.CancelFunc
	once   sync.Once
}

func (watch *sourceWatch) stop() {
	if watch == nil {
		return
	}
	watch.once.Do(func() {
		watch.cancel()
		watch.stream.Stop()
	})
}

func NewService(store *Store, source Source, logger *slog.Logger, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("meet projection store is required")
	}
	if source == nil {
		return nil, fmt.Errorf("meet source is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:       store,
		source:      source,
		logger:      logger,
		now:         now,
		diagnostics: os.Getenv("LAST_ORDERS_MEET_PROJECTION_DIAGNOSTICS") == "true",
		entries:     make(map[string]*entry),
	}, nil
}

func (s *Service) Get(ctx context.Context, meetID string) (Snapshot, error) {
	if meetID == "" {
		return Snapshot{}, ErrNotFound
	}
	current, err := s.admit(meetID)
	if err != nil {
		return Snapshot{}, err
	}
	timer := time.NewTimer(initialWait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		if current.missing {
			s.mu.Unlock()
			return Snapshot{}, ErrNotFound
		}
		if current.retired {
			s.mu.Unlock()
			return Snapshot{}, ErrUnavailable
		}
		if current.ready && current.available {
			snapshot, readErr := s.store.Get(ctx, meetID, current.owner)
			if readErr == nil {
				s.mu.Unlock()
				return snapshot, nil
			}
			if ctx.Err() != nil {
				s.mu.Unlock()
				return Snapshot{}, ctx.Err()
			}
			current.available = false
			watch := current.watch
			signal(current)
			s.mu.Unlock()
			if watch != nil {
				watch.stop()
			}
			s.markUnavailable(current, readErr)
			continue
		}
		changed := current.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-timer.C:
			return Snapshot{}, ErrUnavailable
		case <-changed:
		}
	}
}

func (s *Service) admit(meetID string) (*entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrUnavailable
	}
	if current := s.entries[meetID]; current != nil && !current.retired {
		return current, nil
	}
	owner := uuid.NewString()
	validUntil := s.now().Add(entryLifetime)
	storeCtx, cancelStore := context.WithTimeout(context.Background(), 5*time.Second)
	err := s.store.Admit(storeCtx, meetID, owner, validUntil)
	cancelStore()
	if err != nil {
		s.logger.Warn("Meet projection admission failed", "meet_id", maskedMeetID(meetID), "error_type", fmt.Sprintf("%T", err))
		if s.diagnostics {
			s.logger.Info("Meet projection admission failure detail", "meet_id", maskedMeetID(meetID), "err", err)
		}
		return nil, fmt.Errorf("%w: persist projection admission: %v", ErrUnavailable, err)
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	current := &entry{
		id:         meetID,
		owner:      owner,
		validUntil: validUntil,
		ctx:        watchCtx,
		cancel:     cancel,
		changed:    make(chan struct{}),
	}
	s.entries[meetID] = current
	if s.diagnostics {
		s.logger.Info("Meet projection admitted", "meet_id", maskedMeetID(meetID), "valid_until", validUntil)
	}
	go s.observe(current)
	return current, nil
}

func (s *Service) observe(current *entry) {
	for current.ctx.Err() == nil {
		watchCtx, cancelWatch := context.WithCancel(current.ctx)
		stream, err := s.source.Watch(watchCtx, current.id)
		if err != nil {
			cancelWatch()
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		watch := &sourceWatch{stream: stream, cancel: cancelWatch}
		stopStream := func() {
			watch.stop()
			s.clearStream(current, watch)
		}
		s.setStream(current, watch)
		document, err := stream.Next()
		if err != nil {
			stopStream()
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		if !document.Exists {
			stopStream()
			s.retireMissing(current)
			return
		}
		projection, err := ProjectionFromDocument(document)
		if err != nil {
			stopStream()
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		if _, err := s.apply(current, projection, document.Version); err != nil {
			stopStream()
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		s.markAvailable(current)

		for current.ctx.Err() == nil {
			document, err = stream.Next()
			if err != nil {
				stopStream()
				s.markUnavailable(current, err)
				break
			}
			if !document.Exists {
				stopStream()
				s.retireMissing(current)
				return
			}
			projection, err = ProjectionFromDocument(document)
			if err == nil {
				_, err = s.apply(current, projection, document.Version)
			}
			if err != nil {
				stopStream()
				s.markUnavailable(current, err)
				break
			}
		}
		stopStream()
		if !waitRetry(current.ctx) {
			return
		}
	}
}

func (s *Service) apply(current *entry, projection Projection, sourceVersion time.Time) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[current.id] != current || current.retired {
		return Snapshot{}, ErrRetired
	}
	snapshot, err := s.store.Apply(current.ctx, current.id, current.owner, projection, sourceVersion)
	if errors.Is(err, ErrStale) {
		return Snapshot{}, nil
	}
	return snapshot, err
}

func (s *Service) setStream(current *entry, watch *sourceWatch) {
	s.mu.Lock()
	if s.entries[current.id] != current || current.retired {
		s.mu.Unlock()
		watch.stop()
		return
	}
	current.watch = watch
	if s.diagnostics {
		s.logger.Info("Meet projection watch opened", "meet_id", maskedMeetID(current.id))
	}
	s.mu.Unlock()
}

func (s *Service) clearStream(current *entry, watch *sourceWatch) {
	s.mu.Lock()
	if current.watch == watch {
		current.watch = nil
	}
	s.mu.Unlock()
}

func (s *Service) markAvailable(current *entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[current.id] != current || current.retired {
		return
	}
	current.ready = true
	current.available = true
	current.failureLogged = false
	signal(current)
	if s.diagnostics {
		s.logger.Info("Meet projection baseline committed", "meet_id", maskedMeetID(current.id))
	}
}

func (s *Service) markUnavailable(current *entry, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[current.id] != current || current.retired {
		return
	}
	current.available = false
	signal(current)
	if !current.failureLogged {
		s.logger.Warn("Meet projection source unavailable", "meet_id", maskedMeetID(current.id), "error_type", fmt.Sprintf("%T", err))
		current.failureLogged = true
	}
	if s.diagnostics {
		s.logger.Info("Meet projection baseline attempt failed", "meet_id", maskedMeetID(current.id), "err", err)
	}
}

func (s *Service) retireMissing(current *entry) {
	s.mu.Lock()
	if s.entries[current.id] != current || current.retired {
		s.mu.Unlock()
		return
	}
	delete(s.entries, current.id)
	current.missing = true
	current.retired = true
	current.cancel()
	watch := current.watch
	current.watch = nil
	signal(current)
	if s.diagnostics {
		s.logger.Info("Meet projection retired after source deletion", "meet_id", maskedMeetID(current.id))
	}
	s.mu.Unlock()
	watch.stop()
	_ = s.store.DeleteIfOwner(context.Background(), current.id, current.owner)
}

func (s *Service) EvictExpired(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	entries, err := s.store.Expired(ctx, now)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	watches := make([]*sourceWatch, 0, len(entries))
	for _, expired := range entries {
		if current := s.entries[expired.ID]; current != nil && current.owner == expired.Owner {
			delete(s.entries, expired.ID)
			current.retired = true
			current.cancel()
			if current.watch != nil {
				watches = append(watches, current.watch)
				current.watch = nil
			}
			signal(current)
			if s.diagnostics {
				s.logger.Info("Meet projection evicted", "meet_id", maskedMeetID(expired.ID))
			}
		}
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.stop()
	}
	for _, expired := range entries {
		if err := s.store.DeleteIfOwner(ctx, expired.ID, expired.Owner); err != nil {
			return fmt.Errorf("evict Meet projection %q: %w", maskedMeetID(expired.ID), err)
		}
	}
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	watches := make([]*sourceWatch, 0, len(s.entries))
	for id, current := range s.entries {
		delete(s.entries, id)
		current.retired = true
		current.cancel()
		if current.watch != nil {
			watches = append(watches, current.watch)
			current.watch = nil
		}
		signal(current)
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.stop()
	}
}

func waitRetry(ctx context.Context) bool {
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func signal(current *entry) {
	close(current.changed)
	current.changed = make(chan struct{})
}

func maskedMeetID(meetID string) string {
	digest := sha256.Sum256([]byte(meetID))
	return fmt.Sprintf("%x", digest[:4])
}
