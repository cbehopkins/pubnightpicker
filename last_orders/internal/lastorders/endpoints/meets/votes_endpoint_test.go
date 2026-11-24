package meets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func (profiles votingProfilesStub) VisibleProfiles(_ context.Context, userIDs []string) map[string]votecache.VoterProfile {
	visible := make(map[string]votecache.VoterProfile)
	for _, userID := range userIDs {
		if profiles[userID] {
			name := "Name " + userID
			visible[userID] = votecache.VoterProfile{ID: userID, Name: &name}
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
			ID             string         `json:"id"`
			Total          int            `json:"total"`
			AnonymousCount int            `json:"anonymous_count"`
			Voters         []voterSummary `json:"voters"`
		} `json:"options"`
		SelectedOptionIDs []string `json:"selected_option_ids"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Options) != 2 || payload.Options[0].ID != "any" || payload.Options[1].ID != "venue-1" {
		t.Fatalf("options=%+v, want sorted active candidates plus any", payload.Options)
	}
	if payload.Options[1].Total != 2 || payload.Options[1].AnonymousCount != 2 || len(payload.Options[1].Voters) != 0 {
		t.Fatalf("venue option=%+v, want two anonymous distinct votes", payload.Options[1])
	}
	if strings.Contains(response.Body.String(), "voter-a") || strings.Contains(response.Body.String(), "inactive") {
		t.Fatalf("snapshot leaked an unconfirmed identity or inactive option: %s", response.Body)
	}
	if len(payload.SelectedOptionIDs) != 2 || payload.SelectedOptionIDs[0] != "removed-venue" || payload.SelectedOptionIDs[1] != "venue-1" {
		t.Fatalf("selected option IDs=%v, want removed and active selections", payload.SelectedOptionIDs)
	}
}

type votingMeetMapStub map[string]meetcache.Projection

func (reader votingMeetMapStub) Get(_ context.Context, meetID string) (meetcache.Snapshot, error) {
	projection, ok := reader[meetID]
	if !ok {
		return meetcache.Snapshot{}, meetcache.ErrNotFound
	}
	return meetcache.Snapshot{Projection: projection}, nil
}

type votingSnapshotMapStub map[string]votecache.Snapshot

func (reader votingSnapshotMapStub) Get(_ context.Context, meetID string) (votecache.Snapshot, error) {
	return reader[meetID], nil
}

func groupVotingStreamEndpoint(t *testing.T) *Endpoint {
	t.Helper()
	codec, err := sse.NewCursorCodec([]byte("test-only-voting-snapshot-cursor-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	close(ready)
	revision := time.Date(2026, 11, 23, 13, 40, 15, 123, time.UTC)
	return &Endpoint{
		Auth: pastMeetAuthStub{},
		Reader: votingMeetMapStub{
			"meet-a":     {ID: "meet-a", GroupID: "public-group", CandidateVenueIDs: []string{"venue-1"}},
			"meet-b":     {ID: "meet-b", GroupID: "public-group", CandidateVenueIDs: []string{"venue-2"}},
			"other-meet": {ID: "other-meet", GroupID: "other-group"},
		},
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Voting: votingSnapshotMapStub{
			"meet-a": {
				Votes: map[string][]string{
					"venue-1": {"verified-user", "voter-a", "voter-a"},
					"removed": {"verified-user"},
				},
				Revision: &revision, Sequence: 7,
			},
			"meet-b": {Votes: map[string][]string{"any": {"voter-b"}}, Sequence: 4},
		},
		Profiles:    votingProfilesStub{"verified-user": true},
		Cursors:     codec,
		VotingHub:   sse.NewHub(),
		VotingReady: ready,
	}
}

func TestGroupVoteStreamReturnsInitialFrameForEveryListedMeet(t *testing.T) {
	endpoint := groupVotingStreamEndpoint(t)
	mux := http.NewServeMux()
	endpoint.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/meets/public-group/votes/stream?meet_id=meet-b&meet_id=meet-a", nil)
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
		t.Fatal("group voting stream did not flush an initial frame")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("group voting stream did not stop after cancellation")
	}

	if response.Code != http.StatusOK {
		t.Fatalf("group voting stream status=%d body=%s, want HTTP 200", response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("stream content type=%q, want text/event-stream", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: voting_snapshots") {
		t.Fatalf("initial group voting frame=%q, want voting_snapshots event", body)
	}
	if strings.Contains(body, "voter-a") || strings.Contains(body, "voter-b") {
		t.Fatalf("initial group voting stream leaked hidden UID: %s", body)
	}
	var streamSnapshot groupVotingSnapshotResponse
	for _, line := range strings.Split(body, "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if err := json.Unmarshal([]byte(data), &streamSnapshot); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if streamSnapshot.Cursor == "" {
		t.Fatal("group voting snapshot omitted cursor")
	}
	claims, err := endpoint.Cursors.Decode(streamSnapshot.Cursor)
	if err != nil || claims.Stream != "group-voting-v1" || claims.Resource != "public-group" || claims.Sequence != 4 {
		t.Fatalf("cursor claims=%+v err=%v, want group cursor at the oldest Meet sequence", claims, err)
	}
	if len(streamSnapshot.Meets) != 2 || streamSnapshot.Meets[0].MeetID != "meet-a" || streamSnapshot.Meets[1].MeetID != "meet-b" {
		t.Fatalf("group voting Meets=%+v, want meet-a and meet-b in order", streamSnapshot.Meets)
	}
	for _, meet := range streamSnapshot.Meets {
		httpResponse := httptest.NewRecorder()
		mux.ServeHTTP(httpResponse, authenticatedPastRequest("/api/meet/"+meet.MeetID+"/votes"))
		if httpResponse.Code != http.StatusOK {
			t.Fatalf("voting snapshot GET status=%d body=%s", httpResponse.Code, httpResponse.Body)
		}
		var httpSnapshot votingSnapshotResponse
		if err := json.Unmarshal(httpResponse.Body.Bytes(), &httpSnapshot); err != nil {
			t.Fatal(err)
		}
		streamJSON, err := json.Marshal(votingSnapshotResponse{Options: meet.Options, SelectedOptionIDs: meet.SelectedOptionIDs, VoteRevision: meet.VoteRevision})
		if err != nil {
			t.Fatal(err)
		}
		httpJSON, err := json.Marshal(httpSnapshot)
		if err != nil {
			t.Fatal(err)
		}
		if string(streamJSON) != string(httpJSON) {
			t.Fatalf("%s stream snapshot=%s, HTTP snapshot=%s", meet.MeetID, streamJSON, httpJSON)
		}
	}
}

func TestGroupVoteStreamRejectsInvalidMeetLists(t *testing.T) {
	endpoint := groupVotingStreamEndpoint(t)
	mux := http.NewServeMux()
	endpoint.Register(mux)
	tooMany := make([]string, 0, maxStreamedVotingMeets+1)
	for index := 0; index <= maxStreamedVotingMeets; index++ {
		tooMany = append(tooMany, "meet_id=meet-"+strconv.Itoa(index))
	}
	for _, test := range []struct {
		name  string
		query string
		want  int
	}{
		{name: "missing", query: "", want: http.StatusBadRequest},
		{name: "empty", query: "?meet_id=", want: http.StatusBadRequest},
		{name: "duplicate", query: "?meet_id=meet-a&meet_id=meet-a", want: http.StatusBadRequest},
		{name: "too many", query: "?" + strings.Join(tooMany, "&"), want: http.StatusBadRequest},
		{name: "unknown Meet", query: "?meet_id=meet-a&meet_id=missing", want: http.StatusNotFound},
		{name: "Meet in another Group", query: "?meet_id=meet-a&meet_id=other-meet", want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/meets/public-group/votes/stream"+test.query, nil)
			request.Header.Set("Authorization", "Bearer valid-token")
			response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
			mux.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s, want HTTP %d", response.Code, response.Body, test.want)
			}
			if strings.Contains(response.Body.String(), "event:") {
				t.Fatalf("rejected stream wrote an event: %s", response.Body)
			}
		})
	}
}

func TestVoteSnapshotIncludesOnlyVisibleProfiles(t *testing.T) {
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
			ID             string         `json:"id"`
			Total          int            `json:"total"`
			AnonymousCount int            `json:"anonymous_count"`
			Voters         []voterSummary `json:"voters"`
		} `json:"options"`
		SelectedOptionIDs []string `json:"selected_option_ids"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	venueOption := payload.Options[1]
	if venueOption.Total != 4 || venueOption.AnonymousCount != 2 || len(venueOption.Voters) != 2 ||
		venueOption.Voters[0].ID != "default-user" || venueOption.Voters[1].ID != "visible-user" ||
		venueOption.Voters[1].Name == nil || *venueOption.Voters[1].Name != "Name visible-user" {
		t.Fatalf("venue option=%+v, want four votes, two anonymous, and sorted visible summaries", venueOption)
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
