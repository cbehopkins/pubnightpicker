package meets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"last_orders/internal/lastorders/components/currentmeetcache"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/services/meetwrite"
)

type reader interface {
	Get(context.Context, string) (meetcache.Snapshot, error)
}

type GroupAuthorizer interface {
	CanRead(context.Context, string, string) (bool, error)
}

type venueReader interface {
	GetForMeet(context.Context, string) (venuecache.VenueProjection, bool, error)
}

type currentReader interface {
	Get(context.Context, string) (currentmeetcache.Snapshot, error)
	Renew(context.Context, string, string) error
	CurrentDate() string
}

type openMeetsReader interface {
	GetOpen(context.Context, string) (currentmeetcache.Snapshot, error)
	RenewOpen(context.Context, string) error
}

type pastMeetsReader interface {
	Today() string
	List(context.Context, string, string, int, string, string) ([]meetcache.Projection, bool, error)
}

type MeetWriter interface {
	Create(context.Context, string, string, string) (meetwrite.Meet, error)
	GroupID(context.Context, string) (string, error)
	ChangeCandidate(context.Context, string, string, string, bool) (meetwrite.Meet, error)
	Complete(context.Context, string, string, string, string, string) (meetwrite.Meet, error)
	Reschedule(context.Context, string, meetwrite.RescheduleInput, string) (meetwrite.Meet, error)
	Delete(context.Context, string, string) error
}

type Endpoint struct {
	Reader       reader
	Venues       venueReader
	Groups       GroupAuthorizer
	Auth         firebaseauth.Verifier
	Current      currentReader
	Open         openMeetsReader
	CurrentReady <-chan struct{}
	CurrentHub   *sse.Hub
	Cursors      *sse.CursorCodec
	Past         pastMeetsReader
	Writer       MeetWriter
	WriteAccess  WriteAuthorizer
}

type venueSummary struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	WebsiteURL *string `json:"website_url"`
	ImageURL   *string `json:"image_url"`
	VenueType  string  `json:"venue_type"`
}

type missingVenueReference struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type meetResponse struct {
	ID                string   `json:"id"`
	GroupID           string   `json:"group_id"`
	Date              string   `json:"date"`
	VotingComplete    bool     `json:"voting_complete"`
	Venue             any      `json:"venue"`
	CandidateVenueIDs []string `json:"candidate_venue_ids"`
	RestaurantID      *string  `json:"restaurant_id"`
	RestaurantTime    *string  `json:"restaurant_time"`
	PreviousVenueIDs  []string `json:"previous_venue_ids"`
}

type currentMeetsResponse struct {
	Cursor string         `json:"cursor"`
	Meets  []meetResponse `json:"meets"`
}

type pastMeetsResponse struct {
	Meets      []pastMeetResponse `json:"meets"`
	NextCursor *string            `json:"next_cursor"`
}

type pastMeetResponse struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	Venue any    `json:"venue"`
}

type pastMeetPosition struct {
	Date string `json:"date"`
	ID   string `json:"id"`
}

const pastMeetsCursorScope = "past-meets-v1"

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/meets/{group_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.create)))
	mux.Handle("PUT /api/meet/{meet_id}/candidates/{venue_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.addCandidate)))
	mux.Handle("DELETE /api/meet/{meet_id}/candidates/{venue_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.removeCandidate)))
	mux.Handle("POST /api/meet/{meet_id}/complete", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.complete)))
	mux.Handle("POST /api/meet/{meet_id}/reschedule", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.reschedule)))
	mux.Handle("DELETE /api/meet/{meet_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.delete)))
	mux.Handle("GET /api/meet/{meet_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.get)))
	mux.Handle("GET /api/meets/{group_id}/current", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.current)))
	mux.Handle("GET /api/meets/{group_id}/current/stream", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.currentStream)))
	mux.Handle("GET /api/meets/{group_id}/open", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.open)))
	mux.Handle("GET /api/meets/{group_id}/open/stream", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.openStream)))
	mux.Handle("GET /api/meets/{group_id}/past", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.past)))
}

