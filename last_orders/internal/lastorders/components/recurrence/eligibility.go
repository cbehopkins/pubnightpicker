package recurrence

import "time"

const (
	// LeadDays is the poll creation window ahead of an occurrence.
	LeadDays = 7
	// MissingDateKey represents an absent next occurrence inside an idempotency key.
	MissingDateKey = "none"
	// NextOccurrenceField holds the venue's currently calculated next occurrence.
	NextOccurrenceField = "next_occurrence_date"
)

// IdempotencyKey builds the natural key shared by the stale_events and event_due listeners.
func IdempotencyKey(eventID, nextOccurrenceDate string) string {
	if nextOccurrenceDate == "" {
		nextOccurrenceDate = MissingDateKey
	}
	return eventID + "_" + nextOccurrenceDate
}

// IsStale reports whether the stored occurrence is missing, unparseable or in the past.
func IsStale(nextOccurrenceDate string, today time.Time, loc *time.Location) bool {
	occurrence, ok := parseDate(nextOccurrenceDate, loc)
	if !ok {
		return true
	}
	return occurrence.Before(beginningOfDay(today.In(loc)))
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
