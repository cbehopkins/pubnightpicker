package cellar

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fanoutTestPayload struct {
	OrderID string `json:"order_id"`
}

func TestFanoutRegistrationExpandsTypedPayloadToIdentifiedChildren(t *testing.T) {
	due := time.Now().Add(time.Hour)
	fanout, err := NewFanout("order.completed", FanoutExpanderFunc[fanoutTestPayload](
		func(ctx context.Context, parentID CellID, payload fanoutTestPayload) ([]FanoutTarget, error) {
			if parentID != "parent-1" {
				t.Fatalf("parent ID = %q, want %q", parentID, "parent-1")
			}
			if payload.OrderID != "order-42" {
				t.Fatalf("payload = %#v, want order-42", payload)
			}
			return []FanoutTarget{
				{Key: "email", HandlerName: "email.send", Payload: fanoutTestPayload{OrderID: "order-42"}},
				{Key: "analytics", HandlerName: "analytics.publish", Payload: fanoutTestPayload{OrderID: "order-42"}, NotBefore: &due},
			}, nil
		},
	))
	if err != nil {
		t.Fatalf("NewFanout() error = %v", err)
	}

	runtime := New(NewMemoryStore(nil), Config{})
	if err := fanout.Register(runtime); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registration, ok := runtime.registry.Lookup("order.completed")
	if !ok {
		t.Fatal("fanout registration not found")
	}

	raw, err := marshalJSON(fanoutTestPayload{OrderID: "order-42"})
	if err != nil {
		t.Fatalf("marshalJSON() error = %v", err)
	}
	cell := Cell{ID: "parent-1", Steps: []CellStep{{HandlerName: "order.completed", Payload: raw}}, State: CellStateClaimed}
	result := registration.Execute(t.Context(), cell)
	complete, ok := result.(Complete)
	if !ok {
		t.Fatalf("result = %T, want Complete", result)
	}
	if len(complete.NewCells) != 2 {
		t.Fatalf("children = %d, want 2", len(complete.NewCells))
	}
	if complete.NewCells[0].ID == complete.NewCells[1].ID {
		t.Fatal("different target keys produced the same child ID")
	}
	if complete.NewCells[0].Steps[0].HandlerName != "email.send" {
		t.Fatalf("first child handler = %q, want %q", complete.NewCells[0].Steps[0].HandlerName, "email.send")
	}
	if string(complete.NewCells[0].Steps[0].Payload) != `{"order_id":"order-42"}` {
		t.Fatalf("first child payload = %s, want encoded JSON", complete.NewCells[0].Steps[0].Payload)
	}
	if complete.NewCells[1].NotBefore == nil || !complete.NewCells[1].NotBefore.Equal(due) {
		t.Fatalf("second child NotBefore = %v, want %v", complete.NewCells[1].NotBefore, due)
	}

	repeated := registration.Execute(t.Context(), cell).(Complete)
	for index := range complete.NewCells {
		if repeated.NewCells[index].ID != complete.NewCells[index].ID {
			t.Fatalf("repeated child %d ID = %q, want %q", index, repeated.NewCells[index].ID, complete.NewCells[index].ID)
		}
	}
}

func TestFanoutRegistrationRejectsInvalidTargetKeys(t *testing.T) {
	tests := []struct {
		name    string
		targets []FanoutTarget
		want    error
	}{
		{name: "empty", targets: []FanoutTarget{{HandlerName: "child"}}, want: ErrFanoutTargetKeyRequired},
		{name: "duplicate", targets: []FanoutTarget{
			{Key: "same", HandlerName: "child-a"},
			{Key: "same", HandlerName: "child-b"},
		}, want: ErrFanoutTargetKeyDuplicate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fanout, err := NewFanout("fanout", FanoutExpanderFunc[fanoutTestPayload](
				func(context.Context, CellID, fanoutTestPayload) ([]FanoutTarget, error) {
					return test.targets, nil
				},
			))
			if err != nil {
				t.Fatalf("NewFanout() error = %v", err)
			}
			runtime := New(NewMemoryStore(nil), Config{})
			if err := fanout.Register(runtime); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			registration, _ := runtime.registry.Lookup("fanout")
			result := registration.Execute(t.Context(), Cell{ID: "parent", Steps: []CellStep{{HandlerName: "fanout", Payload: []byte(`{"order_id":"one"}`)}}})
			failure, ok := result.(ErrorResult)
			if !ok {
				t.Fatalf("result = %T, want ErrorResult", result)
			}
			if !errors.Is(failure.Err, test.want) {
				t.Fatalf("error = %v, want %v", failure.Err, test.want)
			}
		})
	}
}

