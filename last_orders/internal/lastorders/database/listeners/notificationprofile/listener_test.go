package notificationprofile

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
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
	public    [][]notificationprofile.Change
	endpoints [][]notificationprofile.Change
	deactived []string
}

func (s *fakeSource) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return s.next(ctx, &s.users), nil
}

func (s *fakeSource) WatchPublicUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return s.next(ctx, &s.public), nil
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
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestHarness(t *testing.T, source *fakeSource) (*notificationprofile.Store, *Listener) {
	t.Helper()
	if source.public == nil {
		source.public = [][]notificationprofile.Change{{}}
	}
	store, listener, _ := newSourceHarness(t, source)
	return store, listener
}

func newSourceHarness(t *testing.T, source notificationprofile.Source) (*notificationprofile.Store, *Listener, *sql.DB) {
	t.Helper()
	db := newTestDB(t)
	base, err := basestore.New(db)
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
	return store, listener, db
}

type controlledBatch struct {
	changes []notificationprofile.Change
	err     error
}

type controlledStream struct {
	ctx       context.Context
	requested chan struct{}
	response  chan controlledBatch
	stopped   chan struct{}
}

func newControlledStream() *controlledStream {
	return &controlledStream{requested: make(chan struct{}, 1), response: make(chan controlledBatch), stopped: make(chan struct{})}
}

func (stream *controlledStream) Next() ([]notificationprofile.Change, error) {
	select {
	case stream.requested <- struct{}{}:
	case <-stream.ctx.Done():
		return nil, stream.ctx.Err()
	}
	select {
	case batch := <-stream.response:
		return batch.changes, batch.err
	case <-stream.ctx.Done():
		return nil, stream.ctx.Err()
	}
}

func (stream *controlledStream) Stop() { close(stream.stopped) }

type controlledSource struct {
	fakeSource
	privateStreams  []*controlledStream
	publicStreams   []*controlledStream
	endpointStreams []*controlledStream
}

func (source *controlledSource) open(ctx context.Context, streams *[]*controlledStream) (notificationprofile.ChangeStream, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if len(*streams) == 0 {
		return nil, errors.New("unexpected stream reopen")
	}
	stream := (*streams)[0]
	*streams = (*streams)[1:]
	stream.ctx = ctx
	return stream, nil
}

func (source *controlledSource) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return source.open(ctx, &source.privateStreams)
}

func (source *controlledSource) WatchPublicUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return source.open(ctx, &source.publicStreams)
}

func (source *controlledSource) WatchEndpoints(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return source.open(ctx, &source.endpointStreams)
}

func waitForStreamRequest(t *testing.T, ctx context.Context, stream *controlledStream) {
	t.Helper()
	select {
	case <-stream.requested:
	case <-ctx.Done():
		t.Fatal("stream did not request its next batch")
	}
}

func sendControlledBatch(t *testing.T, ctx context.Context, stream *controlledStream, changes []notificationprofile.Change) {
	t.Helper()
	select {
	case stream.response <- controlledBatch{changes: changes}:
	case <-ctx.Done():
		t.Fatal("stream did not receive its batch")
	}
	waitForStreamRequest(t, ctx, stream)
}

func currentProfileVersion(t *testing.T, db *sql.DB, userID string) (int64, bool) {
	t.Helper()
	var version int64
	var deleted bool
	if err := db.QueryRow(`SELECT change_version, deleted FROM notification_profiles WHERE user_id = ?`, userID).Scan(&version, &deleted); err != nil {
		t.Fatal(err)
	}
	return version, deleted
}

