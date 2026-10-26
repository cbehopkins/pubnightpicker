package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHandlerSendsInitialAndChangedFullViews(t *testing.T) {
	hub := NewHub()
	ready := make(chan struct{})
	close(ready)
	var mu sync.Mutex
	version := 0
	handler := &Handler{
		Hub: hub, Ready: ready, ViewKey: "group-a-current-meets", AuthorizationScope: "group-a",
		UID: "user-a", EventName: "current_meets", Authorize: func(context.Context) (bool, int) { return true, 0 },
		Snapshot: func(context.Context) (json.RawMessage, error) {
			mu.Lock()
			defer mu.Unlock()
			version++
			return json.RawMessage(`{"version":` + string(rune('0'+version)) + `}`), nil
		},
	}
	w := newStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx))
	}()
	w.waitFlush(t)
	if got := w.body(); !strings.Contains(got, "event: current_meets\ndata: {\"version\":1}\n\n") {
		t.Fatalf("initial event missing: %q", got)
	}
	hub.PublishChanged("unrelated-view")
	hub.PublishChanged("group-a-current-meets")
	w.waitFlush(t)
	if got := w.body(); strings.Count(got, "event: current_meets") != 2 || !strings.Contains(got, `data: {"version":2}`) {
		t.Fatalf("changed full view missing or unrelated view woke handler: %q", got)
	}
	cancel()
	waitDone(t, done)
	if hub.SubscriberCount() != 0 {
		t.Fatal("request cancellation leaked subscription")
	}
}

func TestHandlerSendsFlushedHeartbeatComments(t *testing.T) {
	hub := NewHub()
	handler := testHandler(hub, closedSignal())
	handler.HeartbeatInterval = 5 * time.Millisecond
	w := newStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	}()
	w.waitFlush(t)
	w.waitFlush(t)
	got := w.body()
	if !strings.Contains(got, ": keepalive\n\n") || strings.Count(got, "event: view\n") != 1 {
		t.Fatalf("heartbeat did not remain a non-event SSE comment: %q", got)
	}
	cancel()
	waitDone(t, done)
}

func TestHandlerCoalescesChangesAndDeliversNewestSnapshot(t *testing.T) {
	hub := NewHub()
	var mu sync.Mutex
	version, reads := 1, 0
	readEntered, releaseRead := make(chan struct{}), make(chan struct{})
	handler := testHandler(hub, closedSignal())
	handler.Snapshot = func(context.Context) (json.RawMessage, error) {
		mu.Lock()
		reads++
		read := reads
		current := version
		mu.Unlock()
		if read == 2 {
			close(readEntered)
			<-releaseRead
		}
		return json.RawMessage(`{"version":` + string(rune('0'+current)) + `}`), nil
	}
	w := newStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	}()
	w.waitFlush(t)
	mu.Lock()
	version = 2
	mu.Unlock()
	hub.PublishChanged("view")
	select {
	case <-readEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("second snapshot did not start")
	}
	mu.Lock()
	version = 8
	mu.Unlock()
	for range 20 {
		hub.PublishChanged("view")
	}
	close(releaseRead)
	w.waitFlush(t)
	w.waitFlush(t)
	cancel()
	waitDone(t, done)
	got := w.body()
	mu.Lock()
	gotReads := reads
	mu.Unlock()
	if gotReads != 3 || !strings.Contains(got, `data: {"version":8}`) || strings.Count(got, "event: view\n") != 3 {
		t.Fatalf("reads=%d events=%q; wanted one coalesced follow-up with latest view", gotReads, got)
	}
}

