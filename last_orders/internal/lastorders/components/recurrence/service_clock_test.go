package recurrence

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/groupcache"
	"last_orders/internal/lastorders/components/groupcache/groupcachetest"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
	_ "modernc.org/sqlite"
)

type fixedClock struct {
	instant time.Time
}

func (clock *fixedClock) Now() time.Time {
	return clock.instant
}

func newClockTestGroupService(t *testing.T) *groupcache.Service {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := groupcache.NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	service, err := groupcache.NewService(store, groupcachetest.New())
	if err != nil {
		t.Fatal(err)
	}
	return service
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
	groups := newClockTestGroupService(t)

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
			service, err := NewServiceWithClock(client, nil, nil, groups, clock)
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
