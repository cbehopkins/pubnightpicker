package admindelete

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/truths"
)

type fakeSource struct {
	documents []Document
	paused    bool
	status    string
	stream    ChangeStream
}

func (s *fakeSource) Watch(context.Context) (ChangeStream, error) {
	if s.stream != nil {
		return s.stream, nil
	}
	return nil, errors.New("unused")
}

func (s *fakeSource) WatchPause(context.Context) (PauseStream, error) {
	return nil, errors.New("unused")
}

func (s *fakeSource) ListEligible(context.Context, bool) ([]Document, error) {
	return s.documents, nil
}

func (s *fakeSource) IsPaused(context.Context) (bool, error) {
	return s.paused, nil
}

func (s *fakeSource) RequestStatus(context.Context, string) (string, error) {
	return s.status, nil
}

func TestRunOnceCreatesIdempotentTruthWithFrontendFieldConversions(t *testing.T) {
	createdAt := time.Date(2026, time.October, 9, 10, 20, 30, 0, time.FixedZone("BST", 3600))
	source := &fakeSource{documents: []Document{{
		ID: "request-1",
		Data: map[string]any{
			"schemaVersion":    int64(1),
			"targetUid":        "target-1",
			"targetEmail":      nil,
			"requestedByUid":   "admin-1",
			"requestedByEmail": "admin@example.com",
			"reason":           "admin_user_delete",
			"status":           "pending",
			"createdAt":        createdAt,
		},
	}}}
	store := cellar.NewMemoryStore(nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	listener, err := New(Config{Source: source, Store: store, Enabled: true, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}

	ids, err := listener.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(ids) != 1 || ids[0] != "request-1" {
		t.Fatalf("request IDs = %v, want [request-1]", ids)
	}
	cells, err := store.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 1 {
		t.Fatalf("active Cells = %d, want 1", len(cells))
	}
	var step firebaseidempotency.StepPayload
	if err := json.Unmarshal(cells[0].Steps[0].Payload, &step); err != nil {
		t.Fatalf("decode idempotency payload: %v", err)
	}
	if step.EventKey != "request-1" || step.Truth.FanoutName != truths.AdminDeleteRequestedFanout {
		t.Fatalf("idempotency payload = %+v", step)
	}
	var truth truths.AdminDeleteRequested
	if err := json.Unmarshal(step.Truth.Payload, &truth); err != nil {
		t.Fatalf("decode Truth payload: %v", err)
	}
	if truth.Identity() != "request-1" || truth.Request.SchemaVersion != 1 {
		t.Fatalf("Truth identity/version = %q/%d", truth.Identity(), truth.Request.SchemaVersion)
	}
	if truth.Request.TargetEmail != "" || truth.Request.RequestedByEmail != "admin@example.com" {
		t.Fatalf("email fields = %q, %q", truth.Request.TargetEmail, truth.Request.RequestedByEmail)
	}
	if !truth.Request.CreatedAt.Equal(createdAt.UTC()) || truth.Request.CreatedAt.Location() != time.UTC {
		t.Fatalf("createdAt = %s, want %s in UTC", truth.Request.CreatedAt, createdAt.UTC())
	}
}

func TestRunOnceSkipsWhenPaused(t *testing.T) {
	source := &fakeSource{paused: true, documents: []Document{{ID: "request-1"}}}
	store := cellar.NewMemoryStore(nil)
	listener, err := New(Config{Source: source, Store: store, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := listener.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("request IDs = %v, want none while paused", ids)
	}
	cells, err := store.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 0 {
		t.Fatalf("active Cells = %d, want none while paused", len(cells))
	}
}

func TestRunOnceSurfacesMalformedPendingRequest(t *testing.T) {
	source := &fakeSource{documents: []Document{{ID: "request-1", Data: map[string]any{
		"schemaVersion":    1,
		"targetUid":        "target-1",
		"targetEmail":      17,
		"requestedByUid":   "admin-1",
		"requestedByEmail": nil,
		"reason":           "admin_user_delete",
		"status":           "pending",
		"createdAt":        time.Now(),
	}}}}
	listener, err := New(Config{Source: source, Store: cellar.NewMemoryStore(nil), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listener.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce accepted a non-string email")
	}
}

func TestDecodeTruthRejectsUnsupportedFirestoreValues(t *testing.T) {
	document := Document{ID: "request-1", Data: map[string]any{
		"schemaVersion":    float64(1),
		"targetUid":        "target-1",
		"targetEmail":      "",
		"requestedByUid":   "admin-1",
		"requestedByEmail": "",
		"reason":           "admin_user_delete",
		"createdAt":        time.Now(),
	}}
	if _, err := decodeTruth(document); err == nil {
		t.Fatal("decodeTruth accepted a floating-point schema version")
	}

}

func TestValidatedDryRunEligibilityAndPhaseIdentity(t *testing.T) {
	document := Document{ID: "request-1", Data: map[string]any{
		"schemaVersion": int64(1), "targetUid": "target-1", "targetEmail": nil,
		"requestedByUid": "admin-1", "requestedByEmail": nil, "reason": "admin_user_delete",
		"status": "dry_run_validated", "createdAt": time.Now(),
	}}
	for _, realDelete := range []bool{false, true} {
		store := cellar.NewMemoryStore(nil)
		listener, err := New(Config{Source: &fakeSource{documents: []Document{document}, status: "dry_run_validated"},
			Store: store, Enabled: true, RealDelete: realDelete})
		if err != nil {
			t.Fatal(err)
		}
		ids, err := listener.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if realDelete {
			want = 1
		}
		if len(ids) != want {
			t.Fatalf("realDelete=%t: selected %v", realDelete, ids)
		}
		if realDelete {
			cells, err := store.ListActive()
			if err != nil {
				t.Fatal(err)
			}
			var step firebaseidempotency.StepPayload
			if err := json.Unmarshal(cells[0].Steps[0].Payload, &step); err != nil {
				t.Fatal(err)
			}
			if step.EventKey != document.ID {
				t.Fatalf("event key = %q", step.EventKey)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err := listener.WaitForTerminal(ctx, ids); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("real run accepted dry-run as terminal: %v", err)
			}
		}
		source := listener.source.(*fakeSource)
		source.stream = &singleChangeStream{documents: []Document{document}}
		if err := listener.watchOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		cells, err := store.ListActive()
		if err != nil {
			t.Fatal(err)
		}
		wantCells := 0
		if realDelete {
			wantCells = 2
		}
		if len(cells) != wantCells {
			t.Fatalf("realDelete=%t: watched active cells = %d, want %d", realDelete, len(cells), wantCells)
		}
	}
}

type singleChangeStream struct {
	documents []Document
}

func (s *singleChangeStream) Next() ([]Document, error) {
	if s.documents == nil {
		return nil, context.Canceled
	}
	documents := s.documents
	s.documents = nil
	return documents, nil
}

func (*singleChangeStream) Stop() {}
