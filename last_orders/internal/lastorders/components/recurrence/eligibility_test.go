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

func TestEventDueKey(t *testing.T) {
	if got := EventDueKey("event-123", "2026-08-19"); got != "event-123_2026-08-19" {
		t.Fatalf("unexpected key: %s", got)
	}
	if got := EventDueKey("event-123", ""); got != "event-123_none" {
		t.Fatalf("unexpected missing-date key: %s", got)
	}
}

func TestStaleEventKeyChangesWithRecurrenceContent(t *testing.T) {
	ruleA := map[string]any{"frequency": "once", "date": "2026-08-19"}
	ruleB := map[string]any{"frequency": "once", "date": "2026-08-20"}

	keyA := StaleEventKey("event-123", "2026-08-19", ruleA)
	keyAAgain := StaleEventKey("event-123", "2026-08-19", map[string]any{"date": "2026-08-19", "frequency": "once"})
	keyB := StaleEventKey("event-123", "2026-08-19", ruleB)

	if keyA != keyAAgain {
		t.Fatalf("hash should be insensitive to map construction order: %s != %s", keyA, keyAAgain)
	}
	if keyA == keyB {
		t.Fatal("editing the recurrence definition should change the stale_events key")
	}
}

func TestNeedsRecalculation(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	today := londonTime(t, "2026-08-13")
	rule := map[string]any{"frequency": "once", "date": "2026-08-20"}

	if !NeedsRecalculation(rule, "", today, loc) {
		t.Fatal("missing stored date should need recalculation")
	}
	if !NeedsRecalculation(rule, "2026-08-01", today, loc) {
		t.Fatal("stale stored date should need recalculation")
	}
	if NeedsRecalculation(rule, "2026-08-20", today, loc) {
		t.Fatal("stored date matching the current rule should not need recalculation")
	}

	// Editing the rule so the stored (still future) date is no longer what it produces.
	edited := map[string]any{"frequency": "once", "date": "2026-08-25"}
	if !NeedsRecalculation(edited, "2026-08-20", today, loc) {
		t.Fatal("stored date inconsistent with an edited rule should need recalculation")
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
