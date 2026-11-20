package meets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/currentmeetcache"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
)

type currentReaderStub struct {
	snapshot   currentmeetcache.Snapshot
	renewCalls int
	date       string
	readErr    error
}

func (r *currentReaderStub) Get(context.Context, string) (currentmeetcache.Snapshot, error) {
	return r.snapshot, r.readErr
}

func (r *currentReaderStub) Renew(context.Context, string, string) error {
	r.renewCalls++
	return nil
}

func (r *currentReaderStub) CurrentDate() string { return r.date }

type groupAuthorizerStub func(context.Context, string, string) (bool, error)

func (a groupAuthorizerStub) CanRead(ctx context.Context, uid, groupID string) (bool, error) {
	return a(ctx, uid, groupID)
}

type flushableRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func (w *flushableRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *flushableRecorder) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() { close(w.flushed) })
}

func TestRegisterCurrentMeetsGetUnavailableWithoutProjection(t *testing.T) {
	mux := http.NewServeMux()
	(&Endpoint{}).Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("current-Meets GET status=%d body=%s, want HTTP 503", response.Code, response.Body)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "projection_unavailable" {
		t.Fatalf("error code=%q, want projection_unavailable", payload.Error.Code)
	}
}

func TestCurrentMeetsGetReturnsCompletePublicSnapshot(t *testing.T) {
	reader := &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
		Date: "2026-11-20", Generation: "generation-1", Sequence: 9,
		Meets: []currentmeetcache.MeetView{
			{
				Meet:  meetcache.Projection{ID: "meet-1", GroupID: "group-a", Date: "2026-11-20", VotingComplete: true, CandidateVenueIDs: []string{"venue-1"}},
				Venue: &currentmeetcache.VenueSummary{ID: "venue-1", Status: "deleted", Name: "Old Pub", Address: "Old Street", VenueType: "pub"},
			},
			{Meet: meetcache.Projection{ID: "meet-2", GroupID: "group-a", Date: "2026-11-21", VotingComplete: true, CandidateVenueIDs: []string{}}, MissingVenueID: "missing-venue"},
		},
	}}
	endpoint := &Endpoint{
		Current: reader, Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "" && groupID == "group-a", nil
		}),
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("current-Meets GET status=%d body=%s", response.Code, response.Body)
	}
	var payload struct {
		Cursor string `json:"cursor"`
		Meets  []struct {
			ID    string `json:"id"`
			Venue struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"venue"`
		} `json:"meets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Cursor == "" || len(payload.Meets) != 2 || payload.Meets[0].ID != "meet-1" || payload.Meets[0].Venue.Status != "deleted" || payload.Meets[1].Venue.ID != "missing-venue" || payload.Meets[1].Venue.Status != "missing" {
		t.Fatalf("current-Meets response=%+v", payload)
	}
	if strings.Contains(response.Body.String(), "attendance") || strings.Contains(response.Body.String(), "votes") {
		t.Fatalf("response contains voting or attendance data: %s", response.Body)
	}
}

func TestCurrentMeetsStreamSendsReplacementForInvalidCursorAndRenews(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
		Date: "2026-11-20", Generation: "generation-1", Sequence: 9,
	}}
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current/stream?after=invalid", nil).WithContext(ctx)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-response.flushed:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("current-Meets stream did not flush its initial snapshot")
	}
	cancel()
	<-done
	body := response.Body.String()
	if !strings.Contains(body, "event: current_meets") || !strings.Contains(body, `"meets":[]`) || !strings.Contains(body, "id: v1.") {
		t.Fatalf("stream did not send a complete snapshot and cursor: %s", body)
	}
	if reader.renewCalls != 1 {
		t.Fatalf("successful stream renewed %d times, want once", reader.renewCalls)
	}
}

func TestDeniedCurrentMeetsStreamDoesNotRenew(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20"}
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return false, nil }),
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current/stream", nil))
	if response.Code != http.StatusNotFound || reader.renewCalls != 0 {
		t.Fatalf("denied stream status=%d renewals=%d, want HTTP 404 and no renewal", response.Code, reader.renewCalls)
	}
}

func TestCurrentMeetsStreamReauthorizesBeforeRenewal(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
		Date: "2026-11-20", Generation: "generation-1", Sequence: 9,
	}}
	authorizationChecks := 0
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) {
			authorizationChecks++
			return authorizationChecks == 1, nil
		}),
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current/stream", nil))
	if authorizationChecks != 2 || response.Code != http.StatusNotFound || reader.renewCalls != 0 {
		t.Fatalf("checks=%d status=%d renewals=%d, want second-check denial without renewal", authorizationChecks, response.Code, reader.renewCalls)
	}
}

func TestCurrentMeetsStreamReturnsUnavailableBeforeBaseline(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20", readErr: currentmeetcache.ErrUnavailable}
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current/stream", nil))
	if response.Code != http.StatusServiceUnavailable || reader.renewCalls != 0 {
		t.Fatalf("unavailable stream status=%d renewals=%d, want HTTP 503 without renewal", response.Code, reader.renewCalls)
	}
}

func testCurrentCursorCodec(t *testing.T) *sse.CursorCodec {
	t.Helper()
	codec, err := sse.NewCursorCodec([]byte("test-only-current-meet-cursor-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}
