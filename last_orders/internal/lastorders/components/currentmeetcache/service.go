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

	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sourcewatch"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"

	"github.com/google/uuid"
)

const (
	entryLifetime = 6 * time.Hour
	retryDelay    = 5 * time.Second
	initialWait   = 5 * time.Second
)

type Service struct {
	store           *Store
	source          Source
	venues          *venuecache.Store
	venueResolver   VenueResolver
	hub             *sse.Hub
	logger          *slog.Logger
	now             func() time.Time
	location        *time.Location
	diagnostics     bool
	openDiagnostics bool
	mu              sync.Mutex
	entries         map[string]*entry
	closed          bool
	venueAvailable  bool
}

type VenueResolver interface {
	GetForMeet(context.Context, string) (venuecache.VenueProjection, bool, error)
}

type entry struct {
	kind          ViewKind
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
	watch         *sourcewatch.Watch
	venueIDs      map[string]struct{}
	lastDigest    string
}

func NewService(store *Store, source Source, venues *venuecache.Store, hub *sse.Hub, logger *slog.Logger, now func() time.Time) (*Service, error) {
	return NewServiceWithVenueResolver(store, source, venues, venues, hub, logger, now)
}

func NewServiceWithVenueResolver(store *Store, source Source, venues *venuecache.Store, resolver VenueResolver, hub *sse.Hub, logger *slog.Logger, now func() time.Time) (*Service, error) {
	if store == nil || source == nil || venues == nil || resolver == nil || hub == nil {
		return nil, fmt.Errorf("Group Meet store, source, Venue store, Venue resolver, and stream hub are required")
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
		venueResolver: resolver,
		location:      location, diagnostics: os.Getenv("LAST_ORDERS_CURRENT_MEET_DIAGNOSTICS") == "true",
		openDiagnostics: os.Getenv("LAST_ORDERS_OPEN_MEET_DIAGNOSTICS") == "true",
		entries:         make(map[string]*entry), venueAvailable: true,
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
	return s.get(ctx, CurrentView, groupID, date)
}

func (s *Service) GetOpen(ctx context.Context, groupID string) (Snapshot, error) {
	if groupID == "" {
		return Snapshot{}, ErrUnavailable
	}
	return s.get(ctx, OpenView, groupID, "")
}

func (s *Service) snapshot(ctx context.Context, kind ViewKind, groupID, date, owner string) (Snapshot, error) {
	confirmedMissing := make(map[string]struct{})
	for {
		snapshot, err := s.store.snapshot(ctx, kind, groupID, date, owner, s.venues, confirmedMissing)
		if err == nil {
			return snapshot, nil
		}
		cacheMiss, isCacheMiss := err.(missingVenueCacheEntry)
		if !isCacheMiss {
			return Snapshot{}, err
		}
		venue, missing, err := s.venueResolver.GetForMeet(ctx, cacheMiss.venueID)
		if err != nil {
			return Snapshot{}, fmt.Errorf("resolve selected Venue %q: %w", cacheMiss.venueID, err)
		}
		if missing {
			confirmedMissing[cacheMiss.venueID] = struct{}{}
			continue
		}
		if _, err := s.venues.PutIfAbsent(ctx, venue); err != nil {
			return Snapshot{}, fmt.Errorf("persist resolved Venue %q: %w", cacheMiss.venueID, err)
		}
	}
}

func (s *Service) get(ctx context.Context, kind ViewKind, groupID, date string) (Snapshot, error) {
	current, err := s.admit(kind, groupID, date)
	if err != nil {
		return Snapshot{}, err
	}
	timer := time.NewTimer(initialWait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		if current.retired || s.entries[entryKey(kind, groupID, date)] != current {
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
			s.mu.Unlock()
			snapshot, readErr := s.snapshot(ctx, kind, groupID, date, current.owner)
			s.mu.Lock()
			if current.retired || s.entries[entryKey(kind, groupID, date)] != current {
				s.mu.Unlock()
				return Snapshot{}, ErrUnavailable
			}
			if !current.available || (!s.venueAvailable && len(current.venueIDs) > 0) {
				s.mu.Unlock()
				continue
			}
			if readErr == nil && (kind != CurrentView || date == s.CurrentDate()) {
				current.lastDigest = digest(snapshot)
				s.mu.Unlock()
				return snapshot, nil
			}
			if readErr == nil && kind == CurrentView {
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
				watch.Stop()
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
	return s.renew(ctx, CurrentView, groupID, date)
}

func (s *Service) RenewOpen(ctx context.Context, groupID string) error {
	return s.renew(ctx, OpenView, groupID, "")
}

func (s *Service) renew(ctx context.Context, kind ViewKind, groupID, date string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == CurrentView && date != s.CurrentDate() {
		return ErrRetired
	}
	current := s.entries[entryKey(kind, groupID, date)]
	if current == nil || current.retired || !current.available {
		return ErrRetired
	}
	validUntil := s.now().Add(entryLifetime)
	if err := s.store.Renew(ctx, kind, groupID, date, current.owner, validUntil); err != nil {
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
		snapshot, err := s.snapshot(ctx, current.kind, current.groupID, current.date, current.owner)
		if err != nil {
			s.markUnavailable(current, err)
			continue
		}
		newDigest := digest(snapshot)
		s.mu.Lock()
		if s.entries[entryKey(current.kind, current.groupID, current.date)] != current || current.retired {
			s.mu.Unlock()
			continue
		}
		oldDigest := current.lastDigest
		current.lastDigest = newDigest
		s.mu.Unlock()
		if oldDigest == "" || oldDigest != newDigest {
			s.hub.PublishChanged(viewKey(current.kind, current.groupID, current.date), snapshot.Sequence)
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
		s.hub.ResetView(viewKey(current.kind, current.groupID, current.date))
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
	var watches []*sourcewatch.Watch
	var retired []ExpiredEntry
	for _, expired := range entries {
		current := s.entries[entryKey(expired.Kind, expired.GroupID, expired.Date)]
		if current != nil && (current.owner != expired.Owner || current.validUntil.After(now)) {
			continue
		}
		if current != nil {
			s.retireLocked(current)
			if s.diagnosticsEnabled(expired.Kind) {
				s.logger.Info(viewLabel(expired.Kind)+" projection evicted", "group_id", maskID(expired.GroupID), "date", expired.Date)
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
		watch.Stop()
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
	var watches []*sourcewatch.Watch
	for _, current := range s.entries {
		if current.kind != CurrentView || current.date >= date {
			continue
		}
		s.retireLocked(current)
		if s.diagnosticsEnabled(current.kind) {
			s.logger.Info("current-Meets projection retired at London midnight", "group_id", maskID(current.groupID), "date", current.date)
		}
		retired = append(retired, ExpiredEntry{Kind: current.kind, GroupID: current.groupID, Date: current.date, Owner: current.owner})
		if current.watch != nil {
			watches = append(watches, current.watch)
			current.watch = nil
		}
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.Stop()
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
	var watches []*sourcewatch.Watch
	for _, current := range s.entries {
		s.retireLocked(current)
		if current.watch != nil {
			watches = append(watches, current.watch)
			current.watch = nil
		}
	}
	s.mu.Unlock()
	for _, watch := range watches {
		watch.Stop()
	}
}

func (s *Service) admit(kind ViewKind, groupID, date string) (*entry, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	key := entryKey(kind, groupID, date)
	var expiredWatch *sourcewatch.Watch
	if current := s.entries[key]; current != nil && !current.retired {
		if current.validUntil.After(s.now()) {
			s.mu.Unlock()
			return current, nil
		}
		s.retireLocked(current)
		expiredWatch = current.watch
		current.watch = nil
	}
	owner := uuid.NewString()
	validUntil := s.now().Add(entryLifetime)
	storeCtx, cancelStore := context.WithTimeout(context.Background(), 5*time.Second)
	err := s.store.Admit(storeCtx, kind, groupID, date, owner, validUntil)
	cancelStore()
	if err != nil {
		s.logger.Warn(viewLabel(kind)+" projection admission failed", "group_id", maskID(groupID), "error_type", fmt.Sprintf("%T", err))
		s.mu.Unlock()
		if expiredWatch != nil {
			expiredWatch.Stop()
		}
		return nil, fmt.Errorf("%w: persist current-Meets admission: %v", ErrUnavailable, err)
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	current := &entry{
		kind: kind, groupID: groupID, date: date, owner: owner, validUntil: validUntil,
		ctx: watchCtx, cancel: cancel, changed: make(chan struct{}), venueIDs: make(map[string]struct{}),
	}
	s.entries[key] = current
	if s.diagnosticsEnabled(kind) {
		s.logger.Info(viewLabel(kind)+" projection admitted", "group_id", maskID(groupID), "date", date, "valid_until", validUntil)
	}
	s.mu.Unlock()
	if expiredWatch != nil {
		expiredWatch.Stop()
	}
	go s.observe(current)
	return current, nil
}

func (s *Service) observe(current *entry) {
	for current.ctx.Err() == nil {
		watchCtx, cancelWatch := context.WithCancel(current.ctx)
		stream, err := s.source.Watch(watchCtx, current.groupID, ViewQuery{Kind: current.kind, Date: current.date})
		if err != nil {
			cancelWatch()
			s.markUnavailable(current, err)
			if !waitRetry(current.ctx) {
				return
			}
			continue
		}
		watch := sourcewatch.New(cancelWatch, stream)
		s.setWatch(current, watch)
		snapshot, err := stream.Next()
		if err == nil {
			err = s.apply(current, snapshot)
		}
		if err != nil {
			watch.Stop()
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
		watch.Stop()
		s.clearWatch(current, watch)
		if !waitRetry(current.ctx) {
			return
		}
	}
}

func (s *Service) apply(current *entry, snapshot QuerySnapshot) error {
	if current.kind == CurrentView && current.date != s.CurrentDate() {
		s.mu.Lock()
		var watch *sourcewatch.Watch
		if s.entries[entryKey(current.kind, current.groupID, current.date)] == current && !current.retired {
			s.retireLocked(current)
			watch = current.watch
			current.watch = nil
		}
		s.mu.Unlock()
		if watch != nil {
			watch.Stop()
		}
		_ = s.store.DeleteIfOwner(context.Background(), ExpiredEntry{Kind: current.kind, GroupID: current.groupID, Date: current.date, Owner: current.owner})
		return ErrRetired
	}
	projections, err := validateSnapshot(current.groupID, ViewQuery{Kind: current.kind, Date: current.date}, snapshot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.entries[entryKey(current.kind, current.groupID, current.date)] != current || current.retired {
		s.mu.Unlock()
		return ErrRetired
	}
	if current.kind == CurrentView && current.date != s.CurrentDate() {
		s.retireLocked(current)
		watch := current.watch
		current.watch = nil
		s.mu.Unlock()
		if watch != nil {
			watch.Stop()
		}
		_ = s.store.DeleteIfOwner(context.Background(), ExpiredEntry{Kind: current.kind, GroupID: current.groupID, Date: current.date, Owner: current.owner})
		return ErrRetired
	}
	sequence, changed, err := s.store.Apply(current.ctx, current.kind, current.groupID, current.date, current.owner, projections, snapshot.ReadTime)
	if err == nil {
		current.venueIDs = selectedVenueIDs(projections)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if changed {
		view, err := s.snapshot(current.ctx, current.kind, current.groupID, current.date, current.owner)
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.entries[entryKey(current.kind, current.groupID, current.date)] == current && !current.retired {
			current.lastDigest = digest(view)
		}
		s.mu.Unlock()
		s.hub.PublishChanged(viewKey(current.kind, current.groupID, current.date), sequence)
	}
	return nil
}

func (s *Service) setWatch(current *entry, watch *sourcewatch.Watch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[entryKey(current.kind, current.groupID, current.date)] != current || current.retired {
		watch.Stop()
		return
	}
	current.watch = watch
	if s.diagnosticsEnabled(current.kind) {
		s.logger.Info(viewLabel(current.kind)+" query watch opened", "group_id", maskID(current.groupID), "date", current.date)
	}
}

func (s *Service) clearWatch(current *entry, watch *sourcewatch.Watch) {
	s.mu.Lock()
	if current.watch == watch {
		current.watch = nil
	}
	s.mu.Unlock()
}

func (s *Service) markAvailable(current *entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[entryKey(current.kind, current.groupID, current.date)] != current || current.retired {
		return
	}
	current.available = true
	current.failureLogged = false
	s.signal(current)
}

func (s *Service) markUnavailable(current *entry, err error) {
	s.mu.Lock()
	if s.entries[entryKey(current.kind, current.groupID, current.date)] != current || current.retired {
		s.mu.Unlock()
		return
	}
	current.available = false
	s.signal(current)
	if !current.failureLogged {
		s.logger.Warn(viewLabel(current.kind)+" projection unavailable", "group_id", maskID(current.groupID), "date", current.date, "error_type", fmt.Sprintf("%T", err))
		current.failureLogged = true
	}
	s.mu.Unlock()
	s.hub.ResetView(viewKey(current.kind, current.groupID, current.date))
	if s.diagnosticsEnabled(current.kind) {
		s.logger.Info(viewLabel(current.kind)+" baseline attempt failed", "group_id", maskID(current.groupID), "date", current.date, "err", err)
	}
}

func (s *Service) retireLocked(current *entry) {
	key := entryKey(current.kind, current.groupID, current.date)
	if s.entries[key] == current {
		delete(s.entries, key)
	}
	current.retired = true
	current.cancel()
	s.signal(current)
	s.hub.ResetView(viewKey(current.kind, current.groupID, current.date))
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

func entryKey(kind ViewKind, groupID, date string) string {
	return string(kind) + "\x00" + groupID + "\x00" + date
}

func viewKey(kind ViewKind, groupID, date string) string {
	if kind == OpenView {
		return "open-meets:" + groupID
	}
	return "current-meets:" + groupID + ":" + date
}

func (s *Service) diagnosticsEnabled(kind ViewKind) bool {
	if kind == OpenView {
		return s.openDiagnostics
	}
	return s.diagnostics
}

func viewLabel(kind ViewKind) string {
	if kind == OpenView {
		return "open-Meets"
	}
	return "current-Meets"
}

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
