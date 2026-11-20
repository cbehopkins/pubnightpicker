package currentmeetcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
)

const (
	entryLifetime = 6 * time.Hour
	retryDelay    = 5 * time.Second
	initialWait   = 5 * time.Second
)

type Service struct {
	store          *Store
	source         Source
	venues         *venuecache.Store
	hub            *sse.Hub
	logger         *slog.Logger
	now            func() time.Time
	location       *time.Location
	diagnostics    bool
	mu             sync.Mutex
	entries        map[string]*entry
	closed         bool
	venueAvailable bool
}

type entry struct {
	groupID       string
	date          string
	owner         string
	validUntil    time.Time
	ctx           context.Context
	cancel        context.CancelFunc
	changed       chan struct{}
	available     bool
	retired       bool
	failureLogged bool
	watch         *sourceWatch
	venueIDs      map[string]struct{}
	lastDigest    string
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

func NewService(store *Store, source Source, venues *venuecache.Store, hub *sse.Hub, logger *slog.Logger, now func() time.Time) (*Service, error) {
	if store == nil || source == nil || venues == nil || hub == nil {
		return nil, fmt.Errorf("current-Meets store, source, Venue store, and stream hub are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, fmt.Errorf("load London time zone: %w", err)
	}
	return &Service{
		store: store, source: source, venues: venues, hub: hub, logger: logger, now: now,
		location: location, diagnostics: os.Getenv("LAST_ORDERS_CURRENT_MEET_DIAGNOSTICS") == "true",
		entries: make(map[string]*entry), venueAvailable: true,
	}, nil
}

func (s *Service) CurrentDate() string {
	return s.now().In(s.location).Format(time.DateOnly)
}

func (s *Service) Get(ctx context.Context, groupID string) (Snapshot, error) {
	if groupID == "" {
		return Snapshot{}, ErrUnavailable
	}
	date := s.CurrentDate()
	if err := s.RetireBefore(ctx, date); err != nil {
		return Snapshot{}, fmt.Errorf("retire prior current-Meets date: %w", err)
	}
	current, err := s.admit(groupID, date)
	if err != nil {
		return Snapshot{}, err
	}
	timer := time.NewTimer(initialWait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		if current.retired || s.entries[entryKey(groupID, date)] != current {
			s.mu.Unlock()
			return Snapshot{}, ErrUnavailable
		}
		if !s.venueAvailable && len(current.venueIDs) > 0 {
			changed := current.changed
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			case <-timer.C:
				return Snapshot{}, ErrUnavailable
			case <-changed:
			}
			continue
		}
		if current.available {
			snapshot, readErr := s.store.Snapshot(ctx, groupID, current.owner, s.venues)
			if readErr == nil && date == s.CurrentDate() {
				current.lastDigest = digest(snapshot)
				s.mu.Unlock()
				return snapshot, nil
			}
			if readErr == nil {
				readErr = ErrRetired
			}
			if ctx.Err() != nil {
				s.mu.Unlock()
				return Snapshot{}, ctx.Err()
			}
			current.available = false
			watch := current.watch
			s.signal(current)
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

func (s *Service) Renew(ctx context.Context, groupID, date string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if date != s.CurrentDate() {
		return ErrRetired
	}
	current := s.entries[entryKey(groupID, date)]
	if current == nil || current.retired || !current.available {
		return ErrRetired
	}
	validUntil := s.now().Add(entryLifetime)
	if err := s.store.Renew(ctx, groupID, date, current.owner, validUntil); err != nil {
		return err
	}
	current.validUntil = validUntil
	return nil
}

func (s *Service) NotifyVenueChanges(ctx context.Context, venueIDs []string) {
	changedIDs := make(map[string]struct{}, len(venueIDs))
	for _, venueID := range venueIDs {
		changedIDs[venueID] = struct{}{}
	}
	s.mu.Lock()
	if !s.venueAvailable {
		s.mu.Unlock()
		return
	}
	entries := make([]*entry, 0)
	for _, current := range s.entries {
		if current.available && !current.retired && referencesAny(current.venueIDs, changedIDs) {
			entries = append(entries, current)
		}
	}
	s.mu.Unlock()
	for _, current := range entries {
		snapshot, err := s.store.Snapshot(ctx, current.groupID, current.owner, s.venues)
		if err != nil {
			s.markUnavailable(current, err)
			continue
		}
		newDigest := digest(snapshot)
		s.mu.Lock()
		if s.entries[entryKey(current.groupID, current.date)] != current || current.retired {
			s.mu.Unlock()
			continue
		}
		oldDigest := current.lastDigest
		current.lastDigest = newDigest
		s.mu.Unlock()
		if oldDigest == "" || oldDigest != newDigest {
			s.hub.PublishChanged(viewKey(current.groupID, current.date), snapshot.Sequence)
		}
	}
}

func (s *Service) VenueSourceUnavailable(err error) {
	s.mu.Lock()
	if !s.venueAvailable {
		s.mu.Unlock()
		return
	}
	s.venueAvailable = false
	entries := make([]*entry, 0, len(s.entries))
	for _, current := range s.entries {
		if len(current.venueIDs) > 0 {
			entries = append(entries, current)
			s.signal(current)
		}
	}
	s.mu.Unlock()
	for _, current := range entries {
		s.hub.ResetView(viewKey(current.groupID, current.date))
	}
	s.logger.Warn("current-Meets Venue source unavailable", "error_type", fmt.Sprintf("%T", err))
}

func (s *Service) VenueSourceRecovered() {
	s.mu.Lock()
	if s.venueAvailable {
		s.mu.Unlock()
		return
	}
	s.venueAvailable = true
	for _, current := range s.entries {
		s.signal(current)
	}
	s.mu.Unlock()
}

func (s *Service) EvictExpired(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	entries, err := s.store.Expired(ctx, now)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	var watches []*sourceWatch
	var retired []ExpiredEntry
	for _, expired := range entries {
		current := s.entries[entryKey(expired.GroupID, expired.Date)]
		if current != nil && (current.owner != expired.Owner || current.validUntil.After(now)) {
			continue
		}
		if current != nil {
			s.retireLocked(current)
			if s.diagnostics {
				s.logger.Info("current-Meets projection evicted", "group_id", maskID(expired.GroupID), "date", expired.Date)
			}
			if current.watch != nil {
				watches = append(watches, current.watch)
				current.watch = nil
			}
		}
		retired = append(retired, expired)
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.stop()
	}
	for _, expired := range retired {
		if err := s.store.DeleteIfOwner(ctx, expired); err != nil {
			return fmt.Errorf("evict current-Meets projection for Group %q: %w", maskID(expired.GroupID), err)
		}
	}
	return nil
}

func (s *Service) RetireBefore(ctx context.Context, date string) error {
	s.mu.Lock()
	var retired []ExpiredEntry
	var watches []*sourceWatch
	for _, current := range s.entries {
		if current.date >= date {
			continue
		}
		s.retireLocked(current)
		if s.diagnostics {
			s.logger.Info("current-Meets projection retired at London midnight", "group_id", maskID(current.groupID), "date", current.date)
		}
		retired = append(retired, ExpiredEntry{GroupID: current.groupID, Date: current.date, Owner: current.owner})
		if current.watch != nil {
			watches = append(watches, current.watch)
			current.watch = nil
		}
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.stop()
	}
	for _, expired := range retired {
		if err := s.store.DeleteIfOwner(ctx, expired); err != nil {
			return fmt.Errorf("retire current-Meets projection at London midnight: %w", err)
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
	var watches []*sourceWatch
	for _, current := range s.entries {
		s.retireLocked(current)
		if current.watch != nil {
			watches = append(watches, current.watch)
			current.watch = nil
		}
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.stop()
	}
}

func (s *Service) admit(groupID, date string) (*entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrUnavailable
	}
	key := entryKey(groupID, date)
	if current := s.entries[key]; current != nil && !current.retired {
		return current, nil
	}
	owner := uuid.NewString()
	validUntil := s.now().Add(entryLifetime)
	storeCtx, cancelStore := context.WithTimeout(context.Background(), 5*time.Second)
	err := s.store.Admit(storeCtx, groupID, date, owner, validUntil)
	cancelStore()
	if err != nil {
		s.logger.Warn("current-Meets projection admission failed", "group_id", maskID(groupID), "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("%w: persist current-Meets admission: %v", ErrUnavailable, err)
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	current := &entry{
		groupID: groupID, date: date, owner: owner, validUntil: validUntil,
		ctx: watchCtx, cancel: cancel, changed: make(chan struct{}), venueIDs: make(map[string]struct{}),
	}
	s.entries[key] = current
	if s.diagnostics {
		s.logger.Info("current-Meets projection admitted", "group_id", maskID(groupID), "date", date, "valid_until", validUntil)
	}
	go s.observe(current)
	return current, nil
}

func (s *Service) observe(current *entry) {
	for current.ctx.Err() == nil {
		watchCtx, cancelWatch := context.WithCancel(current.ctx)
		stream, err := s.source.Watch(watchCtx, current.groupID, current.date)
		if err != nil {
			cancelWatch()
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		watch := &sourceWatch{stream: stream, cancel: cancelWatch}
		s.setWatch(current, watch)
		snapshot, err := stream.Next()
		if err == nil {
			err = s.apply(current, snapshot)
		}
		if err != nil {
			watch.stop()
			s.clearWatch(current, watch)
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		s.markAvailable(current)
		for current.ctx.Err() == nil {
			snapshot, err = stream.Next()
			if err != nil {
				s.markUnavailable(current, err)
				break
			}
			if err := s.apply(current, snapshot); err != nil {
				s.markUnavailable(current, err)
				break
			}
		}
		watch.stop()
		s.clearWatch(current, watch)
		if !waitRetry(current.ctx) {
			return
		}
	}
}

func (s *Service) apply(current *entry, snapshot QuerySnapshot) error {
	if current.date != s.CurrentDate() {
		s.mu.Lock()
		var watch *sourceWatch
		if s.entries[entryKey(current.groupID, current.date)] == current && !current.retired {
			s.retireLocked(current)
			watch = current.watch
			current.watch = nil
		}
		s.mu.Unlock()
		if watch != nil {
			watch.stop()
		}
		_ = s.store.DeleteIfOwner(context.Background(), ExpiredEntry{GroupID: current.groupID, Date: current.date, Owner: current.owner})
		return ErrRetired
	}
	projections, err := validateSnapshot(current.groupID, current.date, snapshot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.entries[entryKey(current.groupID, current.date)] != current || current.retired {
		s.mu.Unlock()
		return ErrRetired
	}
	if current.date != s.CurrentDate() {
		s.retireLocked(current)
		watch := current.watch
		current.watch = nil
		s.mu.Unlock()
		if watch != nil {
			watch.stop()
		}
		_ = s.store.DeleteIfOwner(context.Background(), ExpiredEntry{GroupID: current.groupID, Date: current.date, Owner: current.owner})
		return ErrRetired
	}
	sequence, changed, err := s.store.Apply(current.ctx, current.groupID, current.date, current.owner, projections, snapshot.ReadTime)
	if err == nil {
		current.venueIDs = selectedVenueIDs(projections)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if changed {
		view, err := s.store.Snapshot(current.ctx, current.groupID, current.owner, s.venues)
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.entries[entryKey(current.groupID, current.date)] == current && !current.retired {
			current.lastDigest = digest(view)
		}
		s.mu.Unlock()
		s.hub.PublishChanged(viewKey(current.groupID, current.date), sequence)
	}
	return nil
}

func (s *Service) setWatch(current *entry, watch *sourceWatch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[entryKey(current.groupID, current.date)] != current || current.retired {
		watch.stop()
		return
	}
	current.watch = watch
	if s.diagnostics {
		s.logger.Info("current-Meets query watch opened", "group_id", maskID(current.groupID), "date", current.date)
	}
}

func (s *Service) clearWatch(current *entry, watch *sourceWatch) {
	s.mu.Lock()
	if current.watch == watch {
		current.watch = nil
	}
	s.mu.Unlock()
}

func (s *Service) markAvailable(current *entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[entryKey(current.groupID, current.date)] != current || current.retired {
		return
	}
	current.available = true
	current.failureLogged = false
	s.signal(current)
}

func (s *Service) markUnavailable(current *entry, err error) {
	s.mu.Lock()
	if s.entries[entryKey(current.groupID, current.date)] != current || current.retired {
		s.mu.Unlock()
		return
	}
	current.available = false
	s.signal(current)
	if !current.failureLogged {
		s.logger.Warn("current-Meets projection unavailable", "group_id", maskID(current.groupID), "date", current.date, "error_type", fmt.Sprintf("%T", err))
		current.failureLogged = true
	}
	s.mu.Unlock()
	s.hub.ResetView(viewKey(current.groupID, current.date))
	if s.diagnostics {
		s.logger.Info("current-Meets baseline attempt failed", "group_id", maskID(current.groupID), "date", current.date, "err", err)
	}
}

func (s *Service) retireLocked(current *entry) {
	key := entryKey(current.groupID, current.date)
	if s.entries[key] == current {
		delete(s.entries, key)
	}
	current.retired = true
	current.cancel()
	s.signal(current)
	s.hub.ResetView(viewKey(current.groupID, current.date))
}

func (s *Service) signal(current *entry) {
	close(current.changed)
	current.changed = make(chan struct{})
}

func selectedVenueIDs(meets []meetcache.Projection) map[string]struct{} {
	result := make(map[string]struct{})
	for _, meet := range meets {
		if meet.SelectedVenueID != "" {
			result[meet.SelectedVenueID] = struct{}{}
		}
	}
	return result
}

func referencesAny(references, changed map[string]struct{}) bool {
	for venueID := range references {
		if _, ok := changed[venueID]; ok {
			return true
		}
	}
	return false
}

func digest(snapshot Snapshot) string {
	encoded, err := json.Marshal(snapshot.Meets)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func entryKey(groupID, date string) string { return groupID + "\x00" + date }
func viewKey(groupID, date string) string  { return "current-meets:" + groupID + ":" + date }

func maskID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
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
