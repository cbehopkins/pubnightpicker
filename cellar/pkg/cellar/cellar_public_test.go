package cellar_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestCellarUsesPersistedHandlerNameAsResolutionKey(t *testing.T) {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	received := make(chan string, 1)
	const handlerName cellar.HandlerName = "email.send.push"
	if err := runtime.Register(handlerName, routedHandler{name: "push", received: received}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	original := cellar.Cell{
		ID:          "persisted-cell",
		HandlerName: handlerName,
		Payload:     []byte(`{"source":"reconstructed"}`),
		State:       cellar.CellStateReady,
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var reconstructed cellar.Cell
	if err := json.Unmarshal(raw, &reconstructed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if err := store.AddWithIDs([]cellar.IdentifiedCellRequest{{
		ID: reconstructed.ID,
		CellRequest: cellar.CellRequest{
			HandlerName: reconstructed.HandlerName,
			Payload:     reconstructed.Payload,
		},
	}}); err != nil {
		t.Fatalf("AddWithIDs() error = %v", err)
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
	if _, err := store.Add([]cellar.CellRequest{{HandlerName: "example.missing", Payload: []byte(`{}`)}}); err != nil {
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
	if _, err := store.Add([]cellar.CellRequest{{HandlerName: "example.increment", Payload: []byte(`{"value":`)}}); err != nil {
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
	if _, err := store.Add([]cellar.CellRequest{{HandlerName: "example.failure", Payload: []byte(`{"value":1}`)}}); err != nil {
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
