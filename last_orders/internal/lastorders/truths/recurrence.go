package truths

import "cellar/pkg/cellar"

// StaleEventFanout is the durable Cellar handler name which fans this Truth out
// to its registered handlers.
const StaleEventFanout cellar.HandlerName = "truths.stale_event"

// StaleEventRegistry declares which handlers receive this Truth.
var StaleEventRegistry = NewRegistry[StaleEvent](StaleEventFanout)

// CreateEventPollFanout is the durable Cellar handler name which fans this
// Truth out to its registered handlers.
const CreateEventPollFanout cellar.HandlerName = "truths.create_event_poll"

// CreateEventPollRegistry declares which handlers receive this Truth.
var CreateEventPollRegistry = NewRegistry[CreateEventPoll](CreateEventPollFanout)

// StaleEvent states that an event venue's recurrence needs to be recalculated.
// ObservedDate is what the listener saw; the Cell revalidates against current state.
type StaleEvent struct {
	EventID      string `json:"event_id"`
	ObservedDate string `json:"observed_date"`
}

// CreateEventPoll states that an event venue occurrence is due for
// poll-materialisation.
type CreateEventPoll struct {
	EventID        string `json:"event_id"`
	OccurrenceDate string `json:"occurrence_date"`
}
