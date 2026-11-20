package meets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/meetcache"
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

type Endpoint struct {
	Reader reader
	Venues venueReader
	Groups GroupAuthorizer
	Auth   firebaseauth.Verifier
}

type venueSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	ImageURL  *string `json:"image_url"`
	VenueType string  `json:"venue_type"`
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

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/meet/{meet_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.get)))
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
			selectedVenue = venueSummary{
				ID: venue.ID, Name: venue.Name, Address: venue.Address,
				ImageURL: optionalValue(venue.PhotoURL), VenueType: venueType,
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