func (e *Endpoint) delete(w http.ResponseWriter, r *http.Request) {
	if e.Writer == nil || e.Groups == nil || e.WriteAccess == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "meet_write_unavailable", "Meet writes are temporarily unavailable.")
		return
	}
	meetID := r.PathValue("meet_id")
	groupID, err := e.Writer.GroupID(r.Context(), meetID)
	if e.writeServiceError(w, err) {
		return
	}
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	allowed, err := e.WriteAccess.CanWrite(r.Context(), currentUID(r.Context()), "canCreatePoll")
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You are not allowed to delete Meets.")
		return
	}
	if err := e.Writer.Delete(r.Context(), meetID, currentUID(r.Context())); err != nil {
		e.writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (e *Endpoint) reschedule(w http.ResponseWriter, r *http.Request) {
	if e.Writer == nil || e.Groups == nil || e.WriteAccess == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "meet_write_unavailable", "Meet writes are temporarily unavailable.")
		return
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The reschedule request is invalid.")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The reschedule request is invalid.")
		return
	}
	if _, hasDate := fields["date"]; hasDate {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "A Meet date cannot be changed by rescheduling.")
		return
	}
	for key := range fields {
		if key != "venue_id" && key != "restaurant_id" && key != "restaurant_time" {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The reschedule request is invalid.")
			return
		}
	}
	input := meetwrite.RescheduleInput{}
	var valid bool
	if input.VenueIDSet, input.VenueID, valid = decodeOptionalString(fields, "venue_id"); !valid {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The reschedule request is invalid.")
		return
	}
	if input.RestaurantIDSet, input.RestaurantID, valid = decodeOptionalString(fields, "restaurant_id"); !valid {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The reschedule request is invalid.")
		return
	}
	if input.RestaurantTimeSet, input.RestaurantTime, valid = decodeOptionalString(fields, "restaurant_time"); !valid {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The reschedule request is invalid.")
		return
	}
	meetID := r.PathValue("meet_id")
	groupID, err := e.Writer.GroupID(r.Context(), meetID)
	if e.writeServiceError(w, err) {
		return
	}
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	allowed, err := e.WriteAccess.CanWrite(r.Context(), currentUID(r.Context()), "canCompletePoll")
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You are not allowed to reschedule Meets.")
		return
	}
	updated, err := e.Writer.Reschedule(r.Context(), meetID, input, currentUID(r.Context()))
	if e.writeServiceError(w, err) {
		return
	}
	e.writeMeet(w, http.StatusOK, updated)
}

func decodeOptionalString(fields map[string]json.RawMessage, key string) (bool, *string, bool) {
	value, exists := fields[key]
	if !exists {
		return false, nil, true
	}
	if string(value) == "null" {
		return true, nil, true
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return true, nil, false
	}
	return true, &decoded, true
}

