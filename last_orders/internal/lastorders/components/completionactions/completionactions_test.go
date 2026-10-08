package completionactions

import "testing"

func TestKeyMatchesPythonCompleteKey(t *testing.T) {
	tests := []struct {
		pub, restaurant, time, want string
	}{
		{pub: "pub-1", want: "pub-1"},
		{pub: "pub-1", restaurant: "restaurant-1", want: "pub-1:restaurant-1:"},
		{pub: "pub-1", restaurant: "restaurant-1", time: "18:30", want: "pub-1:restaurant-1:18:30"},
		{pub: "pub-1", time: "18:30", want: "pub-1::18:30"},
	}
	for _, test := range tests {
		if got := Key(test.pub, test.restaurant, test.time); got != test.want {
			t.Errorf("Key(%q, %q, %q) = %q, want %q", test.pub, test.restaurant, test.time, got, test.want)
		}
	}
}

func TestRecordMatchesPythonActionTrack(t *testing.T) {
	record := Record{ActionEmail: {"pub-1"}, ActionPersonalEmail: {}}

	if record.NeedsAction(ActionEmail, "pub-1") {
		t.Error("an actioned key must not need action")
	}
	if !record.NeedsAction(ActionEmail, "pub-2") {
		t.Error("a new configuration must need action")
	}
	if !record.PreviouslyActioned(ActionEmail) {
		t.Error("an action with recorded keys was previously actioned")
	}
	if !record.PreviouslyActioned(ActionPersonalEmail) {
		t.Error("a present action field counts as previously actioned, as in Python")
	}
	if (Record{}).PreviouslyActioned(ActionEmail) {
		t.Error("a missing action field was not previously actioned")
	}
}
