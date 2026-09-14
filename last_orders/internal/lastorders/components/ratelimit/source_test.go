package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestSourceConsumesTokensAndReportsWaitWhenExhausted(t *testing.T) {
	location := mustLondon(t)
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, location)
	source := mustNewTestSource(t, "email.send", 2, location, nil, func() time.Time { return now })

	if got := source.Acquire(); got != 0 {
		t.Fatalf("first acquire = %d; want grant", got)
	}
	if got := source.Acquire(); got != 0 {
		t.Fatalf("second acquire = %d; want grant", got)
	}
	if got, want := source.Acquire(), 12*60*60; got != want {
		t.Fatalf("exhausted acquire = %d; want %d", got, want)
	}
	if got, want := source.Acquire(), 12*60*60; got != want {
		t.Fatalf("repeated exhausted acquire = %d; want %d", got, want)
	}
}

func TestSourceResetsLazilyOnNewLocalDay(t *testing.T) {
	location := mustLondon(t)
	now := time.Date(2026, time.September, 6, 23, 0, 0, 0, location)
	source := mustNewTestSource(t, "email.send", 2, location, nil, func() time.Time { return now })

	source.Acquire()
	source.Acquire()
	if got := source.Acquire(); got == 0 {
		t.Fatal("exhausted acquire granted a token")
	}

	now = time.Date(2026, time.September, 7, 0, 0, 0, 0, location)
	if got := source.Acquire(); got != 0 {
		t.Fatalf("first acquire after reset = %d; want grant", got)
	}
	if got := source.Acquire(); got != 0 {
		t.Fatalf("second acquire after reset = %d; want grant", got)
	}
}

func TestSourceRoundsExhaustedWaitUp(t *testing.T) {
	location := mustLondon(t)
	now := time.Date(2026, time.September, 6, 23, 59, 59, 500_000_000, location)
	source := mustNewTestSource(t, "email.send", 1, location, nil, func() time.Time { return now })

	source.Acquire()
	if got := source.Acquire(); got != 1 {
		t.Fatalf("exhausted acquire = %d; want 1", got)
	}
}

func TestSourceWaitUsesLocalCalendarAcrossDaylightSaving(t *testing.T) {
	location := mustLondon(t)
	tests := []struct {
		name string
		now  time.Time
		want int
	}{
		{
			name: "spring forward day has twenty-three hours",
			now:  time.Date(2026, time.March, 29, 0, 0, 0, 0, location),
			want: 23 * 60 * 60,
		},
		{
			name: "fall back day has twenty-five hours",
			now:  time.Date(2026, time.October, 25, 0, 0, 0, 0, location),
			want: 25 * 60 * 60,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := mustNewTestSource(t, "email.send", 1, location, nil, func() time.Time { return test.now })
			source.Acquire()
			if got := source.Acquire(); got != test.want {
				t.Fatalf("exhausted acquire = %d; want %d", got, test.want)
			}
		})
	}
}

func TestSourceCallsExhaustionCallbackOncePerTransition(t *testing.T) {
	location := mustLondon(t)
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, location)
	callbackCalls := 0
	var source *Source
	onExhausted := func() {
		callbackCalls++
		if got := source.Acquire(); got == 0 {
			t.Fatal("callback observed a token after exhaustion")
		}
	}
	source = mustNewTestSource(t, "email.send", 1, location, onExhausted, func() time.Time { return now })

	source.Acquire()
	source.Acquire()
	if callbackCalls != 1 {
		t.Fatalf("callback calls = %d; want 1", callbackCalls)
	}

	now = now.AddDate(0, 0, 1)
	source.Acquire()
	if callbackCalls != 2 {
		t.Fatalf("callback calls after reset = %d; want 2", callbackCalls)
	}
}

func TestNewSourceRejectsInvalidConfiguration(t *testing.T) {
	location := mustLondon(t)
	tests := []struct {
		name     string
		source   string
		maximum  int
		location *time.Location
	}{
		{name: "empty name", maximum: 1, location: location},
		{name: "zero maximum", source: "email.send", location: location},
		{name: "negative maximum", source: "email.send", maximum: -1, location: location},
		{name: "nil location", source: "email.send", maximum: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.source, test.maximum, test.location, nil); err == nil {
				t.Fatal("NewSource succeeded; want error")
			}
		})
	}
}

func TestSourceConcurrentFinalTokenHasOneGrant(t *testing.T) {
	location := mustLondon(t)
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, location)
	source := mustNewTestSource(t, "email.send", 1, location, nil, func() time.Time { return now })

	const callers = 100
	results := make(chan int, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for range callers {
		go func() {
			defer group.Done()
			results <- source.Acquire()
		}()
	}
	group.Wait()
	close(results)

	grants := 0
	for result := range results {
		if result == 0 {
			grants++
		}
	}
	if grants != 1 {
		t.Fatalf("grants = %d; want 1", grants)
	}
}

func mustNewTestSource(t *testing.T, name string, maximum int, location *time.Location, onExhausted func(), now func() time.Time) *Source {
	t.Helper()
	source, err := newSource(name, maximum, location, onExhausted, now)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	return source
}

func mustLondon(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London: %v", err)
	}
	return location
}
