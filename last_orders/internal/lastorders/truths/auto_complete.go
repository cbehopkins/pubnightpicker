package truths

const (
	DailyPollAutoCompleteDueName = "DailyPollAutoCompleteDue"
	PollAutoCompletionDueName    = "PollAutoCompletionDue"
)

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
