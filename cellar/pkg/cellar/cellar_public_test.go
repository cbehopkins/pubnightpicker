package cellar_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cellar/pkg/cellar"
)

type incrementPayload struct {
	Value int `json:"value"`
}

type routedPayload struct {
	Source string `json:"source"`
}

type routedHandler struct {
	name     string
	received chan<- string
}

func (h routedHandler) Handle(ctx context.Context, payload routedPayload) cellar.Result {
	h.received <- h.name + ":" + payload.Source
	return cellar.Complete{}
}

type structuredPayload struct {
	ID      int      `json:"id"`
	Message string   `json:"message"`
	Values  []string `json:"values"`
}

type structuredHandler struct {
	received chan<- structuredPayload
}

func (h structuredHandler) Handle(ctx context.Context, payload structuredPayload) cellar.Result {
	h.received <- payload
	return cellar.Complete{}
}

type failingHandler struct {
	err error
}

func (h failingHandler) Handle(ctx context.Context, payload incrementPayload) cellar.Result {
	return cellar.ErrorResult{Message: "increment failed", Err: h.err}
}

type incrementHandler struct {
	received chan<- incrementPayload
}

func (h incrementHandler) Handle(ctx context.Context, payload incrementPayload) cellar.Result {
	h.received <- payload
	return cellar.Complete{}
}

type blockingContinuationHandler struct {
	started chan<- struct{}
	release <-chan struct{}
	child   cellar.CellRequest
}

type schedulingTestClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *schedulingTestClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *schedulingTestClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type claimObservingStore struct {
	cellar.Store
	claimTimes chan time.Time
}

type sequenceRetryHandler struct {
	attempts atomic.Int32
	calls    chan int32
}

func (h *sequenceRetryHandler) Handle(context.Context, incrementPayload) cellar.Result {
	attempt := h.attempts.Add(1)
	h.calls <- attempt
	if attempt == 1 {
		return cellar.RetrySequence{Delay: 5 * time.Minute}
	}
	return cellar.Complete{}
}

func (s *claimObservingStore) ClaimNext(now time.Time) (cellar.Cell, bool, error) {
	s.claimTimes <- now
	return s.Store.ClaimNext(now)
}

func (h blockingContinuationHandler) Handle(ctx context.Context, payload string) cellar.Result {
	h.started <- struct{}{}
	<-h.release
	return cellar.Complete{NewCells: []cellar.CellRequest{h.child}}
}

