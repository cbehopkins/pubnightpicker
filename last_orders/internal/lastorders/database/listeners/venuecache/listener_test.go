package venuecache

import (
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	component "last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/components/venuecache/venuecachetest"

	_ "modernc.org/sqlite"
)

type fakeSource struct{}

func (fakeSource) Get(context.Context, string) (component.Document, error) {
	return component.Document{}, component.ErrNotFound
}

func (fakeSource) ListEventVenues(context.Context) ([]component.Document, error) {
	return nil, nil
}

type blockingStream struct {
	ctx context.Context
}

func (s *blockingStream) Next() ([]component.Change, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *blockingStream) Stop() {}

type blockingSource struct {
	*fakeSource
	stream *blockingStream
}

func (s *blockingSource) Watch(ctx context.Context) (component.ChangeStream, error) {
	s.stream.ctx = ctx
	return s.stream, nil
}

func (fakeSource) Watch(context.Context) (component.ChangeStream, error) {
	return nil, nil
}

func TestApplyAddedModifiedAndRemovedChanges(t *testing.T) {
	store := newTestStore(t)
	service, err := component.NewService(store, &fakeSource{}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	change := component.Change{Kind: component.ChangeAdded, Doc: component.Document{ID: "venue-1", Data: map[string]any{"name": "The Crown"}}}
	if err := listener.apply(context.Background(), change); err != nil {
		t.Fatalf("apply added: %v", err)
	}
	change.Kind = component.ChangeModified
	change.Doc.Data["name"] = "The New Crown"
	if err := listener.apply(context.Background(), change); err != nil {
		t.Fatalf("apply modified: %v", err)
	}
	got, err := store.Get(context.Background(), "venue-1")
	if err != nil || got.Name != "The New Crown" {
		t.Fatalf("cached projection = %#v, %v", got, err)
	}
	change.Kind = component.ChangeRemoved
	if err := listener.apply(context.Background(), change); err != nil {
		t.Fatalf("apply removed: %v", err)
	}
	if _, err := store.Get(context.Background(), "venue-1"); err != component.ErrCacheMiss {
		t.Fatalf("after removal error = %v; want cache miss", err)
	}
}

func TestListenerReadyAfterFirstSnapshot(t *testing.T) {
	store := newTestStore(t)
	source := venuecachetest.New()
	service, err := component.NewService(store, source, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	defer listener.Close()

	select {
	case <-listener.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("listener was not ready after an empty first snapshot")
	}
}

func TestListenerNotReadyBeforeFirstSnapshot(t *testing.T) {
	store := newTestStore(t)
	service, err := component.NewService(store, &blockingSource{fakeSource: &fakeSource{}, stream: &blockingStream{}}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	defer listener.Close()

	select {
	case <-listener.Ready():
		t.Fatal("listener ready before any snapshot")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCloseWaitsForListenerGoroutine(t *testing.T) {
	store := newTestStore(t)
	service, err := component.NewService(store, &blockingSource{
		fakeSource: &fakeSource{},
		stream:     &blockingStream{},
	}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	if err := listener.Start(context.Background()); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	closed := make(chan struct{})
	go func() {
		_ = listener.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("listener close did not complete")
	}
}

func TestListenerReconnectsAfterTransientProjectionFailure(t *testing.T) {
	store := newTestStore(t)
	service, err := component.NewService(store, &replaySource{changes: [][]component.Change{
		{{Kind: component.ChangeAdded, Doc: component.Document{ID: "venue-1", Data: map[string]any{"name": "The Crown"}}}},
		{{Kind: component.ChangeAdded, Doc: component.Document{ID: "venue-1", Data: map[string]any{"name": "The Crown"}}}},
	}}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go listener.watch(ctx)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.Get(context.Background(), "venue-1"); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("listener did not rebuild the projection after a transient persistence failure")
}

type replaySource struct {
	changes [][]component.Change
	index   int
}

func (s *replaySource) Get(context.Context, string) (component.Document, error) {
	return component.Document{}, component.ErrNotFound
}

func (s *replaySource) ListEventVenues(context.Context) ([]component.Document, error) {
	return nil, nil
}

func (s *replaySource) Watch(context.Context) (component.ChangeStream, error) {
	if s.index >= len(s.changes) {
		return &replayStream{done: true}, nil
	}
	stream := &replayStream{changes: s.changes[s.index], failAfterFirst: s.index == 0}
	s.index++
	return stream, nil
}

type replayStream struct {
	changes        []component.Change
	idx            int
	done           bool
	failAfterFirst bool
}

func (s *replayStream) Next() ([]component.Change, error) {
	if s.done {
		return nil, context.Canceled
	}
	if s.failAfterFirst && s.idx == 1 {
		return nil, io.ErrUnexpectedEOF
	}
	if s.idx >= len(s.changes) {
		return nil, io.ErrUnexpectedEOF
	}
	batch := s.changes[s.idx : s.idx+1]
	s.idx++
	return batch, nil
}

func (s *replayStream) Stop() {}

func newTestStore(t *testing.T) *component.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "venue_cache.db")
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	store, err := component.New(base)
	if err != nil {
		t.Fatalf("new venue cache store: %v", err)
	}
	return store
}