func TestThreeStreamArrivalPreservesRawFieldsAndReadiness(t *testing.T) {
	orders := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, order := range orders {
		t.Run(fmt.Sprintf("%v", order), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			private, public, endpoints := newControlledStream(), newControlledStream(), newControlledStream()
			source := &controlledSource{privateStreams: []*controlledStream{private}, publicStreams: []*controlledStream{public}, endpointStreams: []*controlledStream{endpoints}}
			store, listener, db := newSourceHarness(t, source)
			if err := listener.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			streams := []*controlledStream{private, public, endpoints}
			for _, stream := range streams {
				waitForStreamRequest(t, ctx, stream)
			}
			batches := [][]notificationprofile.Change{
				{userChange(notificationprofile.ChangeAdded, "user", map[string]any{"webPushEnabled": true, "email": "private@example.com", "testEmailReq": "request"})},
				{userChange(notificationprofile.ChangeAdded, "user", map[string]any{"uid": "ignored", "name": nil, "photoUrl": "", "votesVisible": false})},
				{endpointChange(notificationprofile.ChangeAdded, "user", "device")},
			}
			privateLoaded, endpointLoaded := false, false
			for index, streamIndex := range order {
				sendControlledBatch(t, ctx, streams[streamIndex], batches[streamIndex])
				privateLoaded = privateLoaded || streamIndex == 0
				endpointLoaded = endpointLoaded || streamIndex == 2
				select {
				case <-listener.Ready():
					if index != 2 {
						t.Fatal("readiness closed before all three baselines committed")
					}
				default:
					if index == 2 {
						t.Fatal("all baselines committed without readiness")
					}
				}
				got, err := store.EligibleEndpoints(ctx, notificationprofile.Selector{Kind: notificationprofile.KindPollOpens})
				if err != nil || (len(got) == 1) != (privateLoaded && endpointLoaded) {
					t.Fatalf("partial arrival push selection = %+v, %v", got, err)
				}
			}
			var rawDefaultsMissing bool
			var email, request string
			if err := db.QueryRow(`SELECT poll_opens IS NULL AND poll_completes IS NULL AND open_poll_email IS NULL, email, test_email_req FROM notification_user_prefs WHERE user_id = 'user'`).Scan(&rawDefaultsMissing, &email, &request); err != nil || !rawDefaultsMissing || email != "private@example.com" || request != "request" {
				t.Fatalf("private ingestion lost raw fields: missing=%v, email=%q, request=%q, err=%v", rawDefaultsMissing, email, request, err)
			}
			var namePresent, nameNull, photoPresent, votesPresent bool
			var photo string
			var votes bool
			if err := db.QueryRow(`SELECT name_present, name IS NULL, photo_url_present, photo_url, votes_visible_present, votes_visible FROM notification_public_users WHERE user_id = 'user'`).Scan(&namePresent, &nameNull, &photoPresent, &photo, &votesPresent, &votes); err != nil || !namePresent || !nameNull || !photoPresent || photo != "" || !votesPresent || votes {
				t.Fatalf("public ingestion lost nullable states: %v", err)
			}
			before, _ := currentProfileVersion(t, db, "user")
			for index, stream := range streams {
				sendControlledBatch(t, ctx, stream, batches[index])
			}
			if version, deleted := currentProfileVersion(t, db, "user"); version != before || deleted {
				t.Fatalf("identical deltas changed profile: version=%d, deleted=%v", version, deleted)
			}
			remove := []notificationprofile.Change{userChange(notificationprofile.ChangeRemoved, "user", nil)}
			sendControlledBatch(t, ctx, private, remove)
			if version, deleted := currentProfileVersion(t, db, "user"); version != before+1 || deleted {
				t.Fatalf("private removal lost public profile: version=%d, deleted=%v", version, deleted)
			}
			if got, err := store.EligibleEndpoints(ctx, notificationprofile.Selector{Kind: notificationprofile.KindDiagnostic}); err != nil || len(got) != 0 {
				t.Fatalf("removed private user can receive push: %+v, %v", got, err)
			}
			sendControlledBatch(t, ctx, public, remove)
			if version, deleted := currentProfileVersion(t, db, "user"); version != before+2 || !deleted {
				t.Fatalf("last-source removal did not tombstone: version=%d, deleted=%v", version, deleted)
			}
			sendControlledBatch(t, ctx, endpoints, []notificationprofile.Change{endpointChange(notificationprofile.ChangeModified, "user", "device")})
			if version, deleted := currentProfileVersion(t, db, "user"); version != before+2 || !deleted {
				t.Fatal("orphan endpoint update resurrected the profile")
			}
			cancel()
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			for _, stream := range streams {
				select {
				case <-stream.stopped:
				default:
					t.Fatal("stream not stopped on listener shutdown")
				}
			}
		})
	}
}

