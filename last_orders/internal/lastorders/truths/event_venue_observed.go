// Package truths defines durable application observations independent of their
// source and the plugins that react to them.
package truths

import (
	"cellar/pkg/cellar"

	"last_orders/internal/lastorders/components/recurrence"
)

// EventVenueObservedFanout is the durable Cellar handler name which fans this
// Truth out to its registered handlers.
const EventVenueObservedFanout cellar.HandlerName = "truths.event_venue_observed"

// EventVenueObservedRegistry declares which handlers receive this Truth.
var EventVenueObservedRegistry = NewRegistry[EventVenueObserved](EventVenueObservedFanout)

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
