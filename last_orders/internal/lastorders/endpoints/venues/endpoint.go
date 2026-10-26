package venues

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
)

type reader interface {
	Get(context.Context, string) (venuecache.VenueProjection, error)
	ListSnapshot(context.Context) (venuecache.Snapshot, error)
	CanManage(context.Context, string) (bool, error)
	Create(context.Context, map[string]any) (venuecache.VenueProjection, error)
	Patch(context.Context, string, map[string]any) (venuecache.VenueProjection, error)
	Delete(context.Context, string) error
}

type Endpoint struct {
	Reader  reader
	Ready   <-chan struct{}
	Hub     *sse.Hub
	Cursors *sse.CursorCodec
	Auth    firebaseauth.Verifier
}

type venue struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	VenueType          string          `json:"venue_type"`
	WebsiteURL         *string         `json:"website_url"`
	MapURL             *string         `json:"map_url"`
	Address            string          `json:"address"`
	LocationID         string          `json:"location_id"`
	ImageURL           *string         `json:"image_url"`
	Recurrence         json.RawMessage `json:"recurrence,omitempty"`
	NextOccurrenceDate string          `json:"next_occurrence_date,omitempty"`
	Notes              *string         `json:"notes,omitempty"`
	Parking            *bool           `json:"parking,omitempty"`
	Food               *bool           `json:"food,omitempty"`
	DogFriendly        *bool           `json:"dog_friendly,omitempty"`
	BeerGarden         *bool           `json:"beer_garden,omitempty"`
	OutOfTown          *bool           `json:"out_of_town,omitempty"`
	Banned             *bool           `json:"banned,omitempty"`
}

type listResponse struct {
	Venues     []venue `json:"venues"`
	NextCursor *string `json:"next_cursor"`
}

type streamResponse struct {
	Venues []venue `json:"venues"`
	Cursor string  `json:"cursor"`
}

type streamEndpoint struct {
	handler *sse.Handler
	reader  reader
	codec   *sse.CursorCodec
}

func (e *streamEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if value, supplied := sse.RequestCursor(r); supplied {
		snapshot, err := e.reader.ListSnapshot(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read venues.")
			return
		}
		if _, err := e.codec.Validate(value, sse.CursorExpectation{Stream: "venues", Resource: "all", Generation: snapshot.State.Generation, HighWater: snapshot.State.Sequence}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "The venue stream cursor is invalid or expired.")
			return
		}
	}
	e.handler.ServeHTTP(w, r)
}

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/venues", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.list)))
	mux.Handle("GET /api/venues/{venue_id}", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.get)))
	guard := func(handler http.HandlerFunc) http.Handler {
		return firebaseauth.Middleware(e.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := firebaseauth.FromContext(r.Context())
			if !ok {
				firebaseauth.WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
				return
			}
			allowed, err := e.Reader.CanManage(r.Context(), principal.UID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "Unable to verify venue-management permission.")
				return
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "forbidden", "The canManagePubs permission is required.")
				return
			}
			handler.ServeHTTP(w, r)
		}))
	}
	mux.Handle("POST /api/venues", guard(e.create))
	mux.Handle("PATCH /api/venues/{venue_id}", guard(e.patch))
	mux.Handle("DELETE /api/venues/{venue_id}", guard(e.delete))
	if e.Hub != nil && e.Cursors != nil && e.Ready != nil {
		stream := &sse.Handler{
			Hub: e.Hub, Ready: e.Ready, ViewKey: "venues:all", AuthorizationScope: "venues:public",
			EventName: "venues", Snapshot: e.snapshot,
			Authorize: func(context.Context) (bool, int) { return true, http.StatusNotFound },
		}
		mux.Handle("GET /api/venues/stream", firebaseauth.OptionalMiddleware(e.Auth, &streamEndpoint{handler: stream, reader: e.Reader, codec: e.Cursors}))
	}
}

var venueFields = map[string]string{
	"name": "name", "venue_type": "venueType", "website_url": "web_site", "map_url": "map",
	"address": "address", "notes": "notes", "image_url": "pubImage", "recurrence": "recurrence",
	"next_occurrence_date": "next_occurrence_date", "parking": "parking", "food": "food",
	"dog_friendly": "dog_friend", "beer_garden": "beer_gerden", "banned": "banned",
	"location_id": "location_id", "visible_group_ids": "visible_group_ids",
}

