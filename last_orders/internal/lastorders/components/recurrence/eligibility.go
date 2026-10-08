package recurrence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	// LeadDays is the poll creation window ahead of an occurrence.
	LeadDays = 7
	// MissingDateKey represents an absent next occurrence inside an idempotency key.
	MissingDateKey = "none"
	// NextOccurrenceField holds the venue's currently calculated next occurrence.
	NextOccurrenceField = "next_occurrence_date"
)

// EventDueKey builds the event_due idempotency key. Poll materialisation only
// depends on the occurrence date, not the recurrence definition that produced it.
func EventDueKey(eventID, nextOccurrenceDate string) string {
	if nextOccurrenceDate == "" {
		nextOccurrenceDate = MissingDateKey
	}
	return eventID + "_" + nextOccurrenceDate
}

// StaleEventKey builds the stale_events idempotency key. It also depends on the
// recurrence definition so that editing the recurrence produces a new key even
// though next_occurrence_date has not been recalculated yet.
func StaleEventKey(eventID, nextOccurrenceDate string, recurrenceRaw map[string]any) string {
	return EventDueKey(eventID, nextOccurrenceDate) + "_" + RecurrenceHash(recurrenceRaw)
}

// RecurrenceHash is a deterministic content hash of a recurrence definition.
// encoding/json sorts map keys, so equal definitions hash equally regardless of
// construction order.
func RecurrenceHash(recurrenceRaw map[string]any) string {
	canonical, err := json.Marshal(recurrenceRaw)
	if err != nil {
		canonical = []byte(err.Error())
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:12]
}

// NeedsRecalculation reports whether the stored occurrence no longer matches what
// the current recurrence definition produces relative to today. This subsumes a
// purely date-based staleness check: a missing or past stored date will also
// mismatch a freshly computed occurrence, and so does an edited recurrence rule
// whose stored date happens to still be in the future.
func NeedsRecalculation(recurrenceRaw map[string]any, storedDate string, today time.Time, loc *time.Location) bool {
	return computeOccurrence(recurrenceRaw, today, loc) != storedDate
}

func computeOccurrence(recurrenceRaw map[string]any, today time.Time, loc *time.Location) string {
	rule, ok := ParseRule(recurrenceRaw)
	if !ok {
		return ""
	}
	occurrence, ok := NextOccurrence(rule, beginningOfDay(today.In(loc)), loc)
	if !ok {
		return ""
	}
	return occurrence.String()
}

// IsDue reports whether the stored occurrence sits inside the poll creation window.
func IsDue(nextOccurrenceDate string, today time.Time, loc *time.Location) bool {
	occurrence, ok := parseDate(nextOccurrenceDate, loc)
	if !ok {
		return false
	}
	day := beginningOfDay(today.In(loc))
	return !occurrence.Before(day) && !occurrence.After(day.AddDate(0, 0, LeadDays))
}