func TestHandlerPrioritizesInvalidationAfterRegistrationAndSnapshot(t *testing.T) {
	t.Run("races authorization", func(t *testing.T) {
		hub := NewHub()
		ready := closedSignal()
		snapshotCalled := false
		handler := testHandler(hub, ready)
		handler.Authorize = func(context.Context) (bool, int) {
			hub.InvalidateUID("user-a")
			return true, 0
		}
		handler.Snapshot = func(context.Context) (json.RawMessage, error) {
			snapshotCalled = true
			return json.RawMessage(`{"private":true}`), nil
		}
		w := newStreamWriter()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if snapshotCalled || w.statusCode() != http.StatusOK || w.body() != "event: view_reset\ndata: {}\n\n" {
			t.Fatalf("snapshot=%v status=%d body=%q", snapshotCalled, w.statusCode(), w.body())
		}
	})

	t.Run("during snapshot read", func(t *testing.T) {
		hub := NewHub()
		ready := closedSignal()
		readEntered, releaseRead := make(chan struct{}), make(chan struct{})
		handler := testHandler(hub, ready)
		handler.Snapshot = func(context.Context) (json.RawMessage, error) {
			close(readEntered)
			<-releaseRead
			return json.RawMessage(`{"private":true}`), nil
		}
		w := newStreamWriter()
		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		}()
		<-readEntered
		hub.InvalidateScope("group-a")
		close(releaseRead)
		waitDone(t, done)
		if got := w.body(); got != "event: view_reset\ndata: {}\n\n" {
			t.Fatalf("invalidation did not preempt snapshot: %q", got)
		}
	})
}

func TestHandlerReadinessCancellationAndShutdown(t *testing.T) {
	hub := NewHub()
	var ready <-chan struct{} = make(chan struct{})
	w := httptest.NewRecorder()
	testHandler(hub, ready).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" || hub.SubscriberCount() != 0 {
		t.Fatalf("status=%d retry=%q subscribers=%d", w.Code, w.Header().Get("Retry-After"), hub.SubscriberCount())
	}

	ready = closedSignal()
	handler := testHandler(hub, ready)
	w2 := newStreamWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	w2.waitFlush(t)
	hub.Shutdown()
	waitDone(t, done)
	if hub.SubscriberCount() != 0 || strings.Contains(w2.body(), "view_reset") {
		t.Fatalf("shutdown leaked subscription or emitted reset: %q", w2.body())
	}
}

func TestAnonymousViewStreamsOnlyAdapterSuppliedAggregate(t *testing.T) {
	hub := NewHub()
	handler := testHandler(hub, closedSignal())
	handler.UID = ""
	handler.Snapshot = func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(`{"attendee_count":3}`), nil
	}
	w := newStreamWriter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	}()
	cancel()
	waitDone(t, done)
	if got := w.body(); !strings.Contains(got, `{"attendee_count":3}`) || strings.Contains(got, "name") || strings.Contains(got, "user-") {
		t.Fatalf("anonymous output was not aggregate-only: %q", got)
	}
}

func TestHandlerResetHasNoEventIDAndEnds(t *testing.T) {
	hub := NewHub()
	handler := testHandler(hub, closedSignal())
	handler.WriteTimeout = time.Second
	w := newStreamWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	w.waitFlush(t)
	hub.InvalidateUID("user-a")
	w.waitFlush(t)
	waitDone(t, done)
	got := w.body()
	if !strings.Contains(got, "event: view_reset\ndata: {}\n\n") || strings.Contains(got, "id:") || strings.Contains(got, "user-a") {
		t.Fatalf("unexpected reset framing or detail: %q", got)
	}
	if deadlines := w.deadlineCount(); deadlines != 2 {
		t.Fatalf("write deadlines set=%d, want one per frame", deadlines)
	}
	sequence := w.deadlineSequence()
	if len(sequence) != 5 || !sequence[0].IsZero() || sequence[1].IsZero() || !sequence[2].IsZero() || sequence[3].IsZero() || !sequence[4].IsZero() {
		t.Fatalf("write deadlines were not cleared between frames: %v", sequence)
	}
}

func TestHandlerBoundsAStalledResponseWrite(t *testing.T) {
	hub := NewHub()
	handler := testHandler(hub, closedSignal())
	handler.WriteTimeout = 20 * time.Millisecond
	w := newStreamWriter()
	w.expireNextWriteAtDeadline()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	waitDone(t, done)
	if w.deadlineCount() != 1 || hub.SubscriberCount() != 0 || w.body() != "" {
		t.Fatalf("deadline count=%d subscribers=%d body=%q", w.deadlineCount(), hub.SubscriberCount(), w.body())
	}
}

