package truths

import "testing"

func TestDailyPollAutoCompleteDueIdentity(t *testing.T) {
	truth := DailyPollAutoCompleteDue{ObservedOn: "2026-09-01"}
	if got, want := truth.Identity(), "2026-09-01"; got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestPollAutoCompletionDueIdentity(t *testing.T) {
	truth := PollAutoCompletionDue{Poll: PollAutoCompletionSnapshot{
		PollID:   "poll-1",
		PollDate: "2026-09-01",
	}}
	if got, want := truth.Identity(), "poll-1_2026-09-01"; got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}
