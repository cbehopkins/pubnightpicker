package meets

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/currentmeetcache"
	groupcomponent "last_orders/internal/lastorders/components/groupcache"
	"last_orders/internal/lastorders/components/sse"
	groupcachelistener "last_orders/internal/lastorders/database/listeners/groupcache"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type reconnectingGroupSource struct {
	base         groupcomponent.Source
	mu           sync.Mutex
	calls        int
	opened       chan int
	disconnected chan struct{}
}

func (source *reconnectingGroupSource) Watch(ctx context.Context) (groupcomponent.ChangeStream, error) {
	stream, err := source.base.Watch(ctx)
	if err != nil {
		return nil, err
	}
	source.mu.Lock()
	source.calls++
	call := source.calls
	source.mu.Unlock()
	source.opened <- call
	return &reconnectingGroupStream{
		ctx: ctx, source: stream, failAfterBaseline: call == 1, disconnected: source.disconnected,
	}, nil
}

type reconnectingGroupStream struct {
	ctx               context.Context
	source            groupcomponent.ChangeStream
	failAfterBaseline bool
	baseline          bool
	disconnected      chan struct{}
}

func (stream *reconnectingGroupStream) Next() ([]groupcomponent.Change, error) {
	if !stream.baseline {
		changes, err := stream.source.Next()
		if err == nil {
			stream.baseline = true
		}
		return changes, err
	}
	if stream.failAfterBaseline {
		stream.failAfterBaseline = false
		close(stream.disconnected)
		return nil, errors.New("simulated Groups watch disconnect")
	}
	select {
	case <-stream.ctx.Done():
		return nil, stream.ctx.Err()
	default:
		return stream.source.Next()
	}
}

func (stream *reconnectingGroupStream) Stop() {
	stream.source.Stop()
}

func TestGroupWatchReconnectReauthorizesActiveMeetStreamAgainstEmulator(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-group-reconnect-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	group := client.Collection("groups").Doc("private-group")
	if _, err := group.Set(ctx, map[string]any{
		"name": "Private Group", "visibility": "private", "members": map[string]any{"verified-user": true},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = group.Delete(cleanupCtx)
	})

	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	base, err := basestore.New(database)
	if err != nil {
		t.Fatal(err)
	}
	groupStore, err := groupcomponent.NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	groupSource, err := groupcomponent.NewFirestoreSource(client)
	if err != nil {
		t.Fatal(err)
	}
	hub := sse.NewHub()
	reconnectingSource := &reconnectingGroupSource{
		base: groupSource, opened: make(chan int, 2), disconnected: make(chan struct{}),
	}
	projection, err := groupcomponent.NewService(groupStore, reconnectingSource)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := groupcachelistener.New(projection, groupStore, slog.New(slog.NewTextHandler(io.Discard, nil)), hub)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := NewFirestoreGroupAuthorizer(client)
	if err != nil {
		t.Fatal(err)
	}
	invalidator, ok := authorizer.(groupcachelistener.AuthorizationInvalidator)
	if !ok {
		t.Fatal("Firestore Group authorizer does not implement Group invalidation")
	}
	listener.SetAuthorizationInvalidator(invalidator)
	if err := listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	select {
	case <-listener.Ready():
	case err := <-listener.Errors():
		t.Fatalf("Group listener baseline failed: %v", err)
	case <-ctx.Done():
		t.Fatal("Group listener baseline did not complete")
	}
	if opened := <-reconnectingSource.opened; opened != 1 {
		t.Fatalf("first Group watch=%d, want 1", opened)
	}
	select {
	case <-reconnectingSource.disconnected:
	case <-ctx.Done():
		t.Fatal("Group watch did not disconnect after baseline")
	}

	ready := make(chan struct{})
	close(ready)
	endpoint := &Endpoint{
		Current: &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
			Date: "2026-11-20", Generation: "generation-1",
		}},
		Groups: authorizer, Auth: pastMeetAuthStub{}, CurrentReady: ready,
		CurrentHub: hub, Cursors: testCurrentCursorCodec(t),
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	streamCtx, stopStream := context.WithCancel(ctx)
	t.Cleanup(stopStream)
	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodGet, server.URL+"/api/meets/private-group/current/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamRequest.Header.Set("Authorization", "Bearer valid-token")
	response, err := server.Client().Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	reader := bufio.NewReader(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initial private-Group stream status=%d", response.StatusCode)
	}
	initialFrame, err := readMeetSSEFrame(reader)
	if err != nil {
		t.Fatalf("read initial stream frame: %v", err)
	}
	if !strings.Contains(initialFrame, "event: current_meets") {
		t.Fatalf("initial stream frame=%q, want current_meets", initialFrame)
	}

	if _, err := group.Update(ctx, []firestore.Update{
		{Path: "members", Value: map[string]any{"verified-user": false}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case opened := <-reconnectingSource.opened:
		if opened != 2 {
			t.Fatalf("reconnected Group watch=%d, want 2", opened)
		}
	case <-ctx.Done():
		t.Fatal("Group watch did not reconnect")
	}
	reset := make(chan struct {
		frame string
		err   error
	}, 1)
	go func() {
		frame, err := readMeetSSEFrame(reader)
		reset <- struct {
			frame string
			err   error
		}{frame: frame, err: err}
	}()
	select {
	case result := <-reset:
		if result.err != nil {
			t.Fatalf("read reconnect reset: %v", result.err)
		}
		if !strings.Contains(result.frame, "event: view_reset") || !strings.Contains(result.frame, "data: {}") {
			t.Fatalf("reconnect did not reset the active stream: %q", result.frame)
		}
	case <-ctx.Done():
		t.Fatal("active Meet stream did not reset on Group watch reconnect")
	}
	stopStream()
	response.Body.Close()
	request := httptest.NewRequest(http.MethodGet, "/api/meets/private-group/current", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	apiResponse := httptest.NewRecorder()
	mux.ServeHTTP(apiResponse, request)
	if apiResponse.Code != http.StatusNotFound {
		t.Fatalf("post-reconnect private-Group GET status=%d body=%s, want HTTP 404", apiResponse.Code, apiResponse.Body)
	}
}

func readMeetSSEFrame(reader *bufio.Reader) (string, error) {
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return frame.String(), err
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String(), nil
		}
	}
}