var nullableFields = map[string]bool{
	"website_url": true, "map_url": true, "image_url": true, "recurrence": true,
	"next_occurrence_date": true, "location_id": true,
}

func decodeWrite(r *http.Request, patch bool) (map[string]any, error) {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.UseNumber()
	var input map[string]json.RawMessage
	if err := decoder.Decode(&input); err != nil || input == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("request body must contain one JSON value")
	}
	if len(input) == 0 {
		return nil, errors.New("request body must not be empty")
	}
	fields := make(map[string]any, len(input))
	for apiName, raw := range input {
		field, ok := venueFields[apiName]
		if !ok {
			return nil, errors.New("request contains an unsupported venue field")
		}
		if string(raw) == "null" {
			if !nullableFields[apiName] {
				return nil, errors.New("null is not allowed for this venue field")
			}
			if patch {
				fields[field] = firestore.Delete
			}
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, errors.New("request contains invalid JSON")
		}
		switch field {
		case "name", "venueType", "web_site", "map", "address", "notes", "pubImage", "next_occurrence_date", "location_id":
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("venue text fields must be strings")
			}
			if (field == "web_site" || field == "map" || field == "pubImage") && text != "" {
				parsed, err := url.ParseRequestURI(text)
				if err != nil || parsed.Scheme == "" {
					return nil, errors.New("venue URL fields must be absolute URLs")
				}
			}
			if field == "next_occurrence_date" {
				if _, err := time.Parse(time.DateOnly, text); err != nil {
					return nil, errors.New("next_occurrence_date must be an ISO date")
				}
			}
		case "parking", "food", "dog_friend", "beer_gerden", "banned":
			if _, ok := value.(bool); !ok {
				return nil, errors.New("venue feature fields must be booleans")
			}
		case "visible_group_ids":
			groups, ok := value.([]any)
			if !ok {
				return nil, errors.New("visible_group_ids must be an array")
			}
			for _, group := range groups {
				if _, ok := group.(string); !ok {
					return nil, errors.New("visible_group_ids must contain strings")
				}
			}
		case "recurrence":
			canonical, err := venuecache.NormalizeRecurrence(value)
			if err != nil {
				return nil, errors.New("recurrence is invalid")
			}
			var normalized any
			if err := json.Unmarshal([]byte(canonical), &normalized); err != nil {
				return nil, errors.New("recurrence is invalid")
			}
			value = normalized
		}
		fields[field] = value
	}
	if !patch {
		for _, name := range []string{"name", "venue_type", "address"} {
			if _, ok := input[name]; !ok {
				return nil, errors.New("name, venue_type and address are required")
			}
		}
		for _, name := range []string{"name", "venue_type", "address"} {
			value, ok := fields[venueFields[name]].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, errors.New(name + " must be a non-empty string")
			}
		}
	}
	if value, ok := fields["name"].(string); ok && strings.TrimSpace(value) == "" {
		return nil, errors.New("name must not be empty")
	}
	if value, ok := fields["venueType"].(string); ok && value != "pub" && value != "restaurant" && value != "event" {
		return nil, errors.New("venue_type is invalid")
	}
	if _, ok := input["name"]; ok {
		if _, valid := fields["name"].(string); !valid {
			return nil, errors.New("name must be a string")
		}
	}
	return fields, nil
}

