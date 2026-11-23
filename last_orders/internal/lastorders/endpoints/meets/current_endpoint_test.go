package meets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/currentmeetcache"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/services/meetwrite"
)

type currentReaderStub struct {
	snapshot   currentmeetcache.Snapshot
	renewCalls int
	date       string
	readErr    error
}

type openMeetsReaderStub struct {
	snapshot   currentmeetcache.Snapshot
	groupID    string
	renewCalls int
	readErr    error
}

func (r *openMeetsReaderStub) GetOpen(_ context.Context, groupID string) (currentmeetcache.Snapshot, error) {
	r.groupID = groupID
	return r.snapshot, r.readErr
}

func (r *openMeetsReaderStub) RenewOpen(context.Context, string) error {
	r.renewCalls++
	return nil
}

func (r *currentReaderStub) Get(context.Context, string) (currentmeetcache.Snapshot, error) {
	return r.snapshot, r.readErr
}

func (r *currentReaderStub) Renew(context.Context, string, string) error {
	r.renewCalls++
	return nil
}

func (r *currentReaderStub) CurrentDate() string { return r.date }

type pastMeetsReaderStub struct {
	today string
	list  func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error)
}

func (r *pastMeetsReaderStub) Today() string { return r.today }

func (r *pastMeetsReaderStub) List(ctx context.Context, groupID, beforeDate string, limit int, afterDate, afterID string) ([]meetcache.Projection, bool, error) {
	return r.list(ctx, groupID, beforeDate, limit, afterDate, afterID)
}

type venueReaderStub func(context.Context, string) (venuecache.VenueProjection, bool, error)

func (r venueReaderStub) GetForMeet(ctx context.Context, venueID string) (venuecache.VenueProjection, bool, error) {
	return r(ctx, venueID)
}

type pastMeetAuthStub struct{}

func (pastMeetAuthStub) Verify(_ context.Context, token string) (firebaseauth.Principal, error) {
	if token != "valid-token" {
		return firebaseauth.Principal{}, firebaseauth.ErrUnauthenticated
	}
	return firebaseauth.Principal{UID: "verified-user"}, nil
}

func authenticatedPastRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	return request
}

type groupAuthorizerStub func(context.Context, string, string) (bool, error)

func (a groupAuthorizerStub) CanRead(ctx context.Context, uid, groupID string) (bool, error) {
	return a(ctx, uid, groupID)
}

type writeAuthorizerStub func(context.Context, string, string) (bool, error)

func (a writeAuthorizerStub) CanWrite(ctx context.Context, uid, permission string) (bool, error) {
	return a(ctx, uid, permission)
}

type meetWriterStub struct {
	meet meetwrite.Meet
}

func (w meetWriterStub) Create(context.Context, string, string, string) (meetwrite.Meet, error) {
	return w.meet, nil
}

func (meetWriterStub) GroupID(context.Context, string) (string, error) {
	return "public-group", nil
}

func (w meetWriterStub) ChangeCandidate(context.Context, string, string, string, bool) (meetwrite.Meet, error) {
	return w.meet, nil
}

func (w meetWriterStub) Complete(context.Context, string, string, string, string, string) (meetwrite.Meet, error) {
	return w.meet, nil
}

func (w meetWriterStub) Reschedule(context.Context, string, meetwrite.RescheduleInput, string) (meetwrite.Meet, error) {
	return w.meet, nil
}

func (meetWriterStub) Delete(context.Context, string, string) error { return nil }

func (meetWriterStub) ChangeVote(context.Context, string, string, string, bool) (meetwrite.VoteResult, error) {
	return meetwrite.VoteResult{}, nil
}

