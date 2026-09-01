// Package truths defines durable application observations independent of their
// source and the plugins that react to them.
package truths

import "last_orders/internal/lastorders/components/recurrence"

const EventVenueObservedName = "EventVenueObserved"

// EventVenueObserved is immutable evidence captured when an event venue is
// observed. ObservedOn preserves the London calendar date used for evaluation.
type EventVenueObserved struct {
	Venue      recurrence.EventVenue `json:"venue"`
	ObservedOn string                `json:"observed_on"`
}

// Identity defines when two event venue observations describe the same Truth.
func (event EventVenueObserved) Identity() string {
	return event.Venue.ID + "_" + event.Venue.NextOccurrenceDate + "_" + recurrence.RecurrenceHash(event.Venue.Recurrence) + "_" + event.ObservedOn
}
