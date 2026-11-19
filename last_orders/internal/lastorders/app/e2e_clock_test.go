//go:build e2e

package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/services/autocomplete"
	"last_orders/internal/lastorders/services/autocomplete/autocompletetest"
)

type e2eClockAutocompleteSource struct {
	*autocompletetest.Source
	observed chan string
}

func (source *e2eClockAutocompleteSource) ListOpenPollsThrough(ctx context.Context, observedOn string) ([]autocomplete.Document, error) {
	source.observed <- observedOn
	return source.Source.ListOpenPollsThrough(ctx, observedOn)
}

func TestE2EClockControlHTTPContract(t *testing.T) {
	initial := time.Date(2026, time.July, 4, 15, 59, 0, 0, time.FixedZone("BST", 60*60))
	clock, err := app.NewE2EClock(initial)
	if err != nil {
		t.Fatal(err)
	}
	var notifications atomic.Int32
	server := httptest.NewServer(app.E2EClockControlHandler(clock, func() {
		notifications.Add(1)
	}))
	defer server.Close()

	request := func(body string) (int, map[string]any) {
		t.Helper()
		response, err := http.Post(server.URL+"/clock", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, payload
	}

	for _, body := range []string{
		`{"instant":"not-an-instant"}`,
		`{"instant":"2026-07-04T16:00:00"}`,
		`{"instant":"2026-07-04T16:00:00+01:00","extra":true}`,
	} {
		if status, _ := request(body); status != http.StatusBadRequest {
			t.Fatalf("invalid request status = %d, want %d", status, http.StatusBadRequest)
		}
	}
	if status, _ := request(`{"instant":"2026-07-04T15:58:00+01:00"}`); status != http.StatusConflict {
		t.Fatalf("backwards request status = %d, want %d", status, http.StatusConflict)
	}
	if status, response := request(`{"instant":"2026-07-04T15:59:00+01:00"}`); status != http.StatusOK || response["scheduling_notified"] != false {
		t.Fatalf("equal-time response = %d, %v; want an acknowledged no-op", status, response)
	}
	if notifications.Load() != 0 {
		t.Fatalf("equal-time requests notified scheduling %d times, want 0", notifications.Load())
	}
	if status, response := request(`{"instant":"2026-07-04T16:00:00+01:00"}`); status != http.StatusOK || response["instant"] != "2026-07-04T16:00:00+01:00" || response["scheduling_notified"] != true {
		t.Fatalf("advance response = %d, %v; want accepted instant and scheduling acknowledgement", status, response)
	}
	if notifications.Load() != 1 {
		t.Fatalf("advancement notified scheduling %d times, want 1", notifications.Load())
	}
	if !clock.Now().Equal(time.Date(2026, time.July, 4, 16, 0, 0, 0, time.FixedZone("BST", 60*60))) {
		t.Fatalf("clock after requests = %s, want 2026-07-04T16:00:00+01:00", clock.Now().Format(time.RFC3339Nano))
	}
}

func TestE2EClockControlSerializesConcurrentAdvances(t *testing.T) {
	initial := time.Date(2026, time.July, 4, 15, 59, 0, 0, time.FixedZone("BST", 60*60))
	clock, err := app.NewE2EClock(initial)
	if err != nil {
		t.Fatal(err)
	}
	var notifications atomic.Int32
	server := httptest.NewServer(app.E2EClockControlHandler(clock, func() {
		notifications.Add(1)
	}))
	defer server.Close()

	const advances = 16
	start := make(chan struct{})
	statuses := make(chan int, advances)
	var requests sync.WaitGroup
	for minute := 1; minute <= advances; minute++ {
		instant := initial.Add(time.Duration(minute) * time.Minute).Format(time.RFC3339Nano)
		requests.Add(1)
		go func() {
			defer requests.Done()
			<-start
			response, err := http.Post(server.URL+"/clock", "application/json", strings.NewReader(`{"instant":"`+instant+`"}`))
			if err != nil {
				statuses <- 0
				return
			}
			defer response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	close(start)
	requests.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK && status != http.StatusConflict {
			t.Fatalf("concurrent advance status = %d, want 200 or 409", status)
		}
	}

	response, err := http.Get(server.URL + "/clock")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var accepted struct {
		Instant string `json:"instant"`
	}
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	want := initial.Add(advances * time.Minute)
	got, err := time.Parse(time.RFC3339Nano, accepted.Instant)
	if err != nil || !got.Equal(want) {
		t.Fatalf("clock after concurrent advances = %q, %v; want %s", accepted.Instant, err, want.Format(time.RFC3339Nano))
	}
	if notifications.Load() < 1 || notifications.Load() > advances {
		t.Fatalf("scheduling notifications = %d, want between 1 and %d", notifications.Load(), advances)
	}
}

func TestE2EClockControlMakesDeadlineEligibleThroughRuntime(t *testing.T) {
	initial := time.Date(2026, time.July, 4, 15, 59, 0, 0, time.FixedZone("BST", 60*60))
	clock, err := app.NewE2EClock(initial)
	if err != nil {
		t.Fatal(err)
	}
	source := &e2eClockAutocompleteSource{
		Source:   autocompletetest.New(),
		observed: make(chan string, 4),
	}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "e2e-clock.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Clock = clock
	cfg.AutocompleteSource = source
	application := newApp(t, cfg)
	defer application.Close()

	runCtx, stopRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- application.Run(runCtx) }()
	defer func() {
		stopRun()
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("run app: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("app did not stop while application time was frozen")
		}
	}()

	select {
	case observedOn := <-source.observed:
		if observedOn != "2026-07-03" {
			t.Fatalf("startup discovery date = %q, want 2026-07-03 before today's deadline", observedOn)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup discovery did not run")
	}
	source.SeedPoll("e2e-deadline", map[string]any{
		"date":      "2026-07-04",
		"completed": false,
		"pubs":      map[string]any{"venue-1": map[string]any{}},
	})
	poll, err := source.GetPoll(context.Background(), "e2e-deadline")
	if err != nil {
		t.Fatal(err)
	}
	if completed, _ := poll.Data["completed"].(bool); completed {
		t.Fatal("voting completed before the 16:00 application-time deadline")
	}

	control := httptest.NewServer(app.E2EClockControlHandler(clock, application.NotifyScheduling))
	defer control.Close()
	response, err := http.Post(control.URL+"/clock", "application/json", strings.NewReader(`{"instant":"2026-07-04T16:00:00+01:00"}`))
	if err != nil {
		t.Fatal(err)
	}
	var acknowledgement struct {
		Instant            string `json:"instant"`
		SchedulingNotified bool   `json:"scheduling_notified"`
	}
	if err := json.NewDecoder(response.Body).Decode(&acknowledgement); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || acknowledgement.Instant != "2026-07-04T16:00:00+01:00" || !acknowledgement.SchedulingNotified {
		t.Fatalf("clock acknowledgement = %d, %+v", response.StatusCode, acknowledgement)
	}
	select {
	case observedOn := <-source.observed:
		if observedOn != "2026-07-04" {
			t.Fatalf("deadline discovery date = %q, want 2026-07-04", observedOn)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadline discovery did not run after the clock advanced")
	}

	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		poll, err := source.GetPoll(runCtx, "e2e-deadline")
		if err != nil {
			t.Fatal(err)
		}
		if completed, _ := poll.Data["completed"].(bool); completed {
			return
		}
		select {
		case <-deadline:
			t.Fatal("eligible voting did not complete through the production runtime")
		case <-ticker.C:
		}
	}
}
