package logs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sweego_client/sweego"
)

func recoveryRecord(submittedAt time.Time, recipient, uid string) Record {
	return Record{
		Channel:       "email",
		EmailFrom:     "sender@example.com",
		EmailTo:       recipient,
		EmailCreation: submittedAt.Format(time.RFC3339Nano),
		TransactionID: "transaction-1",
		Headers:       map[string]any{"x-pubnight-message-id": "pn-1"},
		SwgUID:        uid,
	}
}

func TestRecoverClassifiesRecipientsAndStopsQueryingResolvedOnes(t *testing.T) {
	submittedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Channel != "email" || request.StartDate != "2026-08-31" || request.EndDate != "2026-09-02" || request.Size != 500 {
			t.Errorf("unexpected request: %#v", request)
		}
		mu.Lock()
		calls[request.SearchWord]++
		mu.Unlock()

		var records []Record
		switch request.SearchWord {
		case "alice@example.com":
			records = []Record{recoveryRecord(submittedAt, request.SearchWord, "uid-alice")}
		case "bob@example.com":
			record := recoveryRecord(submittedAt, request.SearchWord, "uid-bob")
			records = []Record{record, record}
		}
		_ = json.NewEncoder(w).Encode(Response{Result: records})
	}))
	t.Cleanup(server.Close)

	operation := RecoveryOperation{
		TransactionID: "transaction-1",
		SubmittedAt:   submittedAt,
		Sender:        "sender@example.com",
		Recipients:    []string{"alice@example.com", "bob@example.com", "carol@example.com"},
	}
	results, observations, err := Recover(context.Background(), NewClient(sweego.NewClient(server.URL, "token", time.Second)), operation, "pn-1", RecoveryOptions{Tolerance: time.Minute, Attempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Status != Recovered || results[0].SwgUID != "uid-alice" || results[0].Record == nil {
		t.Fatalf("unexpected recovered result: %#v", results)
	}
	if results[1].Status != Ambiguous || len(results[1].Candidates) != 2 || !strings.Contains(results[1].Reason, "multiple") {
		t.Fatalf("unexpected ambiguous result: %#v", results[1])
	}
	if results[2].Status != Unresolved || len(results[2].Candidates) != 0 || !strings.Contains(results[2].Reason, "no matching") {
		t.Fatalf("unexpected unresolved result: %#v", results[2])
	}
	if calls["alice@example.com"] != 1 || calls["bob@example.com"] != 1 || calls["carol@example.com"] != 3 || len(observations) != 5 {
		t.Fatalf("unexpected calls %v or observations %d", calls, len(observations))
	}
	if observations[0].Attempt != 1 || observations[len(observations)-1].Attempt != 3 || observations[len(observations)-1].Recipient != "carol@example.com" {
		t.Fatalf("unexpected observation metadata: %#v", observations)
	}
}

func TestRecoverClearsTransientErrorAfterSuccessfulRetry(t *testing.T) {
	submittedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(Response{Result: []Record{recoveryRecord(submittedAt, "alice@example.com", "uid-1")}})
	}))
	t.Cleanup(server.Close)

	results, observations, err := Recover(context.Background(), NewClient(sweego.NewClient(server.URL, "token", time.Second)), RecoveryOperation{
		TransactionID: "transaction-1", SubmittedAt: submittedAt, Sender: "sender@example.com", Recipients: []string{"alice@example.com"},
	}, "pn-1", RecoveryOptions{Tolerance: time.Minute, Attempts: 2})

	if err != nil {
		t.Fatalf("successful retry retained a transient error: %v", err)
	}
	if len(results) != 1 || results[0].Status != Recovered || len(observations) != 2 || observations[0].Err == nil || observations[1].Err != nil {
		t.Fatalf("unexpected retry result=%#v observations=%#v", results, observations)
	}
}

func TestRecoverReturnsPersistentQueryErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "non-2xx", status: http.StatusBadGateway, body: `{}`, wantErr: "non-2xx status: 502"},
		{name: "malformed JSON", status: http.StatusOK, body: `{`, wantErr: "decode logs response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			results, observations, err := Recover(context.Background(), NewClient(sweego.NewClient(server.URL, "token", time.Second)), RecoveryOperation{
				SubmittedAt: time.Now(), Sender: "sender@example.com", Recipients: []string{"alice@example.com"},
			}, "pn-1", RecoveryOptions{Tolerance: time.Minute, Attempts: 2})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
			if results[0].Status != Unresolved || len(observations) != 2 || observations[0].Err == nil || observations[1].Err == nil {
				t.Fatalf("unexpected results=%#v observations=%#v", results, observations)
			}
		})
	}
}

func TestRecoverHonoursContextCancellationDuringRetryDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{})
		cancel()
	}))
	t.Cleanup(server.Close)

	results, observations, err := Recover(ctx, NewClient(sweego.NewClient(server.URL, "token", time.Second)), RecoveryOperation{
		SubmittedAt: time.Now(), Sender: "sender@example.com", Recipients: []string{"alice@example.com"},
	}, "pn-1", RecoveryOptions{Tolerance: time.Minute, RetryDelay: time.Hour, Attempts: 2})
	if !errors.Is(err, context.Canceled) || results[0].Status != Unresolved || len(observations) != 1 {
		t.Fatalf("unexpected cancellation result=%#v observations=%#v err=%v", results, observations, err)
	}
}
