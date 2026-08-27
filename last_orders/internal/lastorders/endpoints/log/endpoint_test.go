package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	logsvc "last_orders/internal/lastorders/services/log"
)

type fakeCellAdder struct {
	requests []cellar.CellRequest
	err      error
}

func (f *fakeCellAdder) AddCell(request cellar.CellRequest) error {
	if f.err != nil {
		return f.err
	}
	f.requests = append(f.requests, request)
	return nil
}

func TestEndpointAcceptsValidRequest(t *testing.T) {
	adder := &fakeCellAdder{}
	endpoint := &Endpoint{Cells: adder}

	body := `{"event_id":"evt-1","message":"hello world"}`
	req := httptest.NewRequest(http.MethodPost, "/log", strings.NewReader(body))
	rec := httptest.NewRecorder()

	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(adder.requests) != 1 {
		t.Fatalf("expected one Cell to be added, got %d", len(adder.requests))
	}

	request := adder.requests[0]
	if len(request.Steps) != 3 {
		t.Fatalf("expected the 3-step idempotency sequence, got %d steps", len(request.Steps))
	}

	var step firebaseidempotency.StepPayload
	if err := json.Unmarshal(request.Steps[0].Payload, &step); err != nil {
		t.Fatalf("unmarshal step payload: %v", err)
	}
	if step.Listener != ListenerLog {
		t.Fatalf("expected listener %q, got %q", ListenerLog, step.Listener)
	}
	if step.EventKey != "evt-1" {
		t.Fatalf("expected event key %q, got %q", "evt-1", step.EventKey)
	}
	if step.Fact.Name != logsvc.FactLogMessage {
		t.Fatalf("expected fact %q, got %q", logsvc.FactLogMessage, step.Fact.Name)
	}

	var logPayload logsvc.Payload
	if err := json.Unmarshal(step.Fact.Payload, &logPayload); err != nil {
		t.Fatalf("unmarshal fact payload: %v", err)
	}
	if logPayload.Message != "hello world" {
		t.Fatalf("expected message %q, got %q", "hello world", logPayload.Message)
	}
}

func TestEndpointRejectsMissingEventID(t *testing.T) {
	adder := &fakeCellAdder{}
	endpoint := &Endpoint{Cells: adder}

	req := httptest.NewRequest(http.MethodPost, "/log", strings.NewReader(`{"message":"hello world"}`))
	rec := httptest.NewRecorder()

	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if len(adder.requests) != 0 {
		t.Fatal("expected no Cell to be added")
	}
}

func TestEndpointRejectsMissingMessage(t *testing.T) {
	adder := &fakeCellAdder{}
	endpoint := &Endpoint{Cells: adder}

	req := httptest.NewRequest(http.MethodPost, "/log", strings.NewReader(`{"event_id":"evt-1"}`))
	rec := httptest.NewRecorder()

	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if len(adder.requests) != 0 {
		t.Fatal("expected no Cell to be added")
	}
}

func TestEndpointRejectsMalformedJSON(t *testing.T) {
	adder := &fakeCellAdder{}
	endpoint := &Endpoint{Cells: adder}

	req := httptest.NewRequest(http.MethodPost, "/log", bytes.NewReader([]byte(`{not json`)))
	rec := httptest.NewRecorder()

	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if len(adder.requests) != 0 {
		t.Fatal("expected no Cell to be added")
	}
}

func TestEndpointReturns500WhenAddCellFails(t *testing.T) {
	adder := &fakeCellAdder{err: errors.New("store unavailable")}
	endpoint := &Endpoint{Cells: adder}

	req := httptest.NewRequest(http.MethodPost, "/log", strings.NewReader(`{"event_id":"evt-1","message":"hello world"}`))
	rec := httptest.NewRecorder()

	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
