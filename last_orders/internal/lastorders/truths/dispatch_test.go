package truths

import "testing"

// allFanoutNames lists every declared Truth Fanout name. Every Truth must be
// registered here so a duplicate name is caught rather than silently colliding.
var allFanoutNames = []string{
	string(EventVenueObservedFanout),
	string(DailyPollAutoCompleteDueFanout),
	string(PollAutoCompletionDueFanout),
	string(PollOpenedFanout),
	string(PollCompletedFanout),
	string(StaleEventFanout),
	string(CreateEventPollFanout),
	string(LogMessageFanout),
}

func TestFanoutNamesAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(allFanoutNames))
	for _, name := range allFanoutNames {
		if name == "" {
			t.Fatalf("truth fanout name must not be empty")
		}
		if seen[name] {
			t.Fatalf("duplicate truth fanout name: %q", name)
		}
		seen[name] = true
	}
}