func TestPrivateReconnectPreservesOtherSourceContributions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first, replayed, removed := newControlledStream(), newControlledStream(), newControlledStream()
	public, endpoints := newControlledStream(), newControlledStream()
	source := &controlledSource{
		privateStreams: []*controlledStream{first, replayed, removed},
		publicStreams:  []*controlledStream{public}, endpointStreams: []*controlledStream{endpoints},
	}
	store, listener, db := newSourceHarness(t, source)
	if err := listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, stream := range []*controlledStream{first, public, endpoints} {
		waitForStreamRequest(t, ctx, stream)
	}
	privateBatch := []notificationprofile.Change{userChange(notificationprofile.ChangeAdded, "user", map[string]any{"webPushEnabled": true})}
	sendControlledBatch(t, ctx, first, privateBatch)
	sendControlledBatch(t, ctx, public, []notificationprofile.Change{userChange(notificationprofile.ChangeAdded, "user", map[string]any{"name": "Public name"})})
	sendControlledBatch(t, ctx, endpoints, []notificationprofile.Change{endpointChange(notificationprofile.ChangeAdded, "user", "device")})
	before, _ := currentProfileVersion(t, db, "user")
	for _, transition := range []struct {
		previous *controlledStream
		next     *controlledStream
		changes  []notificationprofile.Change
		want     int64
	}{
		{first, replayed, privateBatch, before},
		{replayed, removed, nil, before + 1},
	} {
		select {
		case transition.previous.response <- controlledBatch{err: io.ErrUnexpectedEOF}:
		case <-ctx.Done():
			t.Fatal("could not interrupt source stream")
		}
		waitForStreamRequest(t, ctx, transition.next)
		select {
		case <-transition.previous.stopped:
		default:
			t.Fatal("failed stream was not stopped before reopening")
		}
		sendControlledBatch(t, ctx, transition.next, transition.changes)
		if version, deleted := currentProfileVersion(t, db, "user"); version != transition.want || deleted {
			t.Fatalf("reconnected profile = version %d, deleted %v; want %d, false", version, deleted, transition.want)
		}
		var name string
		if err := db.QueryRow(`SELECT name FROM notification_public_users WHERE user_id = 'user'`).Scan(&name); err != nil || name != "Public name" {
			t.Fatalf("private reconnect changed public data: %q, %v", name, err)
		}
		var endpointCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notification_endpoints WHERE user_id = 'user'`).Scan(&endpointCount); err != nil || endpointCount != 1 {
			t.Fatalf("private reconnect cleared endpoints: %d, %v", endpointCount, err)
		}
		select {
		case <-listener.Ready():
		default:
			t.Fatal("reconnect reset established adapter readiness")
		}
	}
	if _, err := store.Preferences(ctx, "user"); !errors.Is(err, notificationprofile.ErrNotFound) {
		t.Fatalf("empty private baseline retained offline-deleted user: %v", err)
	}
	if got, err := store.EligibleEndpoints(ctx, notificationprofile.Selector{Kind: notificationprofile.KindDiagnostic}); err != nil || len(got) != 0 {
		t.Fatalf("offline-deleted private user can receive push: %+v, %v", got, err)
	}
	sendControlledBatch(t, ctx, removed, privateBatch)
	if version, deleted := currentProfileVersion(t, db, "user"); version != before+2 || deleted {
		t.Fatalf("private recreation after reconnect: version=%d, deleted=%v", version, deleted)
	}
}

func TestMalformedThreeStreamBaselineLeavesProjectionUnchanged(t *testing.T) {
	for _, invalidSource := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("source=%d", invalidSource), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			streams := []*controlledStream{newControlledStream(), newControlledStream(), newControlledStream()}
			source := &controlledSource{privateStreams: []*controlledStream{streams[0]}, publicStreams: []*controlledStream{streams[1]}, endpointStreams: []*controlledStream{streams[2]}}
			store, listener, db := newSourceHarness(t, source)
			if err := store.PutPrivateUser(ctx, notificationprofile.PrivateUserContribution{UserID: "user"}); err != nil {
				t.Fatal(err)
			}
			if err := store.PutPublicUser(ctx, notificationprofile.PublicUserContribution{UserID: "user"}); err != nil {
				t.Fatal(err)
			}
			if err := store.PutEndpointContribution(ctx, notificationprofile.EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth"}); err != nil {
				t.Fatal(err)
			}
			before, _ := currentProfileVersion(t, db, "user")
			if err := listener.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			for _, stream := range streams {
				waitForStreamRequest(t, ctx, stream)
			}
			invalid := []notificationprofile.Change{
				userChange(notificationprofile.ChangeAdded, "broken", map[string]any{"webPushEnabled": "yes"}),
				userChange(notificationprofile.ChangeAdded, "broken", map[string]any{"name": false}),
				{Kind: notificationprofile.ChangeAdded, Doc: notificationprofile.Document{ID: "broken", UserID: "user", Data: map[string]any{"endpoint": "url"}}},
			}
			select {
			case streams[invalidSource].response <- controlledBatch{changes: []notificationprofile.Change{invalid[invalidSource]}}:
			case <-ctx.Done():
				t.Fatal("could not supply malformed baseline")
			}
			select {
			case err := <-listener.Errors():
				if !errors.Is(err, ErrMalformedDocument) {
					t.Fatalf("baseline error = %v; want malformed document", err)
				}
			case <-ctx.Done():
				t.Fatal("malformed baseline failure was not reported")
			}
			select {
			case <-listener.Ready():
				t.Fatal("malformed baseline signalled readiness")
			default:
			}
			if version, deleted := currentProfileVersion(t, db, "user"); version != before || deleted {
				t.Fatalf("malformed baseline changed version: %d, %v", version, deleted)
			}
			for _, table := range []string{"notification_user_prefs", "notification_public_users", "notification_endpoints"} {
				var count int
				if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
					t.Fatalf("malformed baseline changed %s: %d, %v", table, count, err)
				}
			}
		})
	}
}

func TestMalformedPublicDeltaRetainsContributionAndLogsRejection(t *testing.T) {
	store, listener, db := newSourceHarness(t, &fakeSource{})
	ctx := t.Context()
	var logs bytes.Buffer
	listener.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if err := listener.applyUsers(ctx, []notificationprofile.Change{
		userChange(notificationprofile.ChangeAdded, "user", map[string]any{"email": "private@example.com"}),
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := listener.applyPublicUsers(ctx, []notificationprofile.Change{
		userChange(notificationprofile.ChangeAdded, "user", map[string]any{"name": "Keep public name"}),
	}, true); err != nil {
		t.Fatal(err)
	}
	before, _ := currentProfileVersion(t, db, "user")
	if err := listener.applyPublicUsers(ctx, []notificationprofile.Change{
		userChange(notificationprofile.ChangeModified, "user", map[string]any{"name": false}),
		userChange(notificationprofile.ChangeAdded, "other", map[string]any{"name": "Valid public name"}),
	}, false); err != nil {
		t.Fatal(err)
	}
	if version, deleted := currentProfileVersion(t, db, "user"); version != before || deleted {
		t.Fatalf("invalid delta changed existing profile: version=%d, deleted=%v", version, deleted)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM notification_public_users WHERE user_id = 'user'`).Scan(&name); err != nil || name != "Keep public name" {
		t.Fatalf("invalid delta changed public contribution: %q, %v", name, err)
	}
	if _, err := store.Preferences(ctx, "user"); err != nil {
		t.Fatalf("public delta erased private contribution: %v", err)
	}
	if version, deleted := currentProfileVersion(t, db, "other"); version != before+1 || deleted {
		t.Fatalf("valid delta was not applied: version=%d, deleted=%v", version, deleted)
	}
	if !strings.Contains(logs.String(), "skip malformed public profile document") || !strings.Contains(logs.String(), `"doc_id":"user"`) {
		t.Fatalf("invalid public delta was not observable: %s", logs.String())
	}
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

