package meets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"last_orders/internal/lastorders/components/currentmeetcache"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
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

type Endpoint struct {
	Reader       reader
	Venues       venueReader
	Groups       GroupAuthorizer
	Auth         firebaseauth.Verifier
	Current      currentReader
	CurrentReady <-chan struct{}
	CurrentHub   *sse.Hub
	Cursors      *sse.CursorCodec
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

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/meet/{meet_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.get)))
	mux.Handle("GET /api/meets/{group_id}/current", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.current)))
	mux.Handle("GET /api/meets/{group_id}/current/stream", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.currentStream)))
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
	var selectedVenue any
	if projection.SelectedVenueID != "" {
		if e.Venues == nil {
			firebaseauth.WriteError(w, http.StatusServiceUnavailable, "venue_unavailable", "Meet Venue data is temporarily unavailable.")
			return
		}
		venue, missing, err := e.Venues.GetForMeet(r.Context(), projection.SelectedVenueID)
		if err != nil {
			firebaseauth.WriteError(w, http.StatusServiceUnavailable, "venue_unavailable", "Meet Venue data is temporarily unavailable.")
			return
		}
		if missing {
			selectedVenue = missingVenueReference{ID: projection.SelectedVenueID, Status: "missing"}
		} else {
			venueType := venue.VenueType
			if venueType == "" {
				venueType = "pub"
			}
			status := "active"
			if venue.DeletedOn != "" {
				status = "deleted"
			}
			selectedVenue = venueSummary{
				ID: venue.ID, Status: status, Name: venue.Name, Address: venue.Address,
				WebsiteURL: optionalValue(venue.Website), ImageURL: optionalValue(venue.PhotoURL), VenueType: venueType,
			}
		}
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
