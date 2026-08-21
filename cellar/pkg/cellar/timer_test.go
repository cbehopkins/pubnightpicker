package cellar

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestTimerSchedulePersistsOneNamedTimer(t *testing.T) {
	store := NewMemoryStore(nil)
	runtime := New(store, Config{})
	timer, err := NewTimer("reports.refresh", TimerConfig{
		Interval: 5 * time.Minute,
		Mode:     TimerFixedDelay,
	}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("NewTimer() error = %v", err)
	}
	if err := timer.Register(runtime); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	firstID, err := timer.Schedule(runtime)
	if err != nil {
		t.Fatalf("first Schedule() error = %v", err)
	}
	secondID, err := timer.Schedule(runtime)
	if !errors.Is(err, ErrTimerAlreadyExists) {
		t.Fatalf("second Schedule() error = %v, want ErrTimerAlreadyExists", err)
	}
	if secondID != "" {
		t.Fatalf("second Schedule() ID = %q, want empty", secondID)
	}

	cells, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("active cells = %d, want 1", len(cells))
	}
	if cells[0].ID != firstID {
		t.Fatalf("persisted timer ID = %q, want %q", cells[0].ID, firstID)
	}
	if cells[0].HandlerName != "reports.refresh" {
		t.Fatalf("persisted handler name = %q, want reports.refresh", cells[0].HandlerName)
	}
	if cells[0].NotBefore == nil || !cells[0].NotBefore.After(time.Now()) {
		t.Fatalf("persisted not-before = %v, want future time", cells[0].NotBefore)
	}
}

func TestTimerRegisterRejectsDuplicateName(t *testing.T) {
	runtime := New(NewMemoryStore(nil), Config{})
	first, err := NewTimer("reports.refresh", TimerConfig{Interval: time.Minute, Mode: TimerFixedDelay}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("first NewTimer() error = %v", err)
	}
	second, err := NewTimer("reports.refresh", TimerConfig{Interval: time.Hour, Mode: TimerFixedRate}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("second NewTimer() error = %v", err)
	}
	if err := first.Register(runtime); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := second.Register(runtime); !errors.Is(err, ErrTimerAlreadyExists) {
		t.Fatalf("second Register() error = %v, want ErrTimerAlreadyExists", err)
	}
}

func TestTimerFixedDelayRunsAgainAfterCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 5 * time.Second
		registration := newTestTimerRegistration(t, TimerFixedDelay, interval, nil)
		ranAt := time.Now()
		result := registration.Execute(t.Context(), testTimerCell(t, TimerFixedDelay, interval, ranAt))

		retry, ok := result.(Retry)
		if !ok {
			t.Fatalf("result = %T, want Retry", result)
		}
		want := ranAt.Add(interval)
		if retry.NotBefore == nil || !retry.NotBefore.Equal(want) {
			t.Fatalf("next run = %v, want %v", retry.NotBefore, want)
		}
	})
}

func TestTimerFixedRateCoalescesMissedTicks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 5 * time.Second
		registration := newTestTimerRegistration(t, TimerFixedRate, interval, nil)
		now := time.Now()
		previousDue := now.Add(-12 * time.Second)
		result := registration.Execute(t.Context(), testTimerCell(t, TimerFixedRate, interval, previousDue))

		retry, ok := result.(Retry)
		if !ok {
			t.Fatalf("result = %T, want Retry", result)
		}
		want := now.Add(3 * time.Second)
		if retry.NotBefore == nil || !retry.NotBefore.Equal(want) {
			t.Fatalf("next run = %v, want %v", retry.NotBefore, want)
		}
	})
}

func TestTimerCallbackErrorDeletesTimer(t *testing.T) {
	want := errors.New("application handled failure")
	registration := newTestTimerRegistration(t, TimerFixedDelay, time.Minute, want)
	result := registration.Execute(context.Background(), testTimerCell(t, TimerFixedDelay, time.Minute, time.Now()))
	complete, ok := result.(Complete)
	if !ok {
		t.Fatalf("result = %T, want Complete", result)
	}
	if len(complete.NewCells) != 0 {
		t.Fatalf("new cells = %d, want 0", len(complete.NewCells))
	}
}

func TestTimerValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		timer    HandlerName
		config   TimerConfig
		callback TimerCallback
		want     error
	}{
		{name: "name", config: TimerConfig{Interval: time.Second, Mode: TimerFixedDelay}, callback: func(context.Context) error { return nil }, want: ErrHandlerNameRequired},
		{name: "interval", timer: "timer", config: TimerConfig{Mode: TimerFixedDelay}, callback: func(context.Context) error { return nil }, want: ErrTimerIntervalInvalid},
		{name: "mode", timer: "timer", config: TimerConfig{Interval: time.Second, Mode: "unknown"}, callback: func(context.Context) error { return nil }, want: ErrTimerModeInvalid},
		{name: "callback", timer: "timer", config: TimerConfig{Interval: time.Second, Mode: TimerFixedDelay}, want: ErrTimerCallbackNil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewTimer(test.timer, test.config, test.callback)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewTimer() error = %v, want %v", err, test.want)
			}
		})
	}
}

func newTestTimerRegistration(t *testing.T, mode TimerMode, interval time.Duration, callbackErr error) Registration {
	t.Helper()
	timer, err := NewTimer("timer.test", TimerConfig{Interval: interval, Mode: mode}, func(context.Context) error {
		return callbackErr
	})
	if err != nil {
		t.Fatalf("NewTimer() error = %v", err)
	}
	runtime := New(NewMemoryStore(nil), Config{})
	if err := timer.Register(runtime); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registration, ok := runtime.registry.Lookup("timer.test")
	if !ok {
		t.Fatal("timer registration not found")
	}
	return registration
}

func testTimerCell(t *testing.T, mode TimerMode, interval time.Duration, due time.Time) Cell {
	t.Helper()
	raw, err := marshalJSON(timerPayload{Interval: interval, Mode: mode})
	if err != nil {
		t.Fatalf("marshalJSON() error = %v", err)
	}
	return Cell{
		ID:          "timer:timer.test",
		HandlerName: "timer.test",
		Payload:     raw,
		State:       CellStateClaimed,
		NotBefore:   &due,
	}
}
