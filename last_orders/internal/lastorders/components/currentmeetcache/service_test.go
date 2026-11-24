package currentmeetcache

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"

	_ "modernc.org/sqlite"
)

type recoverySource struct {
	mu      sync.Mutex
	starts  chan int
	watches int
}

func (source *recoverySource) Watch(ctx context.Context, _ string, query ViewQuery) (Stream, error) {
	source.mu.Lock()
	source.watches++
	watchNumber := source.watches
	source.mu.Unlock()
	source.starts <- watchNumber
	if query.Kind == OpenView {
		if watchNumber == 1 {
			return &scriptedStream{ctx: ctx, results: []queryResult{
				{snapshot: QuerySnapshot{Documents: []meetcache.Document{}, ReadTime: time.Now()}},
				{err: errors.New("temporary query failure")},
			}}, nil
		}
		return &scriptedStream{ctx: ctx, results: []queryResult{
			{snapshot: QuerySnapshot{Documents: []meetcache.Document{{
				ID: "overdue-open", Exists: true, Version: time.Now(), Data: map[string]any{
					"groupId": "group-a", "date": "2020-01-01", "completed": false,
				},
			}}, ReadTime: time.Now()}},
		}}, nil
	}
	if watchNumber == 1 {
		return &scriptedStream{ctx: ctx, results: []queryResult{
			{snapshot: QuerySnapshot{Documents: []meetcache.Document{}, ReadTime: time.Now()}},
			{err: errors.New("temporary query failure")},
		}}, nil
	}
	return &scriptedStream{ctx: ctx, results: []queryResult{
		{snapshot: QuerySnapshot{Documents: []meetcache.Document{{
			ID: "meet-a", Exists: true, Version: time.Now(), Data: map[string]any{
				"groupId": "group-a", "date": "2026-11-20", "completed": true,
			},
		}}, ReadTime: time.Now()}},
	}}, nil
}

type queryResult struct {
	snapshot QuerySnapshot
	err      error
}

type scriptedStream struct {
	ctx     context.Context
	results []queryResult
	index   int
	mu      sync.Mutex
}

func (stream *scriptedStream) Next() (QuerySnapshot, error) {
	stream.mu.Lock()
	if stream.index < len(stream.results) {
		result := stream.results[stream.index]
		stream.index++
		stream.mu.Unlock()
		return result.snapshot, result.err
	}
	stream.mu.Unlock()
	<-stream.ctx.Done()
	return QuerySnapshot{}, stream.ctx.Err()
}

func (stream *scriptedStream) Stop() {}