func TestListenerReadyAfterAllThreeEmptyFirstSnapshots(t *testing.T) {
	source := &fakeSource{
		users:     [][]notificationprofile.Change{{}},
		endpoints: [][]notificationprofile.Change{{}},
	}
	_, listener := newTestHarness(t, source)
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	waitForReady(t, listener)
	// The scripted streams keep reconnecting; marking readiness again must not panic.
	time.Sleep(50 * time.Millisecond)
	waitForReady(t, listener)
}

func TestListenerNotReadyUntilEveryStreamLoads(t *testing.T) {
	source := &fakeSource{
		users: [][]notificationprofile.Change{{userChange(notificationprofile.ChangeAdded, "user", map[string]any{"notificationEmail": "user@example.com", "openPollEmailEnabled": true})}},
	}
	store, listener := newTestHarness(t, source)
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		recipients, err := store.EmailRecipients(context.Background(), notificationprofile.EmailPollOpens)
		if err != nil {
			t.Fatalf("email recipients: %v", err)
		}
		if len(recipients) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the user snapshot")
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-listener.Ready():
		t.Fatal("listener ready before the endpoint stream loaded")
	default:
	}
}

func waitForReady(t *testing.T, listener *Listener) {
	t.Helper()
	select {
	case <-listener.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for listener readiness")
	}
}

func TestListenerAppliesRemovals(t *testing.T) {
	optedIn := map[string]any{"webPushEnabled": true}
	source := &fakeSource{
		users: [][]notificationprofile.Change{{userChange(notificationprofile.ChangeAdded, "user", optedIn)}},
		endpoints: [][]notificationprofile.Change{
			{endpointChange(notificationprofile.ChangeAdded, "user", "a"), endpointChange(notificationprofile.ChangeAdded, "user", "b")},
			{endpointChange(notificationprofile.ChangeAdded, "user", "b")},
		},
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

func TestListenerDoesNotReplaceProjectionFromMalformedBaseline(t *testing.T) {
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
	ctx := context.Background()
	if err := store.PutPreferences(ctx, notificationprofile.UserPreferences{UserID: "user", WebPushEnabled: true}); err != nil {
		t.Fatalf("seed preferences: %v", err)
	}
	if err := store.PutEndpoint(ctx, notificationprofile.Endpoint{
		UserID: "user", EndpointID: "existing", URL: "https://push.test/existing", P256DH: "key", Auth: "secret", Active: true,
	}); err != nil {
		t.Fatalf("seed existing endpoint: %v", err)
	}
	if err := listener.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Close()

	select {
	case err := <-listener.Errors():
		if !errors.Is(err, ErrMalformedDocument) {
			t.Fatalf("listener error = %v; want malformed-document error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not report malformed baseline")
	}
	select {
	case <-listener.Ready():
		t.Fatal("listener became ready after rejecting the endpoint baseline")
	default:
	}
	waitForEndpointKeys(t, store, "user/existing")
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