func TestCompleteMeetHTTPReturnsSelectedVenue(t *testing.T) {
	endpoint := &Endpoint{
		Writer: meetWriterStub{meet: meetwrite.Meet{
			ID: "meet-1", GroupID: "public-group", Date: "2026-11-18", VotingComplete: true,
			SelectedVenueID: "venue-1", SelectedVenue: &meetwrite.VenueSummary{
				ID: "venue-1", Status: "active", Name: "The Crown", VenueType: "pub",
			}, CandidateVenueIDs: []string{}, PreviousVenueIDs: []string{},
		}},
		Groups:      groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		WriteAccess: writeAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Auth:        pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/meet/meet-1/complete", strings.NewReader(`{"venue_id":"venue-1"}`))
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	wantBody := `{"id":"meet-1","group_id":"public-group","date":"2026-11-18","voting_complete":true,"venue":{"id":"venue-1","status":"active","name":"The Crown","address":"","website_url":null,"image_url":null,"venue_type":"pub"},"candidate_venue_ids":[],"restaurant_id":null,"restaurant_time":null,"previous_venue_ids":[]}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != wantBody {
		t.Fatalf("completion status=%d body=%s, want HTTP 200 and %s", response.Code, response.Body, wantBody)
	}
}

func TestCompleteMeetHTTPRejectsUnresolvedSelectedVenue(t *testing.T) {
	for _, test := range []struct {
		name            string
		selectedVenueID string
		venue           *meetwrite.VenueSummary
	}{
		{name: "mismatched ID", selectedVenueID: "venue-1", venue: &meetwrite.VenueSummary{ID: "venue-other", Status: "active", Name: "The Other Crown"}},
		{name: "unresolved selected ID", selectedVenueID: "venue-1"},
		{name: "completed without selection"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := &Endpoint{
				Writer: meetWriterStub{meet: meetwrite.Meet{
					ID: "meet-1", GroupID: "public-group", Date: "2026-11-18", VotingComplete: true,
					SelectedVenueID: test.selectedVenueID, SelectedVenue: test.venue,
				}},
				Groups:      groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
				WriteAccess: writeAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
				Auth:        pastMeetAuthStub{},
			}
			mux := http.NewServeMux()
			endpoint.Register(mux)
			request := httptest.NewRequest(http.MethodPost, "/api/meet/meet-1/complete", strings.NewReader(`{"venue_id":"venue-1"}`))
			request.Header.Set("Authorization", "Bearer valid-token")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), `"venue":null`) {
				t.Fatalf("completion status=%d body=%s, want a non-success response without a null Venue", response.Code, response.Body)
			}
		})
	}
}

func TestRescheduleMeetHTTPReturnsConfirmedMissingVenue(t *testing.T) {
	endpoint := &Endpoint{
		Writer: meetWriterStub{meet: meetwrite.Meet{
			ID: "meet-1", GroupID: "public-group", Date: "2026-11-18", VotingComplete: true,
			SelectedVenueID: "venue-1", SelectedVenueMissing: true,
			RestaurantID: "restaurant-1", RestaurantTime: "19:15",
			CandidateVenueIDs: []string{}, PreviousVenueIDs: []string{},
		}},
		Groups:      groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		WriteAccess: writeAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Auth:        pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/meet/meet-1/reschedule", strings.NewReader(`{"restaurant_time":"19:15"}`))
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	wantBody := `{"id":"meet-1","group_id":"public-group","date":"2026-11-18","voting_complete":true,"venue":{"id":"venue-1","status":"missing"},"candidate_venue_ids":[],"restaurant_id":"restaurant-1","restaurant_time":"19:15","previous_venue_ids":[]}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != wantBody {
		t.Fatalf("reschedule status=%d body=%s, want HTTP 200 and %s", response.Code, response.Body, wantBody)
	}
}

type flushableRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func (w *flushableRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *flushableRecorder) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() { close(w.flushed) })
}

func TestRegisterCurrentMeetsGetUnavailableWithoutProjection(t *testing.T) {
	mux := http.NewServeMux()
	(&Endpoint{Auth: pastMeetAuthStub{}}).Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current"))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("current-Meets GET status=%d body=%s, want HTTP 503", response.Code, response.Body)
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

func TestAnonymousCanReadPublicGroupMeetCollections(t *testing.T) {
	endpoint := &Endpoint{
		Current: &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
			Date: "2026-11-20", Generation: "generation-1",
		}},
		Open: &openMeetsReaderStub{snapshot: currentmeetcache.Snapshot{Generation: "generation-1"}},
		Past: &pastMeetsReaderStub{today: "2026-11-20", list: func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error) {
			return nil, false, nil
		}},
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return groupID == "public-group" && (uid == "" || uid == "verified-user"), nil
		}),
		Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	for _, path := range []string{
		"/api/meets/public-group/open",
		"/api/meets/public-group/current",
		"/api/meets/public-group/past",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("anonymous GET %s status=%d body=%s, want HTTP 200", path, response.Code, response.Body)
		}
	}

	for _, path := range []string{
		"/api/meets/private-group/open",
		"/api/meets/private-group/current",
		"/api/meets/private-group/past",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("anonymous GET %s status=%d body=%s, want HTTP 404", path, response.Code, response.Body)
		}
	}

	invalidToken := httptest.NewRequest(http.MethodGet, "/api/meets/public-group/current", nil)
	invalidToken.Header.Set("Authorization", "Bearer invalid-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, invalidToken)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid-token current-Meets GET status=%d body=%s, want HTTP 401", response.Code, response.Body)
	}
}

