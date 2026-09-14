package truths

import "cellar/pkg/cellar"

// PollOpenedFanout is the durable Cellar handler name which fans this Truth out
// to its registered handlers.
const PollOpenedFanout cellar.HandlerName = "truths.poll_opened"

// PollOpenedRegistry declares which handlers receive this Truth.
var PollOpenedRegistry = NewRegistry[PollObservedPayload](PollOpenedFanout)

// PollCompletedFanout is the durable Cellar handler name which fans this Truth
// out to its registered handlers.
const PollCompletedFanout cellar.HandlerName = "truths.poll_completed"

// PollCompletedRegistry declares which handlers receive this Truth.
var PollCompletedRegistry = NewRegistry[PollObservedPayload](PollCompletedFanout)

// PollObservedPayload is the evidence observed for a poll document change.
//
// PollOpened and PollCompleted are distinct Truths which currently share this
// payload shape (see ADR-0009 §15).
type PollObservedPayload struct {
	PollID                 string `json:"poll_id"`
	ChangeKind             string `json:"change_kind,omitempty"`
	SelectedRestaurantID   string `json:"selected_restaurant_id,omitempty"`
	SelectedRestaurantTime string `json:"selected_restaurant_time,omitempty"`
}
