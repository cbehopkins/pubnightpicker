package notificationprofile

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/notificationprofile"

	_ "modernc.org/sqlite"
)

// fakeSource hands out pre-scripted streams. Each queued stream yields its batch and
// then fails, driving the listener through a reconnect. The final batch is replayed on
// every subsequent stream, mirroring Firestore's full snapshot on reconnect.
type fakeSource struct {
	mu        sync.Mutex
	users     [][]notificationprofile.Change
	endpoints [][]notificationprofile.Change
	deactived []string
}

func (s *fakeSource) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return s.next(ctx, &s.users), nil
}

func (s *fakeSource) WatchEndpoints(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return s.next(ctx, &s.endpoints), nil
}

func (s *fakeSource) next(ctx context.Context, queue *[][]notificationprofile.Change) notificationprofile.ChangeStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(*queue) == 0 {
		return &fakeStream{ctx: ctx}
	}
	batch := (*queue)[0]
	if len(*queue) > 1 {
		*queue = (*queue)[1:]
	}
	return &fakeStream{ctx: ctx, batch: batch}
}

func (s *fakeSource) DeactivateEndpoint(_ context.Context, userID, endpointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deactived = append(s.deactived, userID+"/"+endpointID)
	return nil
}

type fakeStream struct {
	ctx      context.Context
	batch    []notificationprofile.Change
	returned bool
}

func (s *fakeStream) Next() ([]notificationprofile.Change, error) {
	if s.batch != nil && !s.returned {
		s.returned = true
		return s.batch, nil
	}
	if s.batch != nil {
		return nil, io.ErrUnexpectedEOF
	}
	<-s.ctx.Done()
	return nil, context.Canceled
}

func (s *fakeStream) Stop() {}