func TestGroupScopedMeetReadFailsClosedWhenAuthorizationIsUnavailable(t *testing.T) {
	endpoint := &Endpoint{
		Current: &currentReaderStub{date: "2026-11-20"},
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) {
			return false, errors.New("Firestore unavailable")
		}),
		Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current"))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"authorization_unavailable"`) {
		t.Fatalf("authorization source failure status=%d body=%s, want HTTP 503 authorization_unavailable", response.Code, response.Body)
	}
}

func TestRegisterPastMeetsGetUnavailableWithoutQuery(t *testing.T) {
	mux := http.NewServeMux()
	(&Endpoint{Auth: pastMeetAuthStub{}}).Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/past"))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("past-Meets GET status=%d body=%s, want HTTP 503", response.Code, response.Body)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "past_meets_unavailable" {
		t.Fatalf("error code=%q, want past_meets_unavailable", payload.Error.Code)
	}
}

func TestPastMeetsGetReturnsOpaquePageAndSelectedVenueSummaries(t *testing.T) {
	codec := testCurrentCursorCodec(t)
	var gotGroupID, gotBeforeDate, gotAfterDate, gotAfterID string
	var gotLimit int
	list := func(_ context.Context, groupID, beforeDate string, limit int, afterDate, afterID string) ([]meetcache.Projection, bool, error) {
		gotGroupID, gotBeforeDate, gotLimit = groupID, beforeDate, limit
		gotAfterDate, gotAfterID = afterDate, afterID
		return []meetcache.Projection{
			{ID: "meet-b", GroupID: "group-a", Date: "2026-11-18", VotingComplete: true, SelectedVenueID: "deleted-venue"},
			{ID: "meet-a", GroupID: "group-a", Date: "2026-11-17", VotingComplete: true, SelectedVenueID: "missing-venue"},
		}, true, nil
	}
	endpoint := &Endpoint{
		Past: &pastMeetsReaderStub{today: "2026-11-20", list: list},
		Venues: venueReaderStub(func(_ context.Context, venueID string) (venuecache.VenueProjection, bool, error) {
			if venueID == "missing-venue" {
				return venuecache.VenueProjection{}, true, nil
			}
			return venuecache.VenueProjection{
				ID: venueID, Name: "Old Pub", Address: "Old Street", Website: "https://pub.example",
				PhotoURL: "https://pub.example/photo.jpg", VenueType: "pub", DeletedOn: "2026-11-18T00:00:00Z",
			}, false, nil
		}),
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "group-a", nil
		}),
		Cursors: codec, Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/past?limit=2"))
	if response.Code != http.StatusOK {
		t.Fatalf("past-Meets GET status=%d body=%s", response.Code, response.Body)
	}
	var payload struct {
		Meets []struct {
			ID    string          `json:"id"`
			Date  string          `json:"date"`
			Venue json.RawMessage `json:"venue"`
		} `json:"meets"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if gotGroupID != "group-a" || gotBeforeDate != "2026-11-20" || gotLimit != 2 || gotAfterDate != "" || gotAfterID != "" {
		t.Fatalf("past query args group=%q before=%q limit=%d after=(%q,%q)", gotGroupID, gotBeforeDate, gotLimit, gotAfterDate, gotAfterID)
	}
	if len(payload.Meets) != 2 || payload.Meets[0].ID != "meet-b" || payload.Meets[1].ID != "meet-a" || payload.NextCursor == nil {
		t.Fatalf("past-Meets page=%+v", payload)
	}
	var selectedVenue struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(payload.Meets[0].Venue, &selectedVenue); err != nil {
		t.Fatal(err)
	}
	if selectedVenue.ID != "deleted-venue" || selectedVenue.Status != "deleted" || selectedVenue.Name != "Old Pub" {
		t.Fatalf("deleted Venue summary=%+v", selectedVenue)
	}
	var missingVenue struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(payload.Meets[1].Venue, &missingVenue); err != nil {
		t.Fatal(err)
	}
	if missingVenue.ID != "missing-venue" || missingVenue.Status != "missing" {
		t.Fatalf("missing Venue reference=%+v", missingVenue)
	}
	claims, err := codec.Validate(*payload.NextCursor, sse.CursorExpectation{
		Stream: "past-meets-v1", Resource: "group-a", Generation: "past-meets-v1", HighWater: 0, Date: "2026-11-20",
	})
	if err != nil {
		t.Fatalf("validate next cursor: %v", err)
	}
	if claims.Position == "" || strings.Contains(response.Body.String(), "votes") || strings.Contains(response.Body.String(), "attendance") {
		t.Fatalf("cursor or Meet response is invalid: %s", response.Body)
	}
	secondResponse := httptest.NewRecorder()
	secondRequest := authenticatedPastRequest("/api/meets/group-a/past?limit=2&before=" + *payload.NextCursor)
	mux.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusOK || gotAfterDate != "2026-11-17" || gotAfterID != "meet-a" {
		t.Fatalf("second page status=%d position=(%q,%q) body=%s", secondResponse.Code, gotAfterDate, gotAfterID, secondResponse.Body)
	}
	var secondPayload pastMeetsResponse
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if secondPayload.NextCursor == nil || *secondPayload.NextCursor != *payload.NextCursor {
		t.Fatalf("cursor for stable page position changed: first=%q second=%v", *payload.NextCursor, secondPayload.NextCursor)
	}
}

func TestPastMeetsRejectsInvalidCursorBeforeQuery(t *testing.T) {
	listCalls := 0
	endpoint := &Endpoint{
		Past: &pastMeetsReaderStub{
			today: "2026-11-20",
			list: func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error) {
				listCalls++
				return nil, false, nil
			},
		},
		Groups:  groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/past?before=invalid"))
	if response.Code != http.StatusBadRequest || listCalls != 0 {
		t.Fatalf("invalid cursor status=%d list calls=%d body=%s", response.Code, listCalls, response.Body)
	}
}

func TestPastMeetsRejectsCursorPositionOutsideLondonDate(t *testing.T) {
	codec := testCurrentCursorCodec(t)
	position, err := json.Marshal(pastMeetPosition{Date: "2026-11-20", ID: "meet-today"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.Encode(sse.CursorClaims{
		Stream: pastMeetsCursorScope, Resource: "group-a", Generation: pastMeetsCursorScope,
		Date: "2026-11-20", Position: base64.RawURLEncoding.EncodeToString(position),
	})
	if err != nil {
		t.Fatal(err)
	}
	listCalls := 0
	endpoint := &Endpoint{
		Past: &pastMeetsReaderStub{
			today: "2026-11-20",
			list: func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error) {
				listCalls++
				return nil, false, nil
			},
		},
		Groups:  groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Cursors: codec, Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/past?before="+token))
	if response.Code != http.StatusBadRequest || listCalls != 0 {
		t.Fatalf("today cursor position status=%d list calls=%d body=%s", response.Code, listCalls, response.Body)
	}
}

func TestPastMeetsDoesNotQueryUnauthorizedGroup(t *testing.T) {
	listCalls := 0
	endpoint := &Endpoint{
		Past: &pastMeetsReaderStub{
			today: "2026-11-20",
			list: func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error) {
				listCalls++
				return nil, false, nil
			},
		},
		Groups:  groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return false, nil }),
		Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/private-group/past"))
	if response.Code != http.StatusNotFound || listCalls != 0 {
		t.Fatalf("denied past-Meets status=%d list calls=%d body=%s", response.Code, listCalls, response.Body)
	}
}

func TestPastMeetsDoesNotTreatTransientSourceFailuresAsMissing(t *testing.T) {
	transientErr := errors.New("temporary source failure")
	for _, test := range []struct {
		name   string
		past   pastMeetsReader
		venues venueReader
	}{
		{
			name: "Meet query",
			past: &pastMeetsReaderStub{today: "2026-11-20", list: func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error) {
				return nil, false, transientErr
			}},
		},
		{
			name: "Venue lookup",
			past: &pastMeetsReaderStub{today: "2026-11-20", list: func(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error) {
				return []meetcache.Projection{{ID: "meet-1", GroupID: "group-a", Date: "2026-11-19", SelectedVenueID: "venue-1"}}, false, nil
			}},
			venues: venueReaderStub(func(context.Context, string) (venuecache.VenueProjection, bool, error) {
				return venuecache.VenueProjection{}, false, transientErr
			}),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := &Endpoint{
				Past: test.past, Venues: test.venues,
				Groups:  groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
				Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
			}
			mux := http.NewServeMux()
			endpoint.Register(mux)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/past"))
			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), `"status":"missing"`) {
				t.Fatalf("transient error status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}

func TestCurrentMeetsGetReturnsCompletePublicSnapshot(t *testing.T) {
	reader := &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
		Date: "2026-11-20", Generation: "generation-1", Sequence: 9,
		Meets: []currentmeetcache.MeetView{
			{
				Meet:  meetcache.Projection{ID: "meet-1", GroupID: "group-a", Date: "2026-11-20", VotingComplete: true, CandidateVenueIDs: []string{"venue-1"}},
				Venue: &currentmeetcache.VenueSummary{ID: "venue-1", Status: "deleted", Name: "Old Pub", Address: "Old Street", VenueType: "pub"},
			},
			{Meet: meetcache.Projection{ID: "meet-2", GroupID: "group-a", Date: "2026-11-21", VotingComplete: true, CandidateVenueIDs: []string{}}, MissingVenueID: "missing-venue"},
		},
	}}
	endpoint := &Endpoint{
		Current: reader, Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "group-a", nil
		}),
		Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current"))
	if response.Code != http.StatusOK {
		t.Fatalf("current-Meets GET status=%d body=%s", response.Code, response.Body)
	}
	var payload struct {
		Cursor string `json:"cursor"`
		Meets  []struct {
			ID    string `json:"id"`
			Venue struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"venue"`
		} `json:"meets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Cursor == "" || len(payload.Meets) != 2 || payload.Meets[0].ID != "meet-1" || payload.Meets[0].Venue.Status != "deleted" || payload.Meets[1].Venue.ID != "missing-venue" || payload.Meets[1].Venue.Status != "missing" {
		t.Fatalf("current-Meets response=%+v", payload)
	}
	if strings.Contains(response.Body.String(), "attendance") || strings.Contains(response.Body.String(), "votes") {
		t.Fatalf("response contains voting or attendance data: %s", response.Body)
	}
}

func TestOpenMeetsGetReturnsCompleteGroupSnapshotIncludingPastOpenMeets(t *testing.T) {
	reader := &openMeetsReaderStub{snapshot: currentmeetcache.Snapshot{
		Generation: "generation-1", Sequence: 9,
		Meets: []currentmeetcache.MeetView{{
			Meet: meetcache.Projection{
				ID: "meet-overdue", GroupID: "group-a", Date: "2020-01-01",
				CandidateVenueIDs: []string{"venue-1"},
			},
		}},
	}}
	endpoint := &Endpoint{
		Open: reader, Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "group-a", nil
		}),
		Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/open"))
	if response.Code != http.StatusOK {
		t.Fatalf("open-Meets GET status=%d body=%s", response.Code, response.Body)
	}
	var payload struct {
		Cursor string `json:"cursor"`
		Meets  []struct {
			ID                string   `json:"id"`
			Date              string   `json:"date"`
			VotingComplete    bool     `json:"voting_complete"`
			CandidateVenueIDs []string `json:"candidate_venue_ids"`
		} `json:"meets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if reader.groupID != "group-a" || payload.Cursor == "" || len(payload.Meets) != 1 ||
		payload.Meets[0].ID != "meet-overdue" || payload.Meets[0].Date != "2020-01-01" ||
		payload.Meets[0].VotingComplete || len(payload.Meets[0].CandidateVenueIDs) != 1 ||
		payload.Meets[0].CandidateVenueIDs[0] != "venue-1" {
		t.Fatalf("open-Meets response group=%q payload=%+v", reader.groupID, payload)
	}
	if strings.Contains(response.Body.String(), "attendance") || strings.Contains(response.Body.String(), "votes") {
		t.Fatalf("response contains voting or attendance data: %s", response.Body)
	}
}