func (e *Endpoint) complete(w http.ResponseWriter, r *http.Request) {
	if e.Writer == nil || e.Groups == nil || e.WriteAccess == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "meet_write_unavailable", "Meet writes are temporarily unavailable.")
		return
	}
	var request struct {
		VenueID        string `json:"venue_id"`
		RestaurantID   string `json:"restaurant_id"`
		RestaurantTime string `json:"restaurant_time"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The completion request is invalid.")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The completion request is invalid.")
		return
	}
	meetID := r.PathValue("meet_id")
	groupID, err := e.Writer.GroupID(r.Context(), meetID)
	if e.writeServiceError(w, err) {
		return
	}
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	allowed, err := e.WriteAccess.CanWrite(r.Context(), currentUID(r.Context()), "canCompletePoll")
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You are not allowed to complete Meets.")
		return
	}
	completed, err := e.Writer.Complete(r.Context(), meetID, request.VenueID, request.RestaurantID, request.RestaurantTime, currentUID(r.Context()))
	if e.writeServiceError(w, err) {
		return
	}
	e.writeMeet(w, http.StatusOK, completed)
}

func (e *Endpoint) addCandidate(w http.ResponseWriter, r *http.Request) {
	e.changeCandidate(w, r, true)
}

func (e *Endpoint) removeCandidate(w http.ResponseWriter, r *http.Request) {
	e.changeCandidate(w, r, false)
}

func (e *Endpoint) changeCandidate(w http.ResponseWriter, r *http.Request, add bool) {
	if e.Writer == nil || e.Groups == nil || e.WriteAccess == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "meet_write_unavailable", "Meet writes are temporarily unavailable.")
		return
	}
	meetID, venueID := r.PathValue("meet_id"), r.PathValue("venue_id")
	groupID, err := e.Writer.GroupID(r.Context(), meetID)
	if e.writeServiceError(w, err) {
		return
	}
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	allowed, err := e.WriteAccess.CanWrite(r.Context(), currentUID(r.Context()), "canAddPubToPoll")
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You are not allowed to change Meet candidates.")
		return
	}
	updated, err := e.Writer.ChangeCandidate(r.Context(), meetID, venueID, currentUID(r.Context()), add)
	if e.writeServiceError(w, err) {
		return
	}
	e.writeMeet(w, http.StatusOK, updated)
}

func (e *Endpoint) writeServiceError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, meetwrite.ErrInvalidInput):
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The Meet request is invalid.")
	case errors.Is(err, meetwrite.ErrGroupNotFound), errors.Is(err, meetwrite.ErrMeetNotFound), errors.Is(err, meetwrite.ErrVenueNotFound):
		firebaseauth.WriteError(w, http.StatusNotFound, "not_found", "The requested resource was not found.")
	case errors.Is(err, meetwrite.ErrInvalidState):
		firebaseauth.WriteError(w, http.StatusConflict, "invalid_state", "The Meet is not in a state that allows this operation.")
	default:
		firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to update the Meet.")
	}
	return true
}

func (e *Endpoint) writeMeet(w http.ResponseWriter, statusCode int, meet meetwrite.Meet) {
	response := meetResponse{
		ID: meet.ID, GroupID: meet.GroupID, Date: meet.Date,
		VotingComplete: meet.VotingComplete, CandidateVenueIDs: meet.CandidateVenueIDs,
		Venue: nil, RestaurantID: optionalValue(meet.RestaurantID),
		RestaurantTime: optionalValue(meet.RestaurantTime), PreviousVenueIDs: meet.PreviousVenueIDs,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(response)
}

func (e *Endpoint) create(w http.ResponseWriter, r *http.Request) {
	if e.Writer == nil || e.Groups == nil || e.WriteAccess == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "meet_write_unavailable", "Meet writes are temporarily unavailable.")
		return
	}
	var request struct {
		Date string `json:"date"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The Meet request is invalid.")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The Meet request is invalid.")
		return
	}
	groupID := r.PathValue("group_id")
	if groupID == "" {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "A Group ID is required.")
		return
	}
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	allowed, err := e.WriteAccess.CanWrite(r.Context(), currentUID(r.Context()), "canCreatePoll")
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You are not allowed to create Meets.")
		return
	}
	created, err := e.Writer.Create(r.Context(), groupID, request.Date, currentUID(r.Context()))
	switch {
	case errors.Is(err, meetwrite.ErrInvalidInput):
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The Meet date is invalid.")
		return
	case errors.Is(err, meetwrite.ErrGroupNotFound):
		firebaseauth.WriteError(w, http.StatusNotFound, "group_not_found", "The requested Group was not found.")
		return
	case err != nil:
		firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to create the Meet.")
		return
	}
	response := meetResponse{
		ID: created.ID, GroupID: created.GroupID, Date: created.Date,
		CandidateVenueIDs: []string{}, PreviousVenueIDs: []string{},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(response)
}

