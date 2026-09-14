package truths

import "cellar/pkg/cellar"

// DailyPollAutoCompleteDueFanout is the durable Cellar handler name which fans
// this Truth out to its registered handlers.
const DailyPollAutoCompleteDueFanout cellar.HandlerName = "truths.daily_poll_auto_complete_due"

// DailyPollAutoCompleteDueRegistry declares which handlers receive this Truth.
var DailyPollAutoCompleteDueRegistry = NewRegistry[DailyPollAutoCompleteDue](DailyPollAutoCompleteDueFanout)

// PollAutoCompletionDueFanout is the durable Cellar handler name which fans
// this Truth out to its registered handlers.
const PollAutoCompletionDueFanout cellar.HandlerName = "truths.poll_auto_completion_due"

// PollAutoCompletionDueRegistry declares which handlers receive this Truth.
var PollAutoCompletionDueRegistry = NewRegistry[PollAutoCompletionDue](PollAutoCompletionDueFanout)

// DailyPollAutoCompleteDue states that automatic poll-completion discovery is
// due on an observed London calendar date.
type DailyPollAutoCompleteDue struct {
	ObservedOn string `json:"observed_on"`
}

func (truth DailyPollAutoCompleteDue) Identity() string {
	return truth.ObservedOn
}

// PollAutoCompletionSnapshot is the immutable evidence observed for an open
// poll which became due for automatic-completion consideration.
type PollAutoCompletionSnapshot struct {
	PollID     string   `json:"poll_id"`
	PollDate   string   `json:"poll_date"`
	Completed  bool     `json:"completed"`
	VenueIDs   []string `json:"venue_ids"`
	ObservedOn string   `json:"observed_on"`
}

// PollAutoCompletionDue states that a particular poll occurrence became due
// for automatic-completion consideration.
type PollAutoCompletionDue struct {
	Poll PollAutoCompletionSnapshot `json:"poll"`
}

func (truth PollAutoCompletionDue) Identity() string {
	return truth.Poll.PollID + "_" + truth.Poll.PollDate
}
