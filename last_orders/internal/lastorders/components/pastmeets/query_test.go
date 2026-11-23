package pastmeets

import (
	"context"
	"errors"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/meetcache"
)

type sourceStub struct {
	documents []meetcache.Document
	err       error
	groupID   string
	before    string
	limit     int
	afterDate string
	afterID   string
}

func (s *sourceStub) Query(_ context.Context, groupID, beforeDate string, limit int, afterDate, afterID string) ([]meetcache.Document, error) {
	s.groupID, s.before, s.limit, s.afterDate, s.afterID = groupID, beforeDate, limit, afterDate, afterID
	return s.documents, s.err
}

func TestListUsesLondonDateAndRequestsOneExtraDocument(t *testing.T) {
	source := &sourceStub{documents: []meetcache.Document{
		pastMeetDocument("meet-new", "group-a", "2026-07-14"),
		pastMeetDocument("meet-old", "group-a", "2026-07-13"),
	}}
	now := func() time.Time { return time.Date(2026, 7, 14, 23, 30, 0, 0, time.UTC) }
	service, err := NewService(source, now)
	if err != nil {
		t.Fatal(err)
	}
	if today := service.Today(); today != "2026-07-15" {
		t.Fatalf("London date=%q, want 2026-07-15", today)
	}

	meets, hasNext, err := service.List(context.Background(), "group-a", service.Today(), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if source.groupID != "group-a" || source.before != "2026-07-15" || source.limit != 2 || source.afterDate != "" || source.afterID != "" {
		t.Fatalf("query args group=%q before=%q limit=%d after=(%q,%q)", source.groupID, source.before, source.limit, source.afterDate, source.afterID)
	}
	if len(meets) != 1 || meets[0].ID != "meet-new" || !hasNext {
		t.Fatalf("page meets=%+v hasNext=%t", meets, hasNext)
	}
}

func TestListPreservesKeysetPositionAndSourceErrors(t *testing.T) {
	wantErr := errors.New("Firestore unavailable")
	source := &sourceStub{err: wantErr}
	service, err := NewService(source, func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.List(context.Background(), "group-a", "2026-11-20", 20, "2026-11-19", "meet-9")
	if !errors.Is(err, wantErr) {
		t.Fatalf("List error=%v, want wrapped source error", err)
	}
	if source.afterDate != "2026-11-19" || source.afterID != "meet-9" {
		t.Fatalf("source position=(%q,%q), want (2026-11-19, meet-9)", source.afterDate, source.afterID)
	}
}

func TestListRejectsIncompleteOrOutOfScopeQueries(t *testing.T) {
	source := &sourceStub{}
	service, err := NewService(source, func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		groupID   string
		before    string
		limit     int
		afterDate string
		afterID   string
	}{
		{name: "empty group", before: "2026-11-20", limit: 20},
		{name: "stale date", groupID: "group-a", before: "2026-11-21", limit: 20},
		{name: "invalid limit", groupID: "group-a", before: "2026-11-20", limit: 101},
		{name: "incomplete position", groupID: "group-a", before: "2026-11-20", limit: 20, afterDate: "2026-11-19"},
		{name: "position not before today", groupID: "group-a", before: "2026-11-20", limit: 20, afterDate: "2026-11-20", afterID: "meet-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := service.List(context.Background(), test.groupID, test.before, test.limit, test.afterDate, test.afterID)
			if err == nil {
				t.Fatal("List succeeded for invalid query")
			}
		})
	}
	if source.groupID != "" {
		t.Fatalf("invalid query reached source: %+v", source)
	}
}

func pastMeetDocument(id, groupID, date string) meetcache.Document {
	return meetcache.Document{
		ID: id, Exists: true, Data: map[string]any{"groupId": groupID, "date": date, "completed": true},
	}
}
