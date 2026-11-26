package attendancestats

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestAttendanceStatisticsAgainstFirestoreEmulatorAcrossGroups(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	address, err := net.ResolveTCPAddr("tcp", host)
	if err != nil || address == nil || !address.IP.IsLoopback() {
		t.Fatalf("refusing non-loopback Firestore host %q", host)
	}

	const projectID = "demo-attendance-statistics"
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	for _, venue := range []struct {
		id        string
		venueType string
	}{
		{id: "pub-a", venueType: "pub"},
		{id: "restaurant-a", venueType: "restaurant"},
	} {
		if _, err := client.Collection("pubs").Doc(venue.id).Set(ctx, map[string]any{
			"name": venue.id, "venueType": venue.venueType,
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, meet := range []struct {
		id         string
		groupID    string
		date       string
		selected   string
		restaurant string
		attendance map[string]any
	}{
		{
			id: "meet-a", groupID: "group-a", date: "2026-04-04", selected: "pub-a", restaurant: "restaurant-a",
			attendance: map[string]any{
				"any":          map[string]any{"canCome": []string{"alice", "bob"}, "cannotCome": []string{}},
				"pub-a":        map[string]any{"canCome": []string{"carol"}, "cannotCome": []string{"bob"}},
				"restaurant-a": map[string]any{"canCome": []string{"bob"}, "cannotCome": []string{}},
			},
		},
		{
			id: "meet-b", groupID: "group-b", date: "2026-05-05", selected: "pub-a", restaurant: "restaurant-a",
			attendance: map[string]any{
				"any":          map[string]any{"canCome": []string{"dave"}, "cannotCome": []string{}},
				"restaurant-a": map[string]any{"canCome": []string{"erin"}, "cannotCome": []string{}},
			},
		},
	} {
		if _, err := client.Collection("polls").Doc(meet.id).Set(ctx, map[string]any{
			"groupId": meet.groupID, "date": meet.date, "completed": true,
			"selected": meet.selected, "restaurant": meet.restaurant,
			"pubs": map[string]any{meet.selected: true},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Collection("attendance").Doc(meet.id).Set(ctx, meet.attendance); err != nil {
			t.Fatal(err)
		}
	}

	service, err := NewService(client)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Read(ctx, "2026-04-01", "2026-05-31")
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts["pub-a"] != 4 {
		t.Fatalf("pub-a count = %d, want 4", result.Counts["pub-a"])
	}
	if result.Counts["restaurant-a"] != 2 {
		t.Fatalf("restaurant-a count = %d, want 2", result.Counts["restaurant-a"])
	}
	if result.LastDates["pub-a"] != "2026-05-05" || result.LastDates["restaurant-a"] != "2026-05-05" {
		t.Fatalf("unexpected last dates: %#v", result.LastDates)
	}
}