func TestServiceRecoversAfterQueryWatchFailure(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	hub := sse.NewHub()
	source := &recoverySource{starts: make(chan int, 2)}
	service, err := NewService(store, source, venues, hub, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time {
		return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	date := service.CurrentDate()
	subscription := hub.Register(viewKey(CurrentView, "group-a", date), "current-meets-group:group-a", "")
	defer subscription.Unregister()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	firstRead := make(chan error, 1)
	go func() {
		_, err := service.Get(ctx, "group-a")
		firstRead <- err
	}()
	if watch := waitForWatchStart(t, ctx, source.starts); watch != 1 {
		t.Fatalf("first watch number=%d, want 1", watch)
	}
	select {
	case <-subscription.Invalidated():
	case <-ctx.Done():
		t.Fatal("query failure did not reset the current-Meets subscriber")
	}
	if watch := waitForWatchStart(t, ctx, source.starts); watch != 2 {
		t.Fatalf("recovery watch number=%d, want 2", watch)
	}

	snapshot, err := service.Get(ctx, "group-a")
	if err != nil {
		t.Fatalf("GET after source recovery: %v", err)
	}
	if len(snapshot.Meets) != 1 || snapshot.Meets[0].Meet.ID != "meet-a" {
		t.Fatalf("recovered snapshot=%+v, want meet-a", snapshot.Meets)
	}
	select {
	case err := <-firstRead:
		if err != nil && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("initial shared read returned unexpected error: %v", err)
		}
	default:
	}
}

func TestOpenAndCurrentViewsUseIndependentSharedGroupWatches(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &viewWatchSource{
		calls: make(chan viewWatchCall, 2),
		snapshots: map[ViewKind]QuerySnapshot{
			OpenView: {Documents: []meetcache.Document{{
				ID: "overdue-open", Exists: true, Version: clock, Data: map[string]any{
					"groupId": "group-a", "date": "2020-01-01", "completed": false,
				},
			}}, ReadTime: clock},
			CurrentView: {Documents: []meetcache.Document{{
				ID: "future-complete", Exists: true, Version: clock, Data: map[string]any{
					"groupId": "group-a", "date": "2026-11-21", "completed": true,
				},
			}}, ReadTime: clock},
		},
	}
	service, err := NewService(store, source, venues, sse.NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time {
		return clock
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	open, err := service.GetOpen(ctx, "group-a")
	if err != nil {
		t.Fatalf("read open-Meets view: %v", err)
	}
	if len(open.Meets) != 1 || open.Meets[0].Meet.ID != "overdue-open" || open.Meets[0].Meet.Date != "2020-01-01" {
		t.Fatalf("open-Meets snapshot=%+v, want the past-dated voting-open Meet", open.Meets)
	}
	openCall := waitForViewWatch(t, ctx, source.calls)
	if openCall.groupID != "group-a" || openCall.query.Kind != OpenView || openCall.query.Date != "" {
		t.Fatalf("open-Meets watch=%+v, want an unbounded-date Group query", openCall)
	}
	if _, err := service.GetOpen(ctx, "group-a"); err != nil {
		t.Fatalf("read shared open-Meets view: %v", err)
	}
	select {
	case duplicate := <-source.calls:
		t.Fatalf("second open-Meets GET opened another watch: %+v", duplicate)
	default:
	}

	current, err := service.Get(ctx, "group-a")
	if err != nil {
		t.Fatalf("read current-Meets view: %v", err)
	}
	if len(current.Meets) != 1 || current.Meets[0].Meet.ID != "future-complete" {
		t.Fatalf("current-Meets snapshot=%+v, want the future completed Meet", current.Meets)
	}
	currentCall := waitForViewWatch(t, ctx, source.calls)
	if currentCall.groupID != "group-a" || currentCall.query.Kind != CurrentView || currentCall.query.Date != service.CurrentDate() {
		t.Fatalf("current-Meets watch=%+v, want a date-scoped Group query", currentCall)
	}
	clock = clock.Add(24 * time.Hour)
	openAfterMidnight, err := service.GetOpen(ctx, "group-a")
	if err != nil || len(openAfterMidnight.Meets) != 1 || openAfterMidnight.Meets[0].Meet.ID != "overdue-open" {
		t.Fatalf("open-Meets view changed at London midnight: snapshot=%+v err=%v", openAfterMidnight.Meets, err)
	}
	if replacement := waitForViewWatch(t, ctx, source.calls); replacement.groupID != "group-a" || replacement.query.Kind != OpenView {
		t.Fatalf("open-Meets watch after its six-hour expiry=%+v, want a replacement for the same Group", replacement)
	}
}

func TestOpenViewStreamRenewalProtectsItsWatchUntilRenewedExpiry(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &viewWatchSource{
		calls: make(chan viewWatchCall, 2),
		snapshots: map[ViewKind]QuerySnapshot{
			OpenView: {Documents: []meetcache.Document{}, ReadTime: clock},
		},
	}
	service, err := NewService(store, source, venues, sse.NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time {
		return clock
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	ctx := context.Background()
	if _, err := service.GetOpen(ctx, "group-a"); err != nil {
		t.Fatalf("admit open-Meets view: %v", err)
	}
	watch := waitForViewWatch(t, ctx, source.calls)
	if watch.query.Kind != OpenView {
		t.Fatalf("initial watch=%+v, want open view", watch)
	}
	clock = clock.Add(entryLifetime)
	if err := service.RenewOpen(ctx, "group-a"); err != nil {
		t.Fatalf("renew open-Meets stream lease: %v", err)
	}
	if err := service.EvictExpired(ctx, clock); err != nil {
		t.Fatalf("evict at original deadline: %v", err)
	}
	if _, err := service.GetOpen(ctx, "group-a"); err != nil {
		t.Fatalf("read renewed open-Meets view: %v", err)
	}
	select {
	case duplicate := <-source.calls:
		t.Fatalf("successful stream renewal did not protect the shared watch: %+v", duplicate)
	default:
	}
	clock = clock.Add(entryLifetime + time.Minute)
	if err := service.EvictExpired(ctx, clock); err != nil {
		t.Fatalf("evict after renewed deadline: %v", err)
	}
	if _, err := service.GetOpen(ctx, "group-a"); err != nil {
		t.Fatalf("admit open-Meets replacement after expiry: %v", err)
	}
	if replacement := waitForViewWatch(t, ctx, source.calls); replacement.query.Kind != OpenView {
		t.Fatalf("replacement watch=%+v, want open view", replacement)
	}
}

func TestCurrentDateUsesLondonAcrossDSTTransitions(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	source := &trackedViewWatchSource{calls: make(chan trackedViewWatchCall, 1), stopped: make(chan trackedViewWatchCall, 1)}

	for _, test := range []struct {
		name    string
		instant time.Time
		want    string
	}{
		{name: "spring DST at London midnight", instant: time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC), want: "2026-03-29"},
		{name: "autumn DST at London midnight", instant: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), want: "2026-10-25"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := test.instant
			service, err := NewService(store, source, venues, sse.NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return clock })
			if err != nil {
				t.Fatal(err)
			}
			if got := service.CurrentDate(); got != test.want {
				t.Fatalf("CurrentDate()=%q, want %q", got, test.want)
			}
			service.Close()
		})
	}
}

func TestNewDateAdmissionRacesRetirementAndCannotUseStaleWatch(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &trackedViewWatchSource{
		calls:   make(chan trackedViewWatchCall, 4),
		stopped: make(chan trackedViewWatchCall, 4),
		snapshotFor: func(query ViewQuery) QuerySnapshot {
			meetID := "meet-a"
			date := "2026-11-20"
			if query.Date == "2026-11-21" {
				meetID = "meet-b"
				date = "2026-11-21"
			}
			return QuerySnapshot{Documents: []meetcache.Document{{
				ID: meetID, Exists: true, Version: clock, Data: map[string]any{
					"groupId": "group-a", "date": date, "completed": true,
				},
			}}, ReadTime: clock}
		},
	}
	service, err := NewService(store, source, venues, sse.NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := service.Get(ctx, "group-a"); err != nil {
		t.Fatalf("admit stale current-Meets view: %v", err)
	}
	clock = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	wg.Add(2)
	retireErr := make(chan error, 1)
	getErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		retireErr <- service.RetireBefore(ctx, service.CurrentDate())
	}()
	go func() {
		defer wg.Done()
		_, err := service.Get(ctx, "group-a")
		getErr <- err
	}()
	wg.Wait()
	if err := <-retireErr; err != nil {
		t.Fatalf("retire before racing admission: %v", err)
	}
	if err := <-getErr; err != nil {
		t.Fatalf("racing new-date admission: %v", err)
	}

	foundReplacement := false
	for {
		select {
		case call := <-source.calls:
			if call.query.Kind == CurrentView && call.query.Date == "2026-11-21" {
				foundReplacement = true
			}
		default:
			if !foundReplacement {
				t.Fatal("new-date admission did not open a 2026-11-21 watch")
			}
			return
		}
	}
}

func TestRetireBeforeRetiresOnlyStaleCurrentMeetsAndStopsWatch(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &trackedViewWatchSource{
		calls:   make(chan trackedViewWatchCall, 2),
		stopped: make(chan trackedViewWatchCall, 2),
		snapshots: map[ViewKind]QuerySnapshot{
			CurrentView: {Documents: []meetcache.Document{{
				ID: "meet-a", Exists: true, Version: clock, Data: map[string]any{
					"groupId": "group-a", "date": "2026-11-20", "completed": true,
				},
			}}, ReadTime: clock},
			OpenView: {Documents: []meetcache.Document{{
				ID: "open-a", Exists: true, Version: clock, Data: map[string]any{
					"groupId": "group-a", "date": "2026-11-20", "completed": false,
				},
			}}, ReadTime: clock},
		},
	}
	hub := sse.NewHub()
	service, err := NewService(store, source, venues, hub, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := service.Get(ctx, "group-a"); err != nil {
		t.Fatalf("admit stale current-Meets view: %v", err)
	}
	if _, err := service.GetOpen(ctx, "group-a"); err != nil {
		t.Fatalf("admit open-Meets view: %v", err)
	}
	currentSubscription := hub.Register(viewKey(CurrentView, "group-a", "2026-11-20"), "current-meets-group:group-a", "")
	defer currentSubscription.Unregister()

	clock = clock.Add(24 * time.Hour)
	if err := service.RetireBefore(ctx, service.CurrentDate()); err != nil {
		t.Fatalf("RetireBefore() error=%v", err)
	}
	select {
	case <-currentSubscription.Invalidated():
	case <-ctx.Done():
		t.Fatal("stale current-Meets stream was not reset")
	}
	select {
	case stopped := <-source.stopped:
		if stopped.query.Kind != CurrentView || stopped.query.Date != "2026-11-20" {
			t.Fatalf("stopped watch=%+v, want stale current-Meets view", stopped)
		}
	case <-ctx.Done():
		t.Fatal("stale current-Meets watch was not stopped")
	}
	if err := service.RenewOpen(ctx, "group-a"); err != nil {
		t.Fatalf("open-Meets view was affected by current-Meets retirement: %v", err)
	}
}

func TestOpenViewRecoversFromSourceWatchFailure(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	hub := sse.NewHub()
	source := &recoverySource{starts: make(chan int, 2)}
	service, err := NewService(store, source, venues, hub, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	subscription := hub.Register(viewKey(OpenView, "group-a", ""), "open-meets-group:group-a", "")
	defer subscription.Unregister()
	firstRead := make(chan error, 1)
	go func() {
		_, err := service.GetOpen(ctx, "group-a")
		firstRead <- err
	}()
	if watch := waitForWatchStart(t, ctx, source.starts); watch != 1 {
		t.Fatalf("first open watch number=%d, want 1", watch)
	}
	select {
	case <-subscription.Invalidated():
	case <-ctx.Done():
		t.Fatal("open query failure did not reset its subscriber")
	}
	if watch := waitForWatchStart(t, ctx, source.starts); watch != 2 {
		t.Fatalf("recovery open watch number=%d, want 2", watch)
	}
	snapshot, err := service.GetOpen(ctx, "group-a")
	if err != nil {
		t.Fatalf("read open view after source recovery: %v", err)
	}
	if len(snapshot.Meets) != 1 || snapshot.Meets[0].Meet.ID != "overdue-open" {
		t.Fatalf("recovered open-Meets snapshot=%+v, want overdue-open", snapshot.Meets)
	}
	select {
	case err := <-firstRead:
		if err != nil && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("initial shared read returned unexpected error: %v", err)
		}
	default:
	}
}

type viewWatchCall struct {
	groupID string
	query   ViewQuery
}

type viewWatchSource struct {
	calls     chan viewWatchCall
	snapshots map[ViewKind]QuerySnapshot
}

type trackedViewWatchCall struct {
	groupID string
	query   ViewQuery
}

type trackedViewWatchSource struct {
	calls       chan trackedViewWatchCall
	stopped     chan trackedViewWatchCall
	snapshots   map[ViewKind]QuerySnapshot
	snapshotFor func(ViewQuery) QuerySnapshot
}

func (source *trackedViewWatchSource) Watch(ctx context.Context, groupID string, query ViewQuery) (Stream, error) {
	call := trackedViewWatchCall{groupID: groupID, query: query}
	source.calls <- call
	snapshot := source.snapshots[query.Kind]
	if source.snapshotFor != nil {
		snapshot = source.snapshotFor(query)
	}
	return &trackedViewSnapshotStream{ctx: ctx, call: call, snapshot: snapshot, stopped: source.stopped}, nil
}

type trackedViewSnapshotStream struct {
	ctx       context.Context
	call      trackedViewWatchCall
	snapshot  QuerySnapshot
	stopped   chan trackedViewWatchCall
	delivered bool
}

func (stream *trackedViewSnapshotStream) Next() (QuerySnapshot, error) {
	if !stream.delivered {
		stream.delivered = true
		return stream.snapshot, nil
	}
	<-stream.ctx.Done()
	return QuerySnapshot{}, stream.ctx.Err()
}

func (stream *trackedViewSnapshotStream) Stop() {
	stream.stopped <- stream.call
}

type venueResolverFunc func(context.Context, string) (venuecache.VenueProjection, bool, error)

func (resolver venueResolverFunc) GetForMeet(ctx context.Context, venueID string) (venuecache.VenueProjection, bool, error) {
	return resolver(ctx, venueID)
}

func TestServiceResolvesUnobservedVenuesBeforeComposingSnapshot(t *testing.T) {
	for _, test := range []struct {
		name    string
		resolve venueResolverFunc
		check   func(*testing.T, Snapshot)
	}{
		{
			name: "present in authoritative source",
			resolve: func(_ context.Context, venueID string) (venuecache.VenueProjection, bool, error) {
				return venuecache.VenueProjection{ID: venueID, Name: "The New Crown"}, false, nil
			},
			check: func(t *testing.T, snapshot Snapshot) {
				if len(snapshot.Meets) != 1 || snapshot.Meets[0].Venue == nil || snapshot.Meets[0].Venue.ID != "venue-a" || snapshot.Meets[0].Venue.Name != "The New Crown" {
					t.Fatalf("snapshot=%+v, want the authoritative Venue summary", snapshot)
				}
			},
		},
		{
			name: "confirmed absent from authoritative source",
			resolve: func(context.Context, string) (venuecache.VenueProjection, bool, error) {
				return venuecache.VenueProjection{}, true, nil
			},
			check: func(t *testing.T, snapshot Snapshot) {
				if len(snapshot.Meets) != 1 || snapshot.Meets[0].MissingVenueID != "venue-a" {
					t.Fatalf("snapshot=%+v, want the confirmed missing Venue reference", snapshot)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			t.Setenv("LAST_ORDERS_CURRENT_MEET_DIAGNOSTICS", "true")
			base, err := basestore.New(db)
			if err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(base)
			if err != nil {
				t.Fatal(err)
			}
			venues, err := venuecache.New(base)
			if err != nil {
				t.Fatal(err)
			}
			clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
			source := &viewWatchSource{
				calls: make(chan viewWatchCall, 1),
				snapshots: map[ViewKind]QuerySnapshot{
					CurrentView: {Documents: []meetcache.Document{{
						ID: "meet-a", Exists: true, Version: clock, Data: map[string]any{
							"groupId": "group-a", "date": "2026-11-20", "completed": true, "selected": "venue-a",
						},
					}}, ReadTime: clock},
				},
			}
			var logs bytes.Buffer
			resolverCalls := 0
			resolver := venueResolverFunc(func(ctx context.Context, venueID string) (venuecache.VenueProjection, bool, error) {
				resolverCalls++
				return test.resolve(ctx, venueID)
			})
			service, err := NewServiceWithVenueResolver(store, source, venues, resolver, sse.NewHub(), slog.New(slog.NewTextHandler(&logs, nil)), func() time.Time { return clock })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.Close)
			snapshot, err := service.Get(context.Background(), "group-a")
			if err != nil {
				t.Fatalf("Get() error=%v resolver calls=%d diagnostics=%s", err, resolverCalls, logs.String())
			}
			test.check(t, snapshot)
		})
	}
}

func (source *viewWatchSource) Watch(ctx context.Context, groupID string, query ViewQuery) (Stream, error) {
	source.calls <- viewWatchCall{groupID: groupID, query: query}
	return &viewSnapshotStream{ctx: ctx, snapshot: source.snapshots[query.Kind]}, nil
}

type viewSnapshotStream struct {
	ctx       context.Context
	snapshot  QuerySnapshot
	delivered bool
}

func (stream *viewSnapshotStream) Next() (QuerySnapshot, error) {
	if !stream.delivered {
		stream.delivered = true
		return stream.snapshot, nil
	}
	<-stream.ctx.Done()
	return QuerySnapshot{}, stream.ctx.Err()
}

func (stream *viewSnapshotStream) Stop() {}

func waitForViewWatch(t *testing.T, ctx context.Context, calls <-chan viewWatchCall) viewWatchCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-ctx.Done():
		t.Fatal("timed out waiting for Group view query watch")
		return viewWatchCall{}
	}
}

func waitForWatchStart(t *testing.T, ctx context.Context, starts <-chan int) int {
	t.Helper()
	select {
	case watch := <-starts:
		return watch
	case <-ctx.Done():
		t.Fatal("timed out waiting for current-Meets query watch")
		return 0
	}
}
