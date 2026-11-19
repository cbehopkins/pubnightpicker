package recurrence

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
)

type fixedClock struct {
	instant time.Time
}

func (clock *fixedClock) Now() time.Time {
	return clock.instant
}

func TestServiceTodayTracksInjectedClockAcrossLondonDateTransitions(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	client, err := firestore.NewClient(context.Background(), "clock-test-project", option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tests := []struct {
		name       string
		before     time.Time
		after      time.Time
		wantBefore string
		wantAfter  string
	}{
		{
			name:       "winter offset",
			before:     time.Date(2026, time.January, 1, 23, 30, 0, 0, time.UTC),
			after:      time.Date(2026, time.January, 2, 0, 30, 0, 0, time.UTC),
			wantBefore: "2026-01-01 23:30 +0000",
			wantAfter:  "2026-01-02 00:30 +0000",
		},
		{
			name:       "summer offset",
			before:     time.Date(2026, time.July, 1, 22, 30, 0, 0, time.UTC),
			after:      time.Date(2026, time.July, 1, 23, 30, 0, 0, time.UTC),
			wantBefore: "2026-07-01 23:30 +0100",
			wantAfter:  "2026-07-02 00:30 +0100",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fixedClock{instant: test.before}
			service, err := NewServiceWithClock(client, nil, nil, clock)
			if err != nil {
				t.Fatal(err)
			}
			if got := service.Today().In(location).Format("2006-01-02 15:04 -0700"); got != test.wantBefore {
				t.Fatalf("Today() before transition = %s; want %s", got, test.wantBefore)
			}
			clock.instant = test.after
			if got := service.Today().In(location).Format("2006-01-02 15:04 -0700"); got != test.wantAfter {
				t.Fatalf("Today() after transition = %s; want %s", got, test.wantAfter)
			}
		})
	}
}
