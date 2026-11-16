package truths

import "cellar/pkg/cellar"

// StaleEventFanout is the durable Cellar handler name which fans this Truth out
// to its registered handlers.
const StaleEventFanout cellar.HandlerName = "truths.stale_event"

// StaleEventRegistry declares which handlers receive this Truth.
var StaleEventRegistry = NewRegistry[StaleEvent](StaleEventFanout)

// CreateMeetForEventOccurrenceFanout is the durable Cellar handler name which fans this
// Truth out to its registered handlers.
const CreateMeetForEventOccurrenceFanout cellar.HandlerName = "truths.create_event_poll"

// CreateMeetForEventOccurrenceRegistry declares which handlers receive this Truth.
var CreateMeetForEventOccurrenceRegistry = NewRegistry[CreateMeetForEventOccurrence](CreateMeetForEventOccurrenceFanout)

// StaleEvent states that an event venue's recurrence needs to be recalculated.
// ObservedDate is what the listener saw; the Cell revalidates against current state.
type StaleEvent struct {
	EventID      string `json:"event_id"`
	ObservedDate string `json:"observed_date"`
}

// CreateMeetForEventOccurrence states that an event venue occurrence is due for Meet creation.
type CreateMeetForEventOccurrence struct {
	EventID        string `json:"event_id"`
	OccurrenceDate string `json:"occurrence_date"`
}
