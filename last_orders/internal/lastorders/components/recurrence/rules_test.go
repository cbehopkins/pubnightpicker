package recurrence

import (
	"testing"
	"time"
)

func TestNextOccurrenceOnce(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}

	rule := Rule{Frequency: "once", Date: "2026-08-17", Interval: 1}
	occ, ok := NextOccurrence(rule, time.Date(2026, 8, 1, 12, 0, 0, 0, loc), loc)
	if !ok {
		t.Fatal("expected occurrence")
	}
	if occ.Time(loc).Format("2006-01-02") != "2026-08-17" {
		t.Fatalf("unexpected occurrence: %s", occ.Time(loc).Format("2006-01-02"))
	}
}

func TestNextOccurrenceWeekly(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	weekday := 2 // Wednesday
	rule := Rule{Frequency: "weekly", Interval: 1, Weekday: &weekday}
	occ, ok := NextOccurrence(rule, time.Date(2026, 5, 5, 12, 0, 0, 0, loc), loc)
	if !ok {
		t.Fatal("expected weekly occurrence")
	}
	if occ.Time(loc).Weekday() != time.Wednesday {
		t.Fatalf("expected Wednesday, got %s", occ.Time(loc).Weekday())
	}
}

func TestNextOccurrenceMonthlyLastWednesday(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	weekday := 2
	nth := -1
	rule := Rule{Frequency: "monthly", Interval: 1, Weekday: &weekday, Nth: &nth}
	occ, ok := NextOccurrence(rule, time.Date(2026, 2, 1, 0, 0, 0, 0, loc), loc)
	if !ok {
		t.Fatal("expected monthly occurrence")
	}
	if occ.Time(loc).Format("2006-01-02") != "2026-02-25" {
		t.Fatalf("unexpected monthly occurrence: %s", occ.Time(loc).Format("2006-01-02"))
	}
}