func (e *Endpoint) past(w http.ResponseWriter, r *http.Request) {
	if e.Past == nil || e.Groups == nil || e.Cursors == nil {
		writePastUnavailable(w)
		return
	}
	groupID := r.PathValue("group_id")
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	today := e.Past.Today()
	limit, err := parsePastMeetsLimit(r)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var afterDate, afterID string
	if values, supplied := r.URL.Query()["before"]; supplied {
		if len(values) != 1 || values[0] == "" {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_cursor", "The past-Meets cursor is invalid.")
			return
		}
		claims, err := e.Cursors.Validate(values[0], sse.CursorExpectation{
			Stream: pastMeetsCursorScope, Resource: groupID, Generation: pastMeetsCursorScope,
			HighWater: 0, Date: today,
		})
		if err != nil {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_cursor", "The past-Meets cursor is invalid.")
			return
		}
		position, err := decodePastMeetPosition(claims.Position)
		if err != nil {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_cursor", "The past-Meets cursor is invalid.")
			return
		}
		if position.Date >= today {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_cursor", "The past-Meets cursor is invalid.")
			return
		}
		afterDate, afterID = position.Date, position.ID
	}
	meets, hasNext, err := e.Past.List(r.Context(), groupID, today, limit, afterDate, afterID)
	if err != nil {
		writePastUnavailable(w)
		return
	}
	if len(meets) > limit {
		writePastUnavailable(w)
		return
	}
	response := pastMeetsResponse{Meets: make([]pastMeetResponse, 0, len(meets))}
	for _, meet := range meets {
		selectedVenue, err := e.selectedVenueResponse(r.Context(), meet.SelectedVenueID)
		if err != nil {
			writePastVenueUnavailable(w)
			return
		}
		response.Meets = append(response.Meets, pastMeetResponse{ID: meet.ID, Date: meet.Date, Venue: selectedVenue})
	}
	if hasNext && len(meets) > 0 {
		position, err := json.Marshal(pastMeetPosition{Date: meets[len(meets)-1].Date, ID: meets[len(meets)-1].ID})
		if err != nil {
			firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to create the past-Meets cursor.")
			return
		}
		claims := sse.CursorClaims{
			Stream: pastMeetsCursorScope, Resource: groupID, Generation: pastMeetsCursorScope,
			Date: today, Position: base64.RawURLEncoding.EncodeToString(position),
		}
		token, err := e.Cursors.Encode(claims)
		if err != nil {
			firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to create the past-Meets cursor.")
			return
		}
		response.NextCursor = &token
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func parsePastMeetsLimit(r *http.Request) (int, error) {
	values, supplied := r.URL.Query()["limit"]
	if !supplied {
		return 20, nil
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("limit must be supplied once")
	}
	limit, err := strconv.Atoi(values[0])
	if err != nil || limit < 1 || limit > 100 {
		return 0, fmt.Errorf("limit must be between 1 and 100")
	}
	return limit, nil
}

func decodePastMeetPosition(encoded string) (pastMeetPosition, error) {
	data, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > 512 {
		return pastMeetPosition{}, sse.ErrInvalidCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var position pastMeetPosition
	if err := decoder.Decode(&position); err != nil || position.ID == "" {
		return pastMeetPosition{}, sse.ErrInvalidCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return pastMeetPosition{}, sse.ErrInvalidCursor
	}
	date, err := time.Parse(time.DateOnly, position.Date)
	if err != nil || date.Format(time.DateOnly) != position.Date {
		return pastMeetPosition{}, sse.ErrInvalidCursor
	}
	return position, nil
}

func (e *Endpoint) selectedVenueResponse(ctx context.Context, venueID string) (any, error) {
	if venueID == "" {
		return nil, nil
	}
	if e.Venues == nil {
		return nil, errors.New("Venue reader is unavailable")
	}
	venue, missing, err := e.Venues.GetForMeet(ctx, venueID)
	if err != nil {
		return nil, err
	}
	if missing {
		return missingVenueReference{ID: venueID, Status: "missing"}, nil
	}
	venueType := venue.VenueType
	if venueType == "" {
		venueType = "pub"
	}
	status := "active"
	if venue.DeletedOn != "" {
		status = "deleted"
	}
	return venueSummary{
		ID: venue.ID, Status: status, Name: venue.Name, Address: venue.Address,
		WebsiteURL: optionalValue(venue.Website), ImageURL: optionalValue(venue.PhotoURL), VenueType: venueType,
	}, nil
}

func (e *Endpoint) current(w http.ResponseWriter, r *http.Request) {
	if e.Current == nil || e.Groups == nil || e.Cursors == nil {
		writeCurrentUnavailable(w)
		return
	}
	groupID := r.PathValue("group_id")
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	snapshot, err := e.Current.Get(r.Context(), groupID)
	if err != nil {
		writeCurrentReadError(w, err)
		return
	}
	response, err := e.currentResponse(groupID, snapshot)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to encode the current-Meets snapshot.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func (e *Endpoint) open(w http.ResponseWriter, r *http.Request) {
	if e.Open == nil || e.Groups == nil || e.Cursors == nil {
		writeOpenUnavailable(w)
		return
	}
	groupID := r.PathValue("group_id")
	if !e.authorizeGroupRead(w, r, groupID) {
		return
	}
	snapshot, err := e.Open.GetOpen(r.Context(), groupID)
	if err != nil {
		writeOpenUnavailable(w)
		return
	}
	response, err := e.openResponse(groupID, snapshot)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to encode the open-Meets snapshot.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func (e *Endpoint) openStream(w http.ResponseWriter, r *http.Request) {
	if e.Open == nil || e.Groups == nil || e.Cursors == nil || e.CurrentHub == nil || e.CurrentReady == nil {
		w.Header().Set("Retry-After", "1")
		writeOpenUnavailable(w)
		return
	}
	groupID := r.PathValue("group_id")
	stream := &sse.Handler{
		Hub: e.CurrentHub, Ready: e.CurrentReady,
		ViewKey:            "open-meets:" + groupID,
		AuthorizationScope: "open-meets-group:" + groupID,
		UID:                currentUID(r.Context()), EventName: "open_meets",
		SnapshotFailureStatus: http.StatusServiceUnavailable,
		Authorize: func(ctx context.Context) (bool, int) {
			allowed, err := e.Groups.CanRead(ctx, currentUID(ctx), groupID)
			if err != nil {
				return false, http.StatusServiceUnavailable
			}
			if !allowed {
				return false, http.StatusNotFound
			}
			return true, http.StatusNotFound
		},
		Snapshot: func(ctx context.Context, cursor sse.CursorInput) (sse.ViewSnapshot, error) {
			snapshot, err := e.Open.GetOpen(ctx, groupID)
			if err != nil {
				return sse.ViewSnapshot{}, err
			}
			if cursor.Supplied {
				_, _ = e.Cursors.Validate(cursor.Value, sse.CursorExpectation{
					Stream: "open-meets-v1", Resource: groupID, Generation: snapshot.Generation,
					HighWater: snapshot.Sequence,
				})
			}
			response, err := e.openResponse(groupID, snapshot)
			if err != nil {
				return sse.ViewSnapshot{}, err
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				return sse.ViewSnapshot{}, err
			}
			return sse.ViewSnapshot{Data: encoded, Cursor: response.Cursor, Sequence: snapshot.Sequence}, nil
		},
		Accepted: func(ctx context.Context) error {
			return e.Open.RenewOpen(ctx, groupID)
		},
	}
	stream.ServeHTTP(w, r)
}

func (e *Endpoint) currentStream(w http.ResponseWriter, r *http.Request) {
	if e.Current == nil || e.Groups == nil || e.Cursors == nil || e.CurrentHub == nil || e.CurrentReady == nil {
		w.Header().Set("Retry-After", "1")
		writeCurrentUnavailable(w)
		return
	}
	groupID := r.PathValue("group_id")
	date := e.Current.CurrentDate()
	var acceptedDate string
	stream := &sse.Handler{
		Hub: e.CurrentHub, Ready: e.CurrentReady,
		ViewKey:            fmt.Sprintf("current-meets:%s:%s", groupID, date),
		AuthorizationScope: "current-meets-group:" + groupID,
		UID:                currentUID(r.Context()), EventName: "current_meets",
		SnapshotFailureStatus: http.StatusServiceUnavailable,
		Authorize: func(ctx context.Context) (bool, int) {
			if e.Current.CurrentDate() != date {
				return false, http.StatusNotFound
			}
			allowed, err := e.Groups.CanRead(ctx, currentUID(ctx), groupID)
			if err != nil {
				return false, http.StatusServiceUnavailable
			}
			if !allowed {
				return false, http.StatusNotFound
			}
			return true, http.StatusNotFound
		},
		Snapshot: func(ctx context.Context, cursor sse.CursorInput) (sse.ViewSnapshot, error) {
			snapshot, err := e.Current.Get(ctx, groupID)
			if err != nil {
				return sse.ViewSnapshot{}, err
			}
			if snapshot.Date != date {
				return sse.ViewSnapshot{}, currentmeetcache.ErrRetired
			}
			acceptedDate = snapshot.Date
			if cursor.Supplied {
				_, _ = e.Cursors.Validate(cursor.Value, sse.CursorExpectation{
					Stream: "current-meets-v1", Resource: groupID, Generation: snapshot.Generation,
					HighWater: snapshot.Sequence, Date: snapshot.Date,
				})
			}
			response, err := e.currentResponse(groupID, snapshot)
			if err != nil {
				return sse.ViewSnapshot{}, err
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				return sse.ViewSnapshot{}, err
			}
			return sse.ViewSnapshot{Data: encoded, Cursor: response.Cursor, Sequence: snapshot.Sequence}, nil
		},
		Accepted: func(ctx context.Context) error {
			if acceptedDate == "" {
				return currentmeetcache.ErrUnavailable
			}
			return e.Current.Renew(ctx, groupID, acceptedDate)
		},
	}
	stream.ServeHTTP(w, r)
}

func (e *Endpoint) authorizeGroupRead(w http.ResponseWriter, r *http.Request, groupID string) bool {
	allowed, err := e.Groups.CanRead(r.Context(), currentUID(r.Context()), groupID)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return false
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusNotFound, "not_found", "The requested Group was not found.")
		return false
	}
	return true
}

func (e *Endpoint) currentResponse(groupID string, snapshot currentmeetcache.Snapshot) (currentMeetsResponse, error) {
	cursor, err := e.Cursors.Encode(sse.CursorClaims{
		Stream: "current-meets-v1", Resource: groupID, Generation: snapshot.Generation,
		Sequence: snapshot.Sequence, Date: snapshot.Date,
	})
	if err != nil {
		return currentMeetsResponse{}, err
	}
	return e.meetsResponse(snapshot, cursor)
}

func (e *Endpoint) openResponse(groupID string, snapshot currentmeetcache.Snapshot) (currentMeetsResponse, error) {
	cursor, err := e.Cursors.Encode(sse.CursorClaims{
		Stream: "open-meets-v1", Resource: groupID, Generation: snapshot.Generation,
		Sequence: snapshot.Sequence,
	})
	if err != nil {
		return currentMeetsResponse{}, err
	}
	return e.meetsResponse(snapshot, cursor)
}

func (e *Endpoint) meetsResponse(snapshot currentmeetcache.Snapshot, cursor string) (currentMeetsResponse, error) {
	response := currentMeetsResponse{Cursor: cursor, Meets: make([]meetResponse, 0, len(snapshot.Meets))}
	for _, item := range snapshot.Meets {
		var selectedVenue any
		if item.Venue != nil {
			selectedVenue = venueSummary{
				ID: item.Venue.ID, Status: item.Venue.Status, Name: item.Venue.Name,
				Address: item.Venue.Address, WebsiteURL: item.Venue.WebsiteURL,
				ImageURL: item.Venue.ImageURL, VenueType: item.Venue.VenueType,
			}
		} else if item.MissingVenueID != "" {
			selectedVenue = missingVenueReference{ID: item.MissingVenueID, Status: "missing"}
		}
		meet := item.Meet
		response.Meets = append(response.Meets, meetResponse{
			ID: meet.ID, GroupID: meet.GroupID, Date: meet.Date, VotingComplete: meet.VotingComplete,
			Venue: selectedVenue, CandidateVenueIDs: meet.CandidateVenueIDs,
			RestaurantID: optionalValue(meet.RestaurantID), RestaurantTime: optionalValue(meet.RestaurantTime),
			PreviousVenueIDs: meet.PreviousVenueIDs,
		})
	}
	return response, nil
}

func currentUID(ctx context.Context) string {
	principal, ok := firebaseauth.FromContext(ctx)
	if !ok {
		return ""
	}
	return principal.UID
}

func writeCurrentUnavailable(w http.ResponseWriter) {
	firebaseauth.WriteError(w, http.StatusServiceUnavailable, "projection_unavailable", "Current Meets are temporarily unavailable.")
}

func writeOpenUnavailable(w http.ResponseWriter) {
	firebaseauth.WriteError(w, http.StatusServiceUnavailable, "projection_unavailable", "Open Meets are temporarily unavailable.")
}

func writePastUnavailable(w http.ResponseWriter) {
	firebaseauth.WriteError(w, http.StatusServiceUnavailable, "past_meets_unavailable", "Past Meets are temporarily unavailable.")
}

func writePastVenueUnavailable(w http.ResponseWriter) {
	firebaseauth.WriteError(w, http.StatusServiceUnavailable, "venue_unavailable", "Past Meet Venue data is temporarily unavailable.")
}

func writeCurrentReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, currentmeetcache.ErrUnavailable) || errors.Is(err, currentmeetcache.ErrRetired) {
		writeCurrentUnavailable(w)
		return
	}
	firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to read current Meets.")
}

func (e *Endpoint) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := firebaseauth.FromContext(r.Context())
	if !ok {
		firebaseauth.WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
		return
	}
	if e.Reader == nil || e.Groups == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "projection_unavailable", "Meet details are temporarily unavailable.")
		return
	}
	snapshot, err := e.Reader.Get(r.Context(), r.PathValue("meet_id"))
	if errors.Is(err, meetcache.ErrNotFound) {
		firebaseauth.WriteError(w, http.StatusNotFound, "not_found", "The requested Meet was not found.")
		return
	}
	if errors.Is(err, meetcache.ErrUnavailable) {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "projection_unavailable", "Meet details are temporarily unavailable.")
		return
	}
	if err != nil {
		firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to read Meet details.")
		return
	}
	allowed, err := e.Groups.CanRead(r.Context(), principal.UID, snapshot.Projection.GroupID)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "Meet access could not be verified.")
		return
	}
	if !allowed {
		firebaseauth.WriteError(w, http.StatusNotFound, "not_found", "The requested Meet was not found.")
		return
	}
	projection := snapshot.Projection
	selectedVenue, err := e.selectedVenueResponse(r.Context(), projection.SelectedVenueID)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "venue_unavailable", "Meet Venue data is temporarily unavailable.")
		return
	}
	response := meetResponse{
		ID:                projection.ID,
		GroupID:           projection.GroupID,
		Date:              projection.Date,
		VotingComplete:    projection.VotingComplete,
		Venue:             selectedVenue,
		CandidateVenueIDs: projection.CandidateVenueIDs,
		RestaurantID:      optionalValue(projection.RestaurantID),
		RestaurantTime:    optionalValue(projection.RestaurantTime),
		PreviousVenueIDs:  projection.PreviousVenueIDs,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func optionalValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
