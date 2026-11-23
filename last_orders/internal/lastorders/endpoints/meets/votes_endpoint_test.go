package meets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/services/meetwrite"

	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/votecache"
)

type votingMeetReaderStub struct {
	snapshot meetcache.Snapshot
}

func (reader votingMeetReaderStub) Get(context.Context, string) (meetcache.Snapshot, error) {
	return reader.snapshot, nil
}

type votingSnapshotReaderStub struct {
	snapshot votecache.Snapshot
}

func (reader votingSnapshotReaderStub) Get(context.Context, string) (votecache.Snapshot, error) {
	return reader.snapshot, nil
}

type votingProfilesStub map[string]bool

func (profiles votingProfilesStub) VisibleUIDs(_ context.Context, userIDs []string) map[string]bool {
	visible := make(map[string]bool)
	for _, userID := range userIDs {
		if profiles[userID] {
			visible[userID] = true
		}
	}
	return visible
}

func TestChangeVoteUsesVerifiedUIDAndReturnsRevision(t *testing.T) {
	resultTime := time.Date(2026, 11, 23, 12, 30, 45, 123, time.UTC)
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "public-group", nil
		}),
		WriteAccess: writeAuthorizerStub(func(_ context.Context, uid, permission string) (bool, error) {
			return uid == "verified-user" && permission == "canVote", nil
		}),
		Writer: &voteWriterStub{groupID: "public-group", result: meetwrite.VoteResult{UpdateTime: &resultTime}},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	for _, test := range []struct {
		method  string
		wantAdd bool
	}{
		{method: http.MethodPut, wantAdd: true},
		{method: http.MethodDelete, wantAdd: false},
	} {
		method := test.method
		writer := endpoint.Writer.(*voteWriterStub)
		writer.addCalls = nil
		request := httptest.NewRequest(method, "/api/meet/meet-a/votes/venue-1", nil)
		request.Header.Set("Authorization", "Bearer valid-token")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s vote status=%d body=%s, want HTTP 204", method, response.Code, response.Body)
		}
		if got := response.Header().Get("X-Vote-Revision"); got != "2026-11-23T12:30:45.000000123Z" {
			t.Fatalf("%s vote revision=%q, want committed UTC RFC3339Nano", method, got)
		}
		if len(writer.addCalls) != 1 || writer.addCalls[0] != test.wantAdd {
			t.Fatalf("%s vote add argument=%v, want %v", method, writer.addCalls, test.wantAdd)
		}
	}
}

type voteWriterStub struct {
	groupID  string
	result   meetwrite.VoteResult
	addCalls []bool
}

func (w voteWriterStub) Create(context.Context, string, string, string) (meetwrite.Meet, error) {
	return meetwrite.Meet{}, nil
}
func (w voteWriterStub) GroupID(context.Context, string) (string, error) { return w.groupID, nil }
func (w voteWriterStub) ChangeCandidate(context.Context, string, string, string, bool) (meetwrite.Meet, error) {
	return meetwrite.Meet{}, nil
}
func (w voteWriterStub) Complete(context.Context, string, string, string, string, string) (meetwrite.Meet, error) {
	return meetwrite.Meet{}, nil
}
func (w voteWriterStub) Reschedule(context.Context, string, meetwrite.RescheduleInput, string) (meetwrite.Meet, error) {
	return meetwrite.Meet{}, nil
}
func (w voteWriterStub) Delete(context.Context, string, string) error { return nil }
func (w *voteWriterStub) ChangeVote(_ context.Context, _, venueID, uid string, add bool) (meetwrite.VoteResult, error) {
	if uid != "verified-user" || venueID != "venue-1" {
		return meetwrite.VoteResult{}, meetwrite.ErrInvalidInput
	}
	w.addCalls = append(w.addCalls, add)
	return w.result, nil
}

func TestChangeVoteRequiresCanVotePermission(t *testing.T) {
	writer := &voteWriterStub{groupID: "public-group"}
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "public-group", nil
		}),
		WriteAccess: writeAuthorizerStub(func(_ context.Context, uid, permission string) (bool, error) {
			return false, nil
		}),
		Writer: writer,
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodPut, "/api/meet/meet-a/votes/venue-1", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("vote status=%d body=%s, want HTTP 403", response.Code, response.Body)
	}
	if len(writer.addCalls) != 0 {
		t.Fatalf("denied vote reached mutation writer: %v", writer.addCalls)
	}
}

func TestRegisterVoteSnapshotUnavailableWithoutProjection(t *testing.T) {
	mux := http.NewServeMux()
	(&Endpoint{Auth: pastMeetAuthStub{}}).Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meet/meet-a/votes"))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("voting snapshot GET status=%d body=%s, want HTTP 503", response.Code, response.Body)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "projection_unavailable" {
		t.Fatalf("error code=%q, want projection_unavailable", payload.Error.Code)
	}
}

