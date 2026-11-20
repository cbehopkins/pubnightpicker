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

func (source *recoverySource) Watch(ctx context.Context, _, _ string) (Stream, error) {
	source.mu.Lock()
	source.watches++
	watchNumber := source.watches
	source.mu.Unlock()
	source.starts <- watchNumber
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
	subscription := hub.Register(viewKey("group-a", date), "current-meets-group:group-a", "")
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
