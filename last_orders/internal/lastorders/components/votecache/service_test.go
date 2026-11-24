package votecache

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

	_ "modernc.org/sqlite"
)

type votingTestSource struct {
	mu          sync.Mutex
	document    Document
	documentErr error
	watchCount  int
}

func (source *votingTestSource) Watch(ctx context.Context, _ string) (Stream, error) {
	source.mu.Lock()
	source.watchCount++
	document := source.document
	documentErr := source.documentErr
	source.mu.Unlock()
	return &votingTestStream{ctx: ctx, document: document, documentErr: documentErr}, nil
}

func (source *votingTestSource) watches() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.watchCount
}

type votingTestStream struct {
	ctx         context.Context
	document    Document
	documentErr error
	first       bool
}

func (stream *votingTestStream) Next() (Document, error) {
	if !stream.first {
		stream.first = true
		return stream.document, stream.documentErr
	}
	<-stream.ctx.Done()
	return Document{}, stream.ctx.Err()
}

func (stream *votingTestStream) Stop() {}

func newVotingTestService(t *testing.T, source Source) (*Service, *Store) {
	t.Helper()
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
	service, err := NewService(store, source, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, store
}

func TestProjectionFromDocumentDeduplicatesLegacyVotes(t *testing.T) {
	projection, err := ProjectionFromDocument(Document{
		ID: "meet-a", Exists: true, Data: map[string]any{
			"venue-1": []any{"voter-b", "voter-a", "voter-b"},
			"any":     []string{"voter-a"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	venueVotes := projection.Votes["venue-1"]
	if len(venueVotes) != 2 || venueVotes[0] != "voter-a" || venueVotes[1] != "voter-b" {
		t.Fatalf("venue votes=%v, want sorted distinct UIDs", venueVotes)
	}
	if len(projection.Votes["any"]) != 1 || projection.Votes["any"][0] != "voter-a" {
		t.Fatalf("any votes=%v, want the stored UID", projection.Votes["any"])
	}
}

func TestServiceSharesOneWatchAndServesCommittedBaseline(t *testing.T) {
	source := &votingTestSource{document: Document{
		ID: "meet-a", Exists: true, Data: map[string]any{"venue-1": []any{"voter-a", "voter-a"}},
	}}
	service, _ := newVotingTestService(t, source)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	const callers = 8
	start := make(chan struct{})
	results := make(chan error, callers)
	var callersDone sync.WaitGroup
	for range callers {
		callersDone.Add(1)
		go func() {
			defer callersDone.Done()
			<-start
			snapshot, err := service.Get(ctx, "meet-a")
			if err == nil && (len(snapshot.Votes["venue-1"]) != 1 || snapshot.Votes["venue-1"][0] != "voter-a") {
				err = errors.New("snapshot did not contain the committed, deduplicated baseline")
			}
			results <- err
		}()
	}
	close(start)
	callersDone.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent snapshot read: %v", err)
		}
	}
	if got := source.watches(); got != 1 {
		t.Fatalf("source watches=%d, want one", got)
	}
}

func TestServiceTreatsConfirmedMissingDocumentAsEmptySnapshot(t *testing.T) {
	source := &votingTestSource{document: Document{ID: "meet-a", Exists: false}}
	service, _ := newVotingTestService(t, source)
	snapshot, err := service.Get(t.Context(), "meet-a")
	if err != nil {
		t.Fatalf("read missing vote document: %v", err)
	}
	if snapshot.Votes == nil || len(snapshot.Votes) != 0 {
		t.Fatalf("missing vote snapshot=%v, want an empty initialized map", snapshot.Votes)
	}
}

func TestServiceDoesNotServeMalformedInitialBaseline(t *testing.T) {
	source := &votingTestSource{document: Document{
		ID: "meet-a", Exists: true, Data: map[string]any{"venue-1": "not-an-array"},
	}}
	service, _ := newVotingTestService(t, source)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := service.Get(ctx, "meet-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("malformed initial baseline error=%v, want request timeout", err)
	}
}
