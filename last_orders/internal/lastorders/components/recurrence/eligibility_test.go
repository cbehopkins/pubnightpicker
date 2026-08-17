package recurrence

import (
	"testing"
	"time"
)

func londonTime(t *testing.T, date string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestIdempotencyKey(t *testing.T) {
	if got := IdempotencyKey("event-123", "2026-08-19"); got != "event-123_2026-08-19" {
		t.Fatalf("unexpected key: %s", got)
	}
	if got := IdempotencyKey("event-123", ""); got != "event-123_none" {
		t.Fatalf("unexpected missing-date key: %s", got)
	}
}

func TestIsStale(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	today := londonTime(t, "2026-08-13")

	cases := map[string]bool{
		"":           true,
		"nonsense":   true,
		"2026-08-12": true,
		"2026-08-13": false,
		"2026-08-14": false,
	}
	for date, want := range cases {
		if got := IsStale(date, today, loc); got != want {
			t.Fatalf("IsStale(%q) = %v, want %v", date, got, want)
		}
	}
}

func TestIsDue(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	today := londonTime(t, "2026-08-13")

	cases := map[string]bool{
		"":           false,
		"2026-08-12": false,
		"2026-08-13": true,
		"2026-08-20": true,
		"2026-08-21": false,
	}
	for date, want := range cases {
		if got := IsDue(date, today, loc); got != want {
			t.Fatalf("IsDue(%q) = %v, want %v", date, got, want)
		}
	}
}

func TestStaleAndDueAreMutuallyExclusive(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	today := londonTime(t, "2026-08-13")

	for offset := -10; offset <= 10; offset++ {
		date := today.AddDate(0, 0, offset).Format("2006-01-02")
		if IsStale(date, today, loc) && IsDue(date, today, loc) {
			t.Fatalf("%s is both stale and due", date)
		}
	}
}