func TestOpenMeetsStreamSendsCompleteSnapshotAndRenews(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &openMeetsReaderStub{snapshot: currentmeetcache.Snapshot{
		Generation: "generation-1", Sequence: 9,
		Meets: []currentmeetcache.MeetView{{
			Meet: meetcache.Projection{ID: "overdue-open", GroupID: "group-a", Date: "2020-01-01"},
		}},
	}}
	endpoint := &Endpoint{
		Open: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "" && groupID == "group-a", nil
		}),
		Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/meets/group-a/open/stream?after=invalid", nil).WithContext(ctx)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-response.flushed:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("open-Meets stream did not flush its initial snapshot")
	}
	cancel()
	<-done
	body := response.Body.String()
	if !strings.Contains(body, "event: open_meets") || !strings.Contains(body, `"id":"overdue-open"`) || !strings.Contains(body, "id: v1.") {
		t.Fatalf("open-Meets stream did not send a complete snapshot and cursor: %s", body)
	}
	if reader.renewCalls != 1 {
		t.Fatalf("successful open-Meets stream renewed %d times, want once", reader.renewCalls)
	}
}

func TestCurrentMeetsStreamSendsReplacementForInvalidCursorAndRenews(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
		Date: "2026-11-20", Generation: "generation-1", Sequence: 9,
	}}
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "" && groupID == "group-a", nil
		}),
		Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/meets/group-a/current/stream?after=invalid", nil).WithContext(ctx)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-response.flushed:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("current-Meets stream did not flush its initial snapshot")
	}
	cancel()
	<-done
	body := response.Body.String()
	if !strings.Contains(body, "event: current_meets") || !strings.Contains(body, `"meets":[]`) || !strings.Contains(body, "id: v1.") {
		t.Fatalf("stream did not send a complete snapshot and cursor: %s", body)
	}
	if reader.renewCalls != 1 {
		t.Fatalf("successful stream renewed %d times, want once", reader.renewCalls)
	}
}