// newTestDB uses a file-backed database because the listener reads and writes from
// several goroutines, and each pooled connection to ":memory:" would be a separate
// database.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "projection.db")
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestHarness(t *testing.T, source *fakeSource) (*notificationprofile.Store, *Listener) {
	t.Helper()
	base, err := basestore.New(newTestDB(t))
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	store, err := notificationprofile.New(base)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service, err := notificationprofile.NewService(store, source, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	listener.retryDelay = 5 * time.Millisecond
	return store, listener
}

func userChange(kind notificationprofile.ChangeKind, id string, data map[string]any) notificationprofile.Change {
	return notificationprofile.Change{Kind: kind, Doc: notificationprofile.Document{ID: id, Data: data}}
}

func endpointChange(kind notificationprofile.ChangeKind, userID, id string) notificationprofile.Change {
	return notificationprofile.Change{Kind: kind, Doc: notificationprofile.Document{
		ID: id, UserID: userID,
		Data: map[string]any{"endpoint": "https://push.test/" + id, "p256dh": "key", "auth": "secret", "active": true},
	}}
}

func waitForEndpointKeys(t *testing.T, store *notificationprofile.Store, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []string
	for {
		endpoints, err := store.EligibleEndpoints(context.Background(), notificationprofile.Selector{Kind: notificationprofile.KindPollOpens})
		if err != nil {
			t.Fatalf("eligible endpoints: %v", err)
		}
		got = got[:0]
		for _, endpoint := range endpoints {
			got = append(got, endpoint.Key())
		}
		if len(got) == len(want) {
			matched := true
			for i := range got {
				if got[i] != want[i] {
					matched = false
					break
				}
			}
			if matched {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoints = %v; want %v", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestListenerProjectsUsersAndEndpoints(t *testing.T) {
	optedIn := map[string]any{"webPushEnabled": true}
	source := &fakeSource{
		users:     [][]notificationprofile.Change{{userChange(notificationprofile.ChangeAdded, "user", optedIn)}},
		endpoints: [][]notificationprofile.Change{{endpointChange(notificationprofile.ChangeAdded, "user", "a")}},
	}
	store, listener := newTestHarness(t, source)
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	waitForEndpointKeys(t, store, "user/a")
}

func TestListenerAppliesRemovals(t *testing.T) {
	optedIn := map[string]any{"webPushEnabled": true}
	source := &fakeSource{
		users: [][]notificationprofile.Change{{userChange(notificationprofile.ChangeAdded, "user", optedIn)}},
		endpoints: [][]notificationprofile.Change{{
			endpointChange(notificationprofile.ChangeAdded, "user", "a"),
			endpointChange(notificationprofile.ChangeAdded, "user", "b"),
			endpointChange(notificationprofile.ChangeRemoved, "user", "a"),
		}},
	}
	store, listener := newTestHarness(t, source)
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	waitForEndpointKeys(t, store, "user/b")
}

// A lost projection must be rebuilt from the snapshot replayed on reconnect,
// without any checkpointing of its own. See docs/cdd/0009-notification-projection.md.
func TestListenerRebuildsProjectionAfterLocalDataLoss(t *testing.T) {
	optedIn := map[string]any{"webPushEnabled": true}
	snapshot := []notificationprofile.Change{endpointChange(notificationprofile.ChangeAdded, "user", "a")}
	source := &fakeSource{
		users:     [][]notificationprofile.Change{{userChange(notificationprofile.ChangeAdded, "user", optedIn)}},
		endpoints: [][]notificationprofile.Change{snapshot},
	}
	store, listener := newTestHarness(t, source)
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	waitForEndpointKeys(t, store, "user/a")

	if err := store.DeleteEndpoint(context.Background(), "user", "a"); err != nil {
		t.Fatalf("simulate data loss: %v", err)
	}
	if err := store.DeletePreferences(context.Background(), "user"); err != nil {
		t.Fatalf("simulate data loss: %v", err)
	}

	// The scripted stream fails after each batch, so the listener reconnects and
	// replays the snapshot, which is the only recovery mechanism the projection has.
	waitForEndpointKeys(t, store, "user/a")
}

func TestListenerSkipsUnprojectableDocuments(t *testing.T) {
	optedIn := map[string]any{"webPushEnabled": true}
	malformed := notificationprofile.Change{Kind: notificationprofile.ChangeAdded, Doc: notificationprofile.Document{
		ID: "broken", UserID: "user", Data: map[string]any{"endpoint": "https://push.test/broken"},
	}}
	source := &fakeSource{
		users: [][]notificationprofile.Change{{userChange(notificationprofile.ChangeAdded, "user", optedIn)}},
		endpoints: [][]notificationprofile.Change{{
			malformed,
			endpointChange(notificationprofile.ChangeAdded, "user", "a"),
		}},
	}
	store, listener := newTestHarness(t, source)
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	waitForEndpointKeys(t, store, "user/a")
}

func TestServiceInvalidateEndpointWritesBackAndUpdatesProjection(t *testing.T) {
	source := &fakeSource{}
	base, err := basestore.New(newTestDB(t))
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	store, err := notificationprofile.New(base)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service, err := notificationprofile.NewService(store, source, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx := context.Background()
	if err := store.PutPreferences(ctx, notificationprofile.UserPreferences{UserID: "user", WebPushEnabled: true, PollOpens: true}); err != nil {
		t.Fatalf("put preferences: %v", err)
	}
	if err := store.PutEndpoint(ctx, notificationprofile.Endpoint{UserID: "user", EndpointID: "a", URL: "https://push.test/a", P256DH: "k", Auth: "s", Active: true}); err != nil {
		t.Fatalf("put endpoint: %v", err)
	}

	if err := service.InvalidateEndpoint(ctx, "user", "a"); err != nil {
		t.Fatalf("invalidate endpoint: %v", err)
	}
	if len(source.deactived) != 1 || source.deactived[0] != "user/a" {
		t.Fatalf("deactivated = %v; want [user/a] written back to Firebase", source.deactived)
	}
	endpoints, err := service.GetEligiblePushEndpoints(ctx, notificationprofile.Selector{Kind: notificationprofile.KindPollOpens})
	if err != nil {
		t.Fatalf("eligible endpoints: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("endpoints = %v; want the invalidated endpoint excluded", endpoints)
	}
}