func TestVoteSnapshotReturnsActiveOptionsAndCallerSelections(t *testing.T) {
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Reader: votingMeetReaderStub{snapshot: meetcache.Snapshot{Projection: meetcache.Projection{
			ID: "meet-a", GroupID: "public-group", CandidateVenueIDs: []string{"venue-1"},
		}}},
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "public-group", nil
		}),
		Voting: votingSnapshotReaderStub{snapshot: votecache.Snapshot{Votes: map[string][]string{
			"venue-1":       {"verified-user", "voter-a", "voter-a"},
			"removed-venue": {"verified-user"},
			"inactive":      {"other-voter"},
		}}},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meet/meet-a/votes"))

	if response.Code != http.StatusOK {
		t.Fatalf("voting snapshot GET status=%d body=%s, want HTTP 200", response.Code, response.Body)
	}
	var payload struct {
		Options []struct {
			ID             string   `json:"id"`
			Total          int      `json:"total"`
			AnonymousCount int      `json:"anonymous_count"`
			VoterUIDs      []string `json:"voter_uids"`
		} `json:"options"`
		SelectedOptionIDs []string `json:"selected_option_ids"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Options) != 2 || payload.Options[0].ID != "any" || payload.Options[1].ID != "venue-1" {
		t.Fatalf("options=%+v, want sorted active candidates plus any", payload.Options)
	}
	if payload.Options[1].Total != 2 || payload.Options[1].AnonymousCount != 2 || len(payload.Options[1].VoterUIDs) != 0 {
		t.Fatalf("venue option=%+v, want two anonymous distinct votes", payload.Options[1])
	}
	if strings.Contains(response.Body.String(), "voter-a") || strings.Contains(response.Body.String(), "inactive") {
		t.Fatalf("snapshot leaked an unconfirmed identity or inactive option: %s", response.Body)
	}
	if len(payload.SelectedOptionIDs) != 2 || payload.SelectedOptionIDs[0] != "removed-venue" || payload.SelectedOptionIDs[1] != "venue-1" {
		t.Fatalf("selected option IDs=%v, want removed and active selections", payload.SelectedOptionIDs)
	}
}

func TestVoteSnapshotStreamReturnsInitialFrame(t *testing.T) {
	codec, err := sse.NewCursorCodec([]byte("test-only-voting-snapshot-cursor-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	hub := sse.NewHub()
	ready := make(chan struct{})
	close(ready)
	revision := time.Date(2026, 11, 23, 13, 40, 15, 123, time.UTC)
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Reader: votingMeetReaderStub{snapshot: meetcache.Snapshot{Projection: meetcache.Projection{
			ID: "meet-a", GroupID: "public-group", CandidateVenueIDs: []string{"venue-1"},
		}}},
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Voting: votingSnapshotReaderStub{snapshot: votecache.Snapshot{
			Votes: map[string][]string{"venue-1": {"verified-user", "voter-a"}},
		}},
		Profiles:    votingProfilesStub{"verified-user": true},
		Cursors:     codec,
		VotingHub:   hub,
		VotingReady: ready,
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/meet/meet-a/votes/stream", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	streamCtx, cancelStream := context.WithCancel(request.Context())
	request = request.WithContext(streamCtx)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-response.flushed:
		cancelStream()
	case <-time.After(time.Second):
		cancelStream()
		t.Fatal("voting snapshot stream did not flush an initial frame")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("voting snapshot stream did not stop after cancellation")
	}

	if response.Code != http.StatusOK {
		t.Fatalf("voting snapshot stream status=%d body=%s, want HTTP 200", response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("stream content type=%q, want text/event-stream", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: voting_snapshot") || !strings.Contains(body, "selected_option_ids") {
		t.Fatalf("initial voting stream frame=%q, want snapshot event", body)
	}
	if strings.Contains(body, "voter-a") {
		t.Fatalf("initial voting stream leaked hidden UID: %s", body)
	}
	if _, err := time.Parse(time.RFC3339Nano, revision.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}

func TestVoteSnapshotIncludesOnlyVisibleProfileUIDs(t *testing.T) {
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Reader: votingMeetReaderStub{snapshot: meetcache.Snapshot{Projection: meetcache.Projection{
			ID: "meet-a", GroupID: "public-group", CandidateVenueIDs: []string{"venue-1"},
		}}},
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Voting: votingSnapshotReaderStub{snapshot: votecache.Snapshot{Votes: map[string][]string{
			"venue-1": {"visible-user", "default-user", "hidden-user", "unavailable-user", "visible-user"},
			"any":     {"verified-user"},
			"removed": {"verified-user"},
		}}},
		Profiles: votingProfilesStub{"visible-user": true, "default-user": true, "verified-user": true},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meet/meet-a/votes"))
	if response.Code != http.StatusOK {
		t.Fatalf("voting snapshot GET status=%d body=%s, want HTTP 200", response.Code, response.Body)
	}
	var payload struct {
		Options []struct {
			ID             string   `json:"id"`
			Total          int      `json:"total"`
			AnonymousCount int      `json:"anonymous_count"`
			VoterUIDs      []string `json:"voter_uids"`
		} `json:"options"`
		SelectedOptionIDs []string `json:"selected_option_ids"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	venueOption := payload.Options[1]
	if venueOption.Total != 4 || venueOption.AnonymousCount != 2 || len(venueOption.VoterUIDs) != 2 ||
		venueOption.VoterUIDs[0] != "default-user" || venueOption.VoterUIDs[1] != "visible-user" {
		t.Fatalf("venue option=%+v, want four votes, two anonymous, and sorted visible UIDs", venueOption)
	}
	if len(payload.SelectedOptionIDs) != 2 || payload.SelectedOptionIDs[0] != "any" || payload.SelectedOptionIDs[1] != "removed" {
		t.Fatalf("selected option IDs=%v, want any and removed", payload.SelectedOptionIDs)
	}
	for _, privateUID := range []string{"hidden-user", "unavailable-user"} {
		if strings.Contains(response.Body.String(), privateUID) {
			t.Fatalf("snapshot leaked private UID %q: %s", privateUID, response.Body)
		}
	}
}