func TestFanoutExpansionErrorReturnsErrorResult(t *testing.T) {
	want := errors.New("cannot expand")
	fanout, err := NewFanout("fanout", FanoutExpanderFunc[fanoutTestPayload](
		func(context.Context, CellID, fanoutTestPayload) ([]FanoutTarget, error) {
			return nil, want
		},
	))
	if err != nil {
		t.Fatalf("NewFanout() error = %v", err)
	}
	runtime := New(NewMemoryStore(nil), Config{})
	if err := fanout.Register(runtime); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registration, _ := runtime.registry.Lookup("fanout")
	result := registration.Execute(t.Context(), Cell{ID: "parent", Steps: []CellStep{{HandlerName: "fanout", Payload: []byte(`{"order_id":"one"}`)}}})
	failure, ok := result.(ErrorResult)
	if !ok {
		t.Fatalf("result = %T, want ErrorResult", result)
	}
	if !errors.Is(failure.Err, want) {
		t.Fatalf("error = %v, want %v", failure.Err, want)
	}
}

func TestFanoutTargetPayloadEncodingErrorReturnsErrorResult(t *testing.T) {
	fanout, err := NewFanout("fanout", FanoutExpanderFunc[fanoutTestPayload](
		func(context.Context, CellID, fanoutTestPayload) ([]FanoutTarget, error) {
			return []FanoutTarget{{Key: "invalid", HandlerName: "child", Payload: make(chan int)}}, nil
		},
	))
	if err != nil {
		t.Fatalf("NewFanout() error = %v", err)
	}
	runtime := New(NewMemoryStore(nil), Config{})
	if err := fanout.Register(runtime); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registration, _ := runtime.registry.Lookup("fanout")
	result := registration.Execute(t.Context(), Cell{ID: "parent", Steps: []CellStep{{HandlerName: "fanout", Payload: []byte(`{"order_id":"one"}`)}}})
	failure, ok := result.(ErrorResult)
	if !ok {
		t.Fatalf("result = %T, want ErrorResult", result)
	}
	if failure.Err == nil {
		t.Fatal("error = nil, want payload encoding error")
	}
}

func TestFanoutValidatesConstruction(t *testing.T) {
	valid := FanoutExpanderFunc[fanoutTestPayload](func(context.Context, CellID, fanoutTestPayload) ([]FanoutTarget, error) {
		return nil, nil
	})

	if _, err := NewFanout("", valid); !errors.Is(err, ErrHandlerNameRequired) {
		t.Fatalf("unnamed NewFanout() error = %v, want ErrHandlerNameRequired", err)
	}
	if _, err := NewFanout[fanoutTestPayload]("fanout", nil); !errors.Is(err, ErrFanoutExpanderNil) {
		t.Fatalf("nil NewFanout() error = %v, want ErrFanoutExpanderNil", err)
	}
}

func TestFanoutMaterialisesAndExecutesOrdinaryChildren(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("cell-", 1))
	runtime := New(store, Config{PollDelay: time.Millisecond})
	received := make(chan fanoutTestPayload, 2)
	child := fanoutRecordingHandler{received: received}
	if err := runtime.Register("email.send", child); err != nil {
		t.Fatalf("Register(email.send) error = %v", err)
	}
	if err := runtime.Register("analytics.publish", child); err != nil {
		t.Fatalf("Register(analytics.publish) error = %v", err)
	}

	fanout, err := NewFanout("order.completed", FanoutExpanderFunc[fanoutTestPayload](
		func(context.Context, CellID, fanoutTestPayload) ([]FanoutTarget, error) {
			return []FanoutTarget{
				{Key: "email", HandlerName: "email.send", Payload: fanoutTestPayload{OrderID: "order-42"}},
				{Key: "analytics", HandlerName: "analytics.publish", Payload: fanoutTestPayload{OrderID: "order-42"}},
			}, nil
		},
	))
	if err != nil {
		t.Fatalf("NewFanout() error = %v", err)
	}
	if err := fanout.Register(runtime); err != nil {
		t.Fatalf("Register(fanout) error = %v", err)
	}
	if _, err := fanout.Add(runtime, fanoutTestPayload{OrderID: "order-42"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	startDone := make(chan error, 1)
	go func() { startDone <- runtime.Start(context.Background()) }()
	for range 2 {
		select {
		case payload := <-received:
			if payload.OrderID != "order-42" {
				t.Fatalf("child payload = %#v, want order-42", payload)
			}
		case <-time.After(time.Second):
			t.Fatal("fanout child was not executed")
		}
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

type fanoutRecordingHandler struct {
	received chan<- fanoutTestPayload
}

func (h fanoutRecordingHandler) Handle(ctx context.Context, payload fanoutTestPayload) Result {
	h.received <- payload
	return Complete{}
}