func TestHandlerBoundsAStalledHeartbeat(t *testing.T) {
	hub := NewHub()
	handler := testHandler(hub, closedSignal())
	handler.HeartbeatInterval = 5 * time.Millisecond
	handler.WriteTimeout = 20 * time.Millisecond
	w := newStreamWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	w.waitFlush(t)
	w.expireNextWriteAtDeadline()
	waitDone(t, done)
	if w.deadlineCount() != 2 || hub.SubscriberCount() != 0 || strings.Contains(w.body(), "keepalive") {
		t.Fatalf("deadlines=%d subscribers=%d body=%q", w.deadlineCount(), hub.SubscriberCount(), w.body())
	}
}

func TestHandlerAllowsInflightWriteThenResetsAtNextBoundary(t *testing.T) {
	hub := NewHub()
	handler := testHandler(hub, closedSignal())
	w := newStreamWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	w.waitFlush(t)
	w.blockNextWrite()
	hub.PublishChanged("view")
	select {
	case <-w.writeEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("changed event write did not start")
	}
	hub.InvalidateUID("user-a")
	close(w.releaseWrite)
	w.waitFlush(t)
	w.waitFlush(t)
	waitDone(t, done)
	got := w.body()
	if strings.Count(got, "event: view\n") != 2 || !strings.Contains(got, "event: view_reset\ndata: {}\n\n") {
		t.Fatalf("in-flight write/reset boundary was not respected: %q", got)
	}
}

func testHandler(hub *Hub, ready <-chan struct{}) *Handler {
	return &Handler{
		Hub: hub, Ready: ready, ViewKey: "view", AuthorizationScope: "group-a", UID: "user-a", EventName: "view",
		Authorize: func(context.Context) (bool, int) { return true, 0 },
		Snapshot:  func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"ok":true}`), nil },
	}
}

type streamWriter struct {
	mu               sync.Mutex
	header           http.Header
	status           int
	bodyBuf          bytes.Buffer
	flushes          chan struct{}
	deadlines        []time.Time
	blockNext        bool
	writeEntered     chan struct{}
	releaseWrite     chan struct{}
	expireAtDeadline bool
}

func newStreamWriter() *streamWriter {
	return &streamWriter{header: make(http.Header), flushes: make(chan struct{}, 8)}
}

func (w *streamWriter) Header() http.Header { return w.header }
func (w *streamWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
	}
}
func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	block := w.blockNext
	expire := w.expireAtDeadline
	w.expireAtDeadline = false
	var deadline time.Time
	if expire && len(w.deadlines) > 0 {
		deadline = w.deadlines[len(w.deadlines)-1]
	}
	if block {
		w.blockNext = false
	}
	w.mu.Unlock()
	if expire {
		if wait := time.Until(deadline); wait > 0 {
			timer := time.NewTimer(wait)
			<-timer.C
		}
		return 0, errors.New("write deadline exceeded")
	}
	if block {
		w.writeEntered <- struct{}{}
		<-w.releaseWrite
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.bodyBuf.Write(p)
}
func (w *streamWriter) Flush() {
	select {
	case w.flushes <- struct{}{}:
	default:
	}
}
func (w *streamWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadlines = append(w.deadlines, deadline)
	return nil
}
func (w *streamWriter) waitFlush(t *testing.T) {
	t.Helper()
	select {
	case <-w.flushes:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response flush")
	}
}
func (w *streamWriter) body() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bodyBuf.String()
}
func (w *streamWriter) deadlineCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := 0
	for _, deadline := range w.deadlines {
		if !deadline.IsZero() {
			count++
		}
	}
	return count
}
func (w *streamWriter) deadlineSequence() []time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Time(nil), w.deadlines...)
}
func (w *streamWriter) statusCode() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}
func (w *streamWriter) blockNextWrite() {
	w.writeEntered = make(chan struct{}, 1)
	w.releaseWrite = make(chan struct{})
	w.blockNext = true
}
func (w *streamWriter) expireNextWriteAtDeadline() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.expireAtDeadline = true
}

func closedSignal() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not terminate")
	}
}
