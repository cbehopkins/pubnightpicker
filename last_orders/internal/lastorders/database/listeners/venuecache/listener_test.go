package venuecache

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
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

func TestListenerReportsPersistenceFailureBeforeReadiness(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "failed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := component.New(base)
	if err != nil {
		t.Fatal(err)
	}
	source := &replaySource{changes: [][]component.Change{{{Kind: component.ChangeRemoved, Doc: component.Document{ID: "venue"}}}}}
	service, err := component.NewService(store, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	select {
	case err := <-listener.Errors():
		if !strings.Contains(err.Error(), "persist venue cache projection") || !strings.Contains(err.Error(), "database is closed") {
			t.Fatalf("unexpected persistence error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("listener retried persistence failure instead of reporting it")
	}
	select {
	case <-listener.Ready():
		t.Fatal("failed projection must not become ready")
	default:
	}
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

func TestListenerReconcilesBaselineAfterStreamFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	store, base := newTestProjection(t)
	for _, projection := range []component.VenueProjection{
		{ID: "kept", Name: "The Crown"}, {ID: "offline", Name: "Deleted while disconnected"},
		{ID: "stale", Name: "Stale cached venue"},
	} {
		if err := store.Put(ctx, projection); err != nil {
			t.Fatal(err)
		}
	}
	kept := component.Change{Kind: component.ChangeAdded, Doc: component.Document{ID: "kept", Data: map[string]any{"name": "The Crown"}}}
	offline := component.Change{Kind: component.ChangeAdded, Doc: component.Document{ID: "offline", Data: map[string]any{"name": "Deleted while disconnected"}}}
	modified := component.Change{Kind: component.ChangeModified, Doc: component.Document{ID: "kept", Data: map[string]any{"name": "The New Crown"}}}
	first := newScriptedStream([][]component.Change{{kept, offline}})
	second := newScriptedStream([][]component.Change{{kept}, {modified}, {modified}})
	source := &scriptedSource{streams: []*scriptedStream{first, second}}
	service, err := component.NewService(store, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	awaitStreamRequest(t, ctx, first, 0)
	select {
	case <-listener.Ready():
		t.Fatal("listener became ready before baseline application")
	default:
	}
	advanceStream(t, ctx, first)
	awaitStreamRequest(t, ctx, first, 1)
	select {
	case <-listener.Ready():
	default:
		t.Fatal("baseline committed without closing readiness")
	}
	assertListenerSequence(t, base, 4)
	assertListenerVersion(t, base, "stale", 4, true)
	advanceStream(t, ctx, first)
	awaitStreamRequest(t, ctx, second, 0)
	select {
	case <-first.stopped:
	default:
		t.Fatal("failed source stream was not stopped before reopening")
	}
	advanceStream(t, ctx, second)
	awaitStreamRequest(t, ctx, second, 1)
	assertListenerSequence(t, base, 5)
	assertListenerVersion(t, base, "kept", 1, false)
	assertListenerVersion(t, base, "offline", 5, true)
	assertListenerVersion(t, base, "stale", 4, true)
	if _, err := store.Get(ctx, "offline"); !errors.Is(err, component.ErrCacheMiss) {
		t.Fatalf("reconnected baseline retained deleted venue: %v", err)
	}
	advanceStream(t, ctx, second)
	awaitStreamRequest(t, ctx, second, 2)
	assertListenerSequence(t, base, 6)
	assertListenerVersion(t, base, "kept", 6, false)
	if got, err := store.Get(ctx, "kept"); err != nil || got.Name != "The New Crown" {
		t.Fatalf("delta after reconnect = %+v, %v", got, err)
	}
	advanceStream(t, ctx, second)
	awaitStreamRequest(t, ctx, second, 3)
	assertListenerSequence(t, base, 6)
	cancel()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-second.stopped:
	default:
		t.Fatal("active stream was not stopped on cancellation")
	}
	select {
	case err := <-listener.Errors():
		t.Fatalf("listener reported unexpected failure: %v", err)
	default:
	}
}

type scriptedSource struct {
	fakeSource
	streams []*scriptedStream
	index   int
}

func (source *scriptedSource) Watch(ctx context.Context) (component.ChangeStream, error) {
	if source.index >= len(source.streams) {
		return nil, errors.New("unexpected extra stream open")
	}
	stream := source.streams[source.index]
	source.index++
	stream.ctx = ctx
	return stream, nil
}

type scriptedStream struct {
	ctx       context.Context
	batches   [][]component.Change
	index     int
	requested chan int
	advance   chan struct{}
	stopped   chan struct{}
}

func newScriptedStream(batches [][]component.Change) *scriptedStream {
	return &scriptedStream{batches: batches, requested: make(chan int, 1), advance: make(chan struct{}), stopped: make(chan struct{})}
}

func (stream *scriptedStream) Next() ([]component.Change, error) {
	select {
	case stream.requested <- stream.index:
	case <-stream.ctx.Done():
		return nil, stream.ctx.Err()
	}
	select {
	case <-stream.advance:
	case <-stream.ctx.Done():
		return nil, stream.ctx.Err()
	}
	if stream.index >= len(stream.batches) {
		return nil, io.ErrUnexpectedEOF
	}
	batch := stream.batches[stream.index]
	stream.index++
	return batch, nil
}

func (stream *scriptedStream) Stop() { close(stream.stopped) }

func awaitStreamRequest(t *testing.T, ctx context.Context, stream *scriptedStream, want int) {
	t.Helper()
	select {
	case index := <-stream.requested:
		if index != want {
			t.Fatalf("requested batch %d; want %d", index, want)
		}
	case <-ctx.Done():
		t.Fatalf("stream did not request batch %d: %v", want, ctx.Err())
	}
}

func advanceStream(t *testing.T, ctx context.Context, stream *scriptedStream) {
	t.Helper()
	select {
	case stream.advance <- struct{}{}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func assertListenerSequence(t *testing.T, base *basestore.Store, want int64) {
	t.Helper()
	tx, err := base.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state, err := base.ReadSyncState(t.Context(), tx)
	if err != nil || state.Sequence != want {
		t.Fatalf("listener sequence = %d, %v; want %d", state.Sequence, err, want)
	}
}

func assertListenerVersion(t *testing.T, base *basestore.Store, venueID string, want int64, wantDeleted bool) {
	t.Helper()
	var version int64
	var deleted bool
	if err := base.DB().QueryRow(`SELECT change_version, deleted FROM venue_cache WHERE venue_id = ?`, venueID).Scan(&version, &deleted); err != nil {
		t.Fatal(err)
	}
	if version != want || deleted != wantDeleted {
		t.Fatalf("%s: version=%d, deleted=%v; want %d, %v", venueID, version, deleted, want, wantDeleted)
	}
}

func newTestProjection(t *testing.T) (*component.Store, *basestore.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "projection.db")+
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := component.New(base)
	if err != nil {
		t.Fatal(err)
	}
	return store, base
}

func TestListenerMalformedBaselinePreservesProjectionAndReadiness(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	store, base := newTestProjection(t)
	original := component.VenueProjection{ID: "kept", Name: "Original venue"}
	if err := store.Put(ctx, original); err != nil {
		t.Fatal(err)
	}
	stream := newScriptedStream([][]component.Change{{
		{Kind: component.ChangeAdded, Doc: component.Document{ID: "kept", Data: map[string]any{"name": "Changed venue"}}},
		{Kind: component.ChangeAdded, Doc: component.Document{ID: "invalid", Data: map[string]any{"name": "Invalid venue", "parking": "yes"}}},
	}})
	service, err := component.NewService(store, &scriptedSource{streams: []*scriptedStream{stream}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	awaitStreamRequest(t, ctx, stream, 0)
	advanceStream(t, ctx, stream)
	select {
	case err := <-listener.Errors():
		if !errors.Is(err, ErrMalformedDocument) {
			t.Fatalf("baseline error = %v; want malformed document", err)
		}
	case <-ctx.Done():
		t.Fatal("malformed baseline was not reported")
	}
	select {
	case <-listener.Ready():
		t.Fatal("malformed baseline made the listener ready")
	default:
	}
	if got, err := store.Get(ctx, original.ID); err != nil || !got.Equal(original) {
		t.Fatalf("malformed baseline changed projection: %+v, %v", got, err)
	}
	assertListenerSequence(t, base, 1)
	assertListenerVersion(t, base, original.ID, 1, false)
	if _, err := store.Get(ctx, "invalid"); !errors.Is(err, component.ErrCacheMiss) {
		t.Fatalf("malformed baseline inserted invalid venue: %v", err)
	}
	select {
	case <-stream.stopped:
	default:
		t.Fatal("malformed baseline stream was not stopped")
	}
}

func TestListenerMalformedDeltaIsLoggedWithoutChangingExistingVenue(t *testing.T) {
	store, base := newTestProjection(t)
	ctx := t.Context()
	original := component.VenueProjection{ID: "kept", Name: "Original venue"}
	if err := store.Put(ctx, original); err != nil {
		t.Fatal(err)
	}
	service, err := component.NewService(store, &fakeSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	listener, err := New(service, store, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.applyBatch(ctx, []component.Change{
		{Kind: component.ChangeModified, Doc: component.Document{ID: "kept", Data: map[string]any{"parking": true}}},
		{Kind: component.ChangeAdded, Doc: component.Document{ID: "added", Data: map[string]any{"name": "Added venue"}}},
	}, false); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(ctx, original.ID); err != nil || !got.Equal(original) {
		t.Fatalf("skipped malformed delta changed existing venue: %+v, %v", got, err)
	}
	assertListenerVersion(t, base, original.ID, 1, false)
	assertListenerVersion(t, base, "added", 2, false)
	assertListenerSequence(t, base, 2)
	if !strings.Contains(logs.String(), "skip malformed venue cache document") || !strings.Contains(logs.String(), `"venue_id":"kept"`) {
		t.Fatalf("skipped delta was not observable: %s", logs.String())
	}
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