func TestDeniedCurrentMeetsStreamDoesNotRenew(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20"}
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return false, nil }),
		Auth:   pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current/stream"))
	if response.Code != http.StatusNotFound || reader.renewCalls != 0 {
		t.Fatalf("denied stream status=%d renewals=%d, want HTTP 404 and no renewal", response.Code, reader.renewCalls)
	}
}

func TestCurrentMeetsStreamReauthorizesBeforeRenewal(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
		Date: "2026-11-20", Generation: "generation-1", Sequence: 9,
	}}
	authorizationChecks := 0
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) {
			authorizationChecks++
			return authorizationChecks == 1, nil
		}),
		Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current/stream"))
	if authorizationChecks != 2 || response.Code != http.StatusNotFound || reader.renewCalls != 0 {
		t.Fatalf("checks=%d status=%d renewals=%d, want second-check denial without renewal", authorizationChecks, response.Code, reader.renewCalls)
	}
}

func TestCurrentMeetsStreamReturnsUnavailableBeforeBaseline(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	reader := &currentReaderStub{date: "2026-11-20", readErr: currentmeetcache.ErrUnavailable}
	endpoint := &Endpoint{
		Current: reader, CurrentReady: ready, CurrentHub: sse.NewHub(), Cursors: testCurrentCursorCodec(t),
		Groups: groupAuthorizerStub(func(context.Context, string, string) (bool, error) { return true, nil }),
		Auth:   pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	response := &flushableRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current/stream"))
	if response.Code != http.StatusServiceUnavailable || reader.renewCalls != 0 {
		t.Fatalf("unavailable stream status=%d renewals=%d, want HTTP 503 without renewal", response.Code, reader.renewCalls)
	}
}

func testCurrentCursorCodec(t *testing.T) *sse.CursorCodec {
	t.Helper()
	codec, err := sse.NewCursorCodec([]byte("test-only-current-meet-cursor-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}