func (e *Endpoint) create(w http.ResponseWriter, r *http.Request) {
	fields, err := decodeWrite(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	item, err := e.Reader.Create(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue.")
		return
	}
	e.publishVenueChange(r.Context())
	w.Header().Set("Location", "/api/venues/"+item.ID)
	writeJSON(w, http.StatusCreated, fromProjection(item))
}

func (e *Endpoint) patch(w http.ResponseWriter, r *http.Request) {
	fields, err := decodeWrite(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	item, err := e.Reader.Patch(r.Context(), r.PathValue("venue_id"), fields)
	if errors.Is(err, venuecache.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Venue not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update venue.")
		return
	}
	e.publishVenueChange(r.Context())
	writeJSON(w, http.StatusOK, fromProjection(item))
}

func (e *Endpoint) delete(w http.ResponseWriter, r *http.Request) {
	if err := e.Reader.Delete(r.Context(), r.PathValue("venue_id")); err != nil {
		if errors.Is(err, venuecache.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Venue not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to delete venue.")
		return
	}
	e.publishVenueChange(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (e *Endpoint) publishVenueChange(ctx context.Context) {
	if e.Hub == nil {
		return
	}
	if snapshot, err := e.Reader.ListSnapshot(ctx); err == nil {
		e.Hub.PublishChanged("venues:all", snapshot.State.Sequence)
	}
}

func (e *Endpoint) list(w http.ResponseWriter, r *http.Request) {
	if e.Reader == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Venue data is not ready.")
		return
	}
	snapshot, err := e.Reader.ListSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read venues.")
		return
	}
	booleanFilters := map[string]bool{}
	for _, parameter := range []string{"food", "dog_friendly", "beer_garden"} {
		if raw, supplied := r.URL.Query()[parameter]; supplied {
			if len(raw) != 1 {
				writeError(w, http.StatusBadRequest, "bad_request", "Boolean filters must appear once.")
				return
			}
			value, parseErr := strconv.ParseBool(raw[0])
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "Boolean filters must be true or false.")
				return
			}
			booleanFilters[parameter] = value
		}
	}
	items := make([]venue, 0, len(snapshot.Venues))
	for _, item := range snapshot.Venues {
		if value := r.URL.Query().Get("location_id"); value != "" && item.LocationID != value {
			continue
		}
		if value := r.URL.Query().Get("venue_type"); value != "" && item.VenueType != value {
			continue
		}
		if value := r.URL.Query().Get("name"); value != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(value)) {
			continue
		}
		matches := true
		for parameter, expected := range booleanFilters {
			var actual *bool
			switch parameter {
			case "food":
				actual = item.Food
			case "dog_friendly":
				actual = item.DogFriendly
			case "beer_garden":
				actual = item.BeerGarden
			}
			if actual == nil || *actual != expected {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		items = append(items, fromProjection(item))
	}
	writeJSON(w, http.StatusOK, listResponse{Venues: items})
}

func (e *Endpoint) get(w http.ResponseWriter, r *http.Request) {
	if e.Reader == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Venue data is not ready.")
		return
	}
	item, err := e.Reader.Get(r.Context(), r.PathValue("venue_id"))
	if errors.Is(err, venuecache.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Venue not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read venue.")
		return
	}
	writeJSON(w, http.StatusOK, fromProjection(item))
}

func (e *Endpoint) snapshot(ctx context.Context, input sse.CursorInput) (sse.ViewSnapshot, error) {
	state, err := e.Reader.ListSnapshot(ctx)
	if err != nil {
		return sse.ViewSnapshot{}, err
	}
	if input.Supplied {
		_, err = e.Cursors.Validate(input.Value, sse.CursorExpectation{Stream: "venues", Resource: "all", Generation: state.State.Generation, HighWater: state.State.Sequence})
		if err != nil {
			return sse.ViewSnapshot{}, sse.ErrInvalidCursor
		}
	}
	cursor, err := e.Cursors.Encode(sse.CursorClaims{Stream: "venues", Resource: "all", Generation: state.State.Generation, Sequence: state.State.Sequence})
	if err != nil {
		return sse.ViewSnapshot{}, err
	}
	items := make([]venue, 0, len(state.Venues))
	for _, item := range state.Venues {
		items = append(items, fromProjection(item))
	}
	data, err := json.Marshal(streamResponse{Venues: items, Cursor: cursor})
	if err != nil {
		return sse.ViewSnapshot{}, err
	}
	return sse.ViewSnapshot{Data: data, Cursor: cursor, Sequence: state.State.Sequence}, nil
}

func fromProjection(item venuecache.VenueProjection) venue {
	food := false
	if item.Food != nil {
		food = *item.Food
	}
	banned := false
	if item.Banned != nil {
		banned = *item.Banned
	}
	result := venue{ID: item.ID, Name: item.Name, VenueType: item.VenueType, WebsiteURL: optionalString(item.Website),
		MapURL: optionalString(item.Map), Address: item.Address, LocationID: item.LocationID, ImageURL: optionalString(item.PhotoURL), NextOccurrenceDate: item.NextOccurrenceDate,
		Notes: item.Notes, Parking: item.Parking, Food: &food, DogFriendly: item.DogFriendly,
		BeerGarden: item.BeerGarden, OutOfTown: item.OutOfTown, Banned: item.Banned}
	result.Banned = &banned
	if result.VenueType == "" {
		result.VenueType = "pub"
	}
	if item.RecurrenceJSON != "" {
		result.Recurrence = json.RawMessage(item.RecurrenceJSON)
	}
	return result
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
