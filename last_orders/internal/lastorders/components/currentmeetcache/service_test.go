package currentmeetcache

import (
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
	select {
	case duplicate := <-source.calls:
		t.Fatalf("London midnight reopened the open-Meets watch: %+v", duplicate)
	default:
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