func TestCellarRegistersAndExecutesTypedJSONHandler(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	received := make(chan incrementPayload, 1)

	if err := runtime.Register("example.increment", incrementHandler{received: received}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if _, err := runtime.Add("example.increment", incrementPayload{Value: 41}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- runtime.Start(context.Background())
	}()

	select {
	case got := <-received:
		if got.Value != 41 {
			t.Fatalf("handler payload = %#v, want value 41", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler was not invoked")
	}

	if err := runtime.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active cells = %d, want 0", len(active))
	}
}

func TestCellarUsesInjectedClockAndNotifiesScheduler(t *testing.T) {
	initial := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	clock := &schedulingTestClock{now: initial}
	baseStore := cellar.NewMemoryStore(nil)
	store := &claimObservingStore{Store: baseStore, claimTimes: make(chan time.Time, 4)}
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Hour, Clock: clock})
	received := make(chan string, 1)
	if err := runtime.Register("example.scheduled", routedHandler{name: "scheduled", received: received}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	due := initial.Add(time.Minute)
	if _, err := store.Add([]cellar.CellRequest{{
		Steps:     []cellar.CellStep{{HandlerName: "example.scheduled", Payload: []byte(`{"source":"ready"}`)}},
		NotBefore: &due,
	}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	startDone := startRuntime(runtime)
	select {
	case got := <-store.claimTimes:
		if !got.Equal(initial) {
			t.Fatalf("initial claim time = %s, want frozen time %s", got, initial)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not make its initial claim")
	}
	runtime.NotifyScheduling()
	select {
	case got := <-store.claimTimes:
		if !got.Equal(initial) {
			t.Fatalf("notification claim time = %s, want frozen time %s", got, initial)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not recheck after notification")
	}
	select {
	case got := <-received:
		t.Fatalf("handler ran before application time advanced: %q", got)
	case <-time.After(20 * time.Millisecond):
	}
	if got := clock.Now(); !got.Equal(initial) {
		t.Fatalf("frozen clock advanced with real time: got %s, want %s", got, initial)
	}

	advanced := due
	clock.Set(advanced)
	runtime.NotifyScheduling()
	select {
	case got := <-received:
		if got != "scheduled:ready" {
			t.Fatalf("handler result = %q, want scheduled:ready", got)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not dispatch work after application time advanced")
	}
	if got := clock.Now(); !got.Equal(advanced) {
		t.Fatalf("application clock changed after advancement: got %s, want %s", got, advanced)
	}
	stopRuntime(t, runtime, startDone)
}

func TestSequenceRetryStaysDeferredUntilApplicationTimeAdvances(t *testing.T) {
	initial := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	clock := &schedulingTestClock{now: initial}
	baseStore := cellar.NewMemoryStore(nil)
	store := &claimObservingStore{Store: baseStore, claimTimes: make(chan time.Time, 8)}
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Hour, Clock: clock})
	handler := &sequenceRetryHandler{calls: make(chan int32, 2)}
	if err := runtime.Register("example.retry", handler); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := runtime.Add("example.retry", incrementPayload{Value: 1}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	startDone := startRuntime(runtime)
	select {
	case <-store.claimTimes:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not make its initial claim")
	}
	select {
	case attempt := <-handler.calls:
		if attempt != 1 {
			t.Fatalf("first handler attempt = %d, want 1", attempt)
		}
	case <-time.After(time.Second):
		t.Fatal("first handler attempt did not occur")
	}
	select {
	case <-store.claimTimes:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not check eligibility after sequence retry")
	}
	select {
	case attempt := <-handler.calls:
		t.Fatalf("handler retried while application time was frozen: attempt %d", attempt)
	case <-time.After(20 * time.Millisecond):
	}
	if got := clock.Now(); !got.Equal(initial) {
		t.Fatalf("frozen clock advanced with real time: got %s, want %s", got, initial)
	}

	advanced := initial.Add(5 * time.Minute)
	clock.Set(advanced)
	runtime.NotifyScheduling()
	select {
	case attempt := <-handler.calls:
		if attempt != 2 {
			t.Fatalf("handler attempt after advancement = %d, want 2", attempt)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not retry after application time advanced")
	}
	if got := clock.Now(); !got.Equal(advanced) {
		t.Fatalf("application clock changed after advancement: got %s, want %s", got, advanced)
	}
	stopRuntime(t, runtime, startDone)
}

func TestStoreResultApplierUsesInjectedClockForSequenceRetry(t *testing.T) {
	initial := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	clock := &schedulingTestClock{now: initial}
	store := cellar.NewMemoryStore(nil)
	ids, err := store.Add([]cellar.CellRequest{{
		Steps: []cellar.CellStep{
			{HandlerName: "example.first", Payload: []byte(`{}`)},
			{HandlerName: "example.second", Payload: []byte(`{}`)},
		},
	}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	claimed, ok, err := store.ClaimNext(initial)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if !ok {
		t.Fatal("ClaimNext() did not return the sequence")
	}

	const delay = 5 * time.Minute
	applier := cellar.NewStoreResultApplierWithClock(store, clock)
	if err := applier.ApplyResult(context.Background(), claimed, cellar.RetrySequence{Delay: delay}); err != nil {
		t.Fatalf("ApplyResult() error = %v", err)
	}
	cell, err := store.Get(ids[0])
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	wantDue := initial.Add(delay)
	if cell.NotBefore == nil || !cell.NotBefore.Equal(wantDue) {
		t.Fatalf("sequence retry deadline = %v, want %s", cell.NotBefore, wantDue)
	}
	if cell.CurrentStep != 0 {
		t.Fatalf("sequence current step = %d, want 0", cell.CurrentStep)
	}
	if _, ok, err := store.ClaimNext(initial.Add(delay - time.Second)); err != nil || ok {
		t.Fatalf("ClaimNext() before retry deadline = (%t, %v), want no claim", ok, err)
	}
	if _, ok, err := store.ClaimNext(wantDue); err != nil || !ok {
		t.Fatalf("ClaimNext() at retry deadline = (%t, %v), want a claim", ok, err)
	}
}

func TestCalendarTimerCoalescesToLatestDueOccurrence(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	tests := []struct {
		name             string
		initial          time.Time
		advanced         time.Time
		wantOccurrence   time.Time
		wantNextDeadline time.Time
	}{
		{
			name:             "multi-day jump across British Summer Time start",
			initial:          time.Date(2026, time.March, 27, 15, 59, 0, 0, location),
			advanced:         time.Date(2026, time.March, 30, 15, 59, 0, 0, location),
			wantOccurrence:   time.Date(2026, time.March, 29, 16, 0, 0, 0, location),
			wantNextDeadline: time.Date(2026, time.March, 30, 16, 0, 0, 0, location),
		},
		{
			name:             "multi-day jump across Greenwich Mean Time start",
			initial:          time.Date(2026, time.October, 23, 15, 59, 0, 0, location),
			advanced:         time.Date(2026, time.October, 26, 15, 59, 0, 0, location),
			wantOccurrence:   time.Date(2026, time.October, 25, 16, 0, 0, 0, location),
			wantNextDeadline: time.Date(2026, time.October, 26, 16, 0, 0, 0, location),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &schedulingTestClock{now: test.initial}
			baseStore := cellar.NewMemoryStore(nil)
			store := &claimObservingStore{Store: baseStore, claimTimes: make(chan time.Time, 4)}
			runtime := cellar.New(store, cellar.Config{PollDelay: time.Hour, Clock: clock})
			occurrences := make(chan time.Time, 1)
			timer, err := cellar.NewCalendarTimer("reports.daily", cellar.TimerConfig{
				Mode:     cellar.TimerDailyCalendar,
				DailyAt:  "16:00",
				Location: location.String(),
			}, func(_ context.Context, occurrence time.Time) error {
				occurrences <- occurrence
				return nil
			})
			if err != nil {
				t.Fatalf("NewCalendarTimer() error = %v", err)
			}
			if err := timer.Register(runtime); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			id, err := timer.Schedule(runtime)
			if err != nil {
				t.Fatalf("Schedule() error = %v", err)
			}
			initialCell, err := baseStore.Get(id)
			if err != nil {
				t.Fatalf("Get() scheduled cell error = %v", err)
			}
			wantInitialDeadline := time.Date(test.initial.Year(), test.initial.Month(), test.initial.Day(), 16, 0, 0, 0, location)
			if initialCell.NotBefore == nil || !initialCell.NotBefore.Equal(wantInitialDeadline) {
				t.Fatalf("initial timer deadline = %v, want %s", initialCell.NotBefore, wantInitialDeadline)
			}

			startDone := startRuntime(runtime)
			select {
			case <-store.claimTimes:
			case <-time.After(time.Second):
				t.Fatal("scheduler did not make its initial claim")
			}
			clock.Set(test.advanced)
			runtime.NotifyScheduling()
			select {
			case got := <-occurrences:
				if !got.Equal(test.wantOccurrence) {
					t.Fatalf("scheduled occurrence = %s, want %s", got, test.wantOccurrence)
				}
			case <-time.After(time.Second):
				t.Fatal("calendar callback did not run after time advanced")
			}
			for range 2 {
				select {
				case <-store.claimTimes:
				case <-time.After(time.Second):
					t.Fatal("scheduler did not check eligibility after timer rescheduling")
				}
			}
			rescheduled, err := baseStore.Get(id)
			if err != nil {
				t.Fatalf("Get() rescheduled cell error = %v", err)
			}
			if rescheduled.NotBefore == nil || !rescheduled.NotBefore.Equal(test.wantNextDeadline) || !rescheduled.NotBefore.After(test.advanced) {
				t.Fatalf("next timer deadline = %v, want %s after %s", rescheduled.NotBefore, test.wantNextDeadline, test.advanced)
			}
			stopRuntime(t, runtime, startDone)
		})
	}
}

func TestCellarResolvesMultipleHandlersByName(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	received := make(chan string, 3)

	handlers := []struct {
		name cellar.HandlerName
		want string
	}{
		{name: "example.increment", want: "increment:one"},
		{name: "example.fanout", want: "fanout:two"},
		{name: "poll.new", want: "new-poll:three"},
	}
	for _, item := range handlers {
		if err := runtime.Register(item.name, routedHandler{name: strings.Split(item.want, ":")[0], received: received}); err != nil {
			t.Fatalf("Register(%q) error = %v", item.name, err)
		}
	}

	for index, item := range handlers {
		if _, err := runtime.Add(item.name, routedPayload{Source: []string{"one", "two", "three"}[index]}); err != nil {
			t.Fatalf("Add(%q) error = %v", item.name, err)
		}
	}

	startDone := startRuntime(runtime)
	for _, item := range handlers {
		select {
		case got := <-received:
			if got != item.want {
				t.Fatalf("handler invocation = %q, want %q", got, item.want)
			}
		case <-time.After(time.Second):
			t.Fatalf("handler %q was not invoked", item.name)
		}
	}
	stopRuntime(t, runtime, startDone)
}

func TestCellarAddSequencePersistsOrderedHeterogeneousSteps(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{})

	id, err := runtime.AddSequence(
		cellar.Step{HandlerName: "example.first", Payload: incrementPayload{Value: 1}},
		cellar.Step{HandlerName: "example.second", Payload: routedPayload{Source: "two"}},
	)
	if err != nil {
		t.Fatalf("AddSequence() error = %v", err)
	}

	cell, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(cell.Steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(cell.Steps))
	}
	if cell.CurrentStep != 0 {
		t.Fatalf("CurrentStep = %d, want 0", cell.CurrentStep)
	}
	if cell.Steps[0].HandlerName != "example.first" || string(cell.Steps[0].Payload) != `{"value":1}` {
		t.Fatalf("first step = %#v", cell.Steps[0])
	}
	if cell.Steps[1].HandlerName != "example.second" || string(cell.Steps[1].Payload) != `{"source":"two"}` {
		t.Fatalf("second step = %#v", cell.Steps[1])
	}
}

func TestCellarAddSequenceRejectsEmptySteps(t *testing.T) {
	runtime := cellar.New(cellar.NewMemoryStore(nil), cellar.Config{})

	if _, err := runtime.AddSequence(); err == nil {
		t.Fatal("AddSequence() error = nil, want empty sequence error")
	}
}

func TestCellarExecutesSequenceStepsInOrder(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	received := make(chan string, 2)
	for _, name := range []cellar.HandlerName{"example.first", "example.second"} {
		if err := runtime.Register(name, routedHandler{name: string(name), received: received}); err != nil {
			t.Fatalf("Register(%q) error = %v", name, err)
		}
	}
	if _, err := runtime.AddSequence(
		cellar.Step{HandlerName: "example.first", Payload: routedPayload{Source: "one"}},
		cellar.Step{HandlerName: "example.second", Payload: routedPayload{Source: "two"}},
	); err != nil {
		t.Fatalf("AddSequence() error = %v", err)
	}

	startDone := startRuntime(runtime)
	for index, want := range []string{"example.first:one", "example.second:two"} {
		select {
		case got := <-received:
			if got != want {
				t.Fatalf("handler invocation %d = %q, want %q", index, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("handler invocation %d did not occur", index)
		}
	}
	stopRuntime(t, runtime, startDone)
}

func TestCellarDrainWaitsForActiveContinuationAndRunsAllReadyWork(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond, Workers: 2})
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	childReceived := make(chan string, 2)
	blockedReceived := make(chan string, 1)
	child, err := cellar.NewCellDefinition("example.child", routedPayload{Source: "continuation"})
	if err != nil {
		t.Fatal(err)
	}
	childRequest, err := child.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register("example.blocking", blockingContinuationHandler{started: started, release: release, child: childRequest}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register("example.child", routedHandler{name: "child", received: childReceived}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register("example.blocked", routedHandler{name: "blocked", received: blockedReceived}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := runtime.Add("example.blocking", "work"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.Add("example.blocked", routedPayload{Source: "preserved"}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if _, err := store.Add([]cellar.CellRequest{{
		Steps:     []cellar.CellStep{{HandlerName: "example.child", Payload: []byte(`{"source":"future"}`)}},
		NotBefore: &future,
	}}); err != nil {
		t.Fatal(err)
	}

	startDone := startRuntime(runtime)
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("both concurrent workers did not start")
		}
	}
	if err := runtime.BeginDrain(); err != nil {
		t.Fatal(err)
	}

	drainDone := make(chan error, 1)
	go func() { drainDone <- runtime.WaitForDrain(context.Background()) }()
	select {
	case err := <-drainDone:
		t.Fatalf("drain completed with active work: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("WaitForDrain() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not complete after active workers and their continuations")
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for range 2 {
		select {
		case got := <-childReceived:
			if got != "child:continuation" {
				t.Fatalf("continuation = %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("continuation was not executed")
		}
	}
	select {
	case got := <-blockedReceived:
		if got != "blocked:preserved" {
			t.Fatalf("ready work = %q; want blocked:preserved", got)
		}
	case <-time.After(time.Second):
		t.Fatal("ready work did not execute during drain")
	}
	active, err := store.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("future work was not preserved: %+v", active)
	}
	futureCell := 0
	for _, cell := range active {
		if cell.State != cellar.CellStateReady {
			t.Fatalf("unfinished work state = %s, want READY", cell.State)
		}
		if cell.NotBefore != nil {
			futureCell++
		}
	}
	if futureCell != 1 {
		t.Fatalf("future work count = %d; want one", futureCell)
	}
}

func TestCellarUsesPersistedHandlerNameAsResolutionKey(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	received := make(chan string, 1)
	const handlerName cellar.HandlerName = "email.send.push"
	if err := runtime.Register(handlerName, routedHandler{name: "push", received: received}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	original := cellar.Cell{
		ID:    "persisted-cell",
		Steps: []cellar.CellStep{{HandlerName: handlerName, Payload: []byte(`{"source":"reconstructed"}`)}},
		State: cellar.CellStateReady,
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var reconstructed cellar.Cell
	if err := json.Unmarshal(raw, &reconstructed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, err := store.Add([]cellar.CellRequest{{
		Steps: reconstructed.Steps,
	}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	startDone := startRuntime(runtime)
	select {
	case got := <-received:
		if got != "push:reconstructed" {
			t.Fatalf("handler invocation = %q, want %q", got, "push:reconstructed")
		}
	case <-time.After(time.Second):
		t.Fatal("reconstructed cell was not executed")
	}
	stopRuntime(t, runtime, startDone)
}

func TestCellarRoundTripsStructuredJSONPayload(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	received := make(chan structuredPayload, 1)
	if err := runtime.Register("example.structured", structuredHandler{received: received}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	want := structuredPayload{ID: 17, Message: "hello cellar", Values: []string{"one", "two"}}
	if _, err := runtime.Add("example.structured", want); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	startDone := startRuntime(runtime)
	select {
	case got := <-received:
		if got.ID != want.ID || got.Message != want.Message || strings.Join(got.Values, ",") != strings.Join(want.Values, ",") {
			t.Fatalf("handler payload = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("structured handler was not invoked")
	}
	stopRuntime(t, runtime, startDone)
}

func TestCellarClosesRegistrationWhenStarted(t *testing.T) {
	store := &signallingStore{MemoryStore: cellar.NewMemoryStore(nil), claimed: make(chan struct{}, 1)}
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register("example.increment", incrementHandler{received: make(chan incrementPayload, 1)}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	startDone := startRuntime(runtime)
	select {
	case <-store.claimed:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start scheduling")
	}

	err := runtime.Register("example.late", incrementHandler{received: make(chan incrementPayload, 1)})
	if !errors.Is(err, cellar.ErrCellarStarted) {
		t.Fatalf("late Register() error = %v, want ErrCellarStarted", err)
	}
	stopRuntime(t, runtime, startDone)
}

func TestCellarRejectsInvalidRegistrations(t *testing.T) {
	runtime := cellar.New(cellar.NewMemoryStore(nil), cellar.Config{})
	handler := incrementHandler{received: make(chan incrementPayload, 1)}

	if err := runtime.Register("example.increment", handler); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := runtime.Register("example.increment", handler); !errors.Is(err, cellar.ErrHandlerAlreadyRegistered) {
		t.Fatalf("duplicate Register() error = %v, want ErrHandlerAlreadyRegistered", err)
	}
	if err := runtime.Register("", handler); !errors.Is(err, cellar.ErrHandlerNameRequired) {
		t.Fatalf("unnamed Register() error = %v, want ErrHandlerNameRequired", err)
	}
	if err := runtime.Register[incrementPayload]("example.nil", nil); !errors.Is(err, cellar.ErrHandlerNil) {
		t.Fatalf("nil Register() error = %v, want ErrHandlerNil", err)
	}
}

func TestCellarRejectsUnknownPersistedHandlerAtStart(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	if _, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "example.missing", Payload: []byte(`{}`)}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	err := cellar.New(store, cellar.Config{}).Start(context.Background())
	if !errors.Is(err, cellar.ErrHandlerNotRegistered) {
		t.Fatalf("Start() error = %v, want ErrHandlerNotRegistered", err)
	}
}

func TestCellarReturnsMalformedJSONExecutionError(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register("example.increment", incrementHandler{received: make(chan incrementPayload, 1)}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "example.increment", Payload: []byte(`{"value":`)}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	err := runtime.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode handler payload") {
		t.Fatalf("Start() error = %v, want payload decode error", err)
	}
}

func TestCellarReturnsHandlerExecutionError(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	want := errors.New("business operation failed")
	if err := runtime.Register("example.failure", failingHandler{err: want}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "example.failure", Payload: []byte(`{"value":1}`)}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	err := runtime.Start(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want wrapped handler error", err)
	}
}

type signallingStore struct {
	*cellar.MemoryStore
	claimed chan struct{}
}

func (s *signallingStore) ClaimNext(now time.Time) (cellar.Cell, bool, error) {
	select {
	case s.claimed <- struct{}{}:
	default:
	}
	return s.MemoryStore.ClaimNext(now)
}

func startRuntime(runtime *cellar.Cellar) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- runtime.Start(context.Background())
	}()
	return done
}

func stopRuntime(t *testing.T, runtime *cellar.Cellar, startDone <-chan error) {
	t.Helper()
	if err := runtime.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}
