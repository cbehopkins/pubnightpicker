package venues

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/services/venueedit"

	"cloud.google.com/go/firestore"
)

type reader interface {
	Get(context.Context, string) (venuecache.VenueProjection, error)
	ListSnapshot(context.Context) (venuecache.Snapshot, error)
	CanManage(context.Context, string) (bool, error)
	Create(context.Context, map[string]any) (venuecache.VenueProjection, error)
	Patch(context.Context, string, map[string]any) (venuecache.VenueProjection, error)
	DeleteWithVersion(context.Context, string, time.Time) (venuecache.VenueProjection, error)
}

type conditionalReader interface {
	PatchWithBaseline(context.Context, string, []venuecache.FieldUpdate, func(venuecache.VenueProjection, []venuecache.FieldUpdate) ([]venuecache.FieldUpdate, []string, error)) (venuecache.VenueProjection, []string, error)
}

type decodedVenueWrite struct {
	Fields     map[string]any
	Values     map[string]any
	EditTokens map[string]string
}

type Endpoint struct {
	Reader            reader
	Ready             <-chan struct{}
	Hub               *sse.Hub
	OnCommittedChange func(context.Context, string)
	Cursors           *sse.CursorCodec
	EditTokens        *EditTokenCodec
	DeleteTokens      *DeleteTokenCodec
	Auth              firebaseauth.Verifier
}

type venue struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	VenueType          string            `json:"venue_type"`
	WebsiteURL         *string           `json:"website_url"`
	MapURL             *string           `json:"map_url"`
	Address            string            `json:"address"`
	LocationID         *string           `json:"location_id"`
	ImageURL           *string           `json:"image_url"`
	Recurrence         json.RawMessage   `json:"recurrence"`
	NextOccurrenceDate *string           `json:"next_occurrence_date"`
	Notes              string            `json:"notes"`
	Parking            bool              `json:"parking"`
	Food               bool              `json:"food"`
	DogFriendly        bool              `json:"dog_friendly"`
	BeerGarden         bool              `json:"beer_garden"`
	OutOfTown          bool              `json:"out_of_town"`
	Banned             bool              `json:"banned"`
	VisibleGroupIDs    []string          `json:"visible_group_ids"`
	PreviousVenueIDs   []string          `json:"previous_venue_ids"`
	EditTokens         map[string]string `json:"edit_tokens"`
	DeleteToken        string            `json:"delete_token"`
}

type listResponse struct {
	Venues     []venue `json:"venues"`
	NextCursor *string `json:"next_cursor"`
}

type streamResponse struct {
	Venues []venue `json:"venues"`
	Cursor string  `json:"cursor"`
}

type venueListFilters struct {
	LocationID  string `json:"location_id"`
	VenueType   string `json:"venue_type"`
	Name        string `json:"name"`
	Food        *bool  `json:"food"`
	DogFriendly *bool  `json:"dog_friendly"`
	BeerGarden  *bool  `json:"beer_garden"`
	Limit       int    `json:"limit"`
}

type venuePagePosition struct {
	Name string `json:"name"`
	ID   string `json:"id"`
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
		mux.Handle("GET /api/venues/stream", firebaseauth.OptionalMiddleware(e.Auth, stream))
	}
}

var venueFields = map[string]string{
	"name": "name", "venue_type": "venueType", "website_url": "web_site", "map_url": "map",
	"address": "address", "notes": "notes", "image_url": "pubImage", "recurrence": "recurrence",
	"next_occurrence_date": "next_occurrence_date", "parking": "parking", "food": "food",
	"dog_friendly": "dog_friend", "beer_garden": "beer_gerden", "banned": "banned",
	"location_id": "location_id", "visible_group_ids": "visible_group_ids", "out_of_town": "out_of_town",
}

var nullableFields = map[string]bool{
	"website_url": true, "map_url": true, "image_url": true, "recurrence": true,
	"next_occurrence_date": true, "location_id": true,
}

func decodeWrite(r *http.Request, patch bool) (decodedVenueWrite, error) {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.UseNumber()
	var input map[string]json.RawMessage
	if err := decoder.Decode(&input); err != nil || input == nil {
		return decodedVenueWrite{}, errors.New("request body must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return decodedVenueWrite{}, errors.New("request body must contain one JSON value")
	}
	if len(input) == 0 {
		return decodedVenueWrite{}, errors.New("request body must not be empty")
	}
	decoded := decodedVenueWrite{Fields: make(map[string]any, len(input)), Values: make(map[string]any, len(input))}
	for apiName, raw := range input {
		if apiName == "edit_tokens" {
			if !patch {
				return decodedVenueWrite{}, errors.New("edit_tokens is only allowed in venue patches")
			}
			if err := json.Unmarshal(raw, &decoded.EditTokens); err != nil || decoded.EditTokens == nil {
				return decodedVenueWrite{}, errors.New("edit_tokens must be an object of field tokens")
			}
			continue
		}
		field, ok := venueFields[apiName]
		if !ok {
			return decodedVenueWrite{}, errors.New("request contains an unsupported venue field")
		}
		if string(raw) == "null" {
			if !nullableFields[apiName] {
				return decodedVenueWrite{}, errors.New("null is not allowed for this venue field")
			}
			if patch {
				decoded.Fields[field] = firestore.Delete
			}
			decoded.Values[apiName] = nil
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return decodedVenueWrite{}, errors.New("request contains invalid JSON")
		}
		switch field {
		case "name", "venueType", "web_site", "map", "address", "notes", "pubImage", "next_occurrence_date", "location_id":
			text, ok := value.(string)
			if !ok {
				return decodedVenueWrite{}, errors.New("venue text fields must be strings")
			}
			if (field == "web_site" || field == "map" || field == "pubImage") && text != "" {
				parsed, err := url.ParseRequestURI(text)
				if err != nil || parsed.Scheme == "" {
					return decodedVenueWrite{}, errors.New("venue URL fields must be absolute URLs")
				}
			}
			if field == "next_occurrence_date" {
				if _, err := time.Parse(time.DateOnly, text); err != nil {
					return decodedVenueWrite{}, errors.New("next_occurrence_date must be an ISO date")
				}
			}
		case "parking", "food", "dog_friend", "beer_gerden", "out_of_town", "banned":
			if _, ok := value.(bool); !ok {
				return decodedVenueWrite{}, errors.New("venue feature fields must be booleans")
			}
		case "visible_group_ids":
			groups, ok := value.([]any)
			if !ok {
				return decodedVenueWrite{}, errors.New("visible_group_ids must be an array")
			}
			for _, group := range groups {
				if _, ok := group.(string); !ok {
					return decodedVenueWrite{}, errors.New("visible_group_ids must contain strings")
				}
			}
		case "recurrence":
			canonical, err := venuecache.NormalizeRecurrence(value)
			if err != nil {
				return decodedVenueWrite{}, errors.New("recurrence is invalid")
			}
			var normalized any
			if err := json.Unmarshal([]byte(canonical), &normalized); err != nil {
				return decodedVenueWrite{}, errors.New("recurrence is invalid")
			}
			value = normalized
		}
		decoded.Fields[field] = value
		decoded.Values[apiName] = value
	}
	if !patch {
		for _, name := range []string{"name", "venue_type", "address"} {
			if _, ok := input[name]; !ok {
				return decodedVenueWrite{}, errors.New("name, venue_type and address are required")
			}
		}
		for _, name := range []string{"name", "venue_type", "address"} {
			value, ok := decoded.Fields[venueFields[name]].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return decodedVenueWrite{}, errors.New(name + " must be a non-empty string")
			}
		}
	}
	if value, ok := decoded.Fields["name"].(string); ok && strings.TrimSpace(value) == "" {
		return decodedVenueWrite{}, errors.New("name must not be empty")
	}
	if value, ok := decoded.Fields["venueType"].(string); ok && value != "pub" && value != "restaurant" && value != "event" {
		return decodedVenueWrite{}, errors.New("venue_type is invalid")
	}
	if _, ok := input["name"]; ok {
		if _, valid := decoded.Fields["name"].(string); !valid {
			return decodedVenueWrite{}, errors.New("name must be a string")
		}
	}
	return decoded, nil
}

func (e *Endpoint) create(w http.ResponseWriter, r *http.Request) {
	decoded, err := decodeWrite(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	item, err := e.Reader.Create(r.Context(), decoded.Fields)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue.")
		return
	}
	e.publishVenueChange(r.Context(), item.ID)
	w.Header().Set("Location", "/api/venues/"+item.ID)
	result, err := e.represent(item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue response.")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (e *Endpoint) patch(w http.ResponseWriter, r *http.Request) {
	decoded, err := decodeWrite(r, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(decoded.Fields) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "Venue patch must contain at least one editable field.")
		return
	}
	venueID := r.PathValue("venue_id")
	for apiName := range decoded.EditTokens {
		if _, changed := decoded.Values[apiName]; !changed {
			writeError(w, http.StatusBadRequest, "invalid_edit_token", "Edit tokens may only be supplied for changed venue fields.")
			return
		}
	}
	updates := make([]venuecache.FieldUpdate, 0, len(decoded.Values))
	for apiName, value := range decoded.Values {
		token, supplied := decoded.EditTokens[apiName]
		if !supplied {
			writeError(w, http.StatusPreconditionRequired, "precondition_required", "A baseline token is required for every changed venue field.")
			return
		}
		if e.EditTokens == nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Venue edit tokens are unavailable.")
			return
		}
		baseline, err := e.EditTokens.Validate(token, venueID, apiName)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_edit_token", "A venue field baseline token is invalid.")
			return
		}
		updates = append(updates, venuecache.FieldUpdate{APIField: apiName, FirestoreField: venueFields[apiName],
			Value: decoded.Fields[venueFields[apiName]], CanonicalValue: value, BaselineDigest: baseline})
	}
	conditional, ok := e.Reader.(conditionalReader)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "Conditional venue writes are unavailable.")
		return
	}
	item, conflicts, err := conditional.PatchWithBaseline(r.Context(), venueID, updates, venueedit.ResolvePatch)
	if errors.Is(err, venuecache.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Venue not found.")
		return
	}
	if errors.Is(err, venuecache.ErrFieldConflict) {
		current, representErr := e.represent(item)
		if representErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create current venue response.")
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":         map[string]any{"code": "field_conflict", "message": "One or more venue fields changed since they were loaded.", "fields": conflicts},
			"current_venue": current,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update venue.")
		return
	}
	e.publishVenueChange(r.Context(), item.ID)
	result, err := e.represent(item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue response.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (e *Endpoint) delete(w http.ResponseWriter, r *http.Request) {
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch == "" {
		writeError(w, http.StatusPreconditionRequired, "precondition_required", "A whole-document version token is required to delete a venue.")
		return
	}
	if len(ifMatch) < 3 || ifMatch[0] != '"' || ifMatch[len(ifMatch)-1] != '"' {
		writeError(w, http.StatusBadRequest, "invalid_edit_token", "A venue delete token is invalid.")
		return
	}
	if e.DeleteTokens == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Venue delete tokens are unavailable.")
		return
	}
	venueID := r.PathValue("venue_id")
	updateTime, err := e.DeleteTokens.Validate(ifMatch[1:len(ifMatch)-1], venueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_edit_token", "A venue delete token is invalid.")
		return
	}
	currentItem, err := e.Reader.DeleteWithVersion(r.Context(), venueID, updateTime)
	if errors.Is(err, venuecache.ErrVersionConflict) {
		current, representErr := e.represent(currentItem)
		if representErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create current venue response.")
			return
		}
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{
			"error":         map[string]string{"code": "precondition_failed", "message": "The venue changed after it was loaded."},
			"current_venue": current,
		})
		return
	}
	if err != nil {
		if errors.Is(err, venuecache.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Venue not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to delete venue.")
		return
	}
	e.publishVenueChange(r.Context(), venueID)
	w.WriteHeader(http.StatusNoContent)
}

func (e *Endpoint) publishVenueChange(ctx context.Context, venueID string) {
	if e.OnCommittedChange != nil {
		e.OnCommittedChange(ctx, venueID)
	}
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
	filters, cursor, err := parseVenueListFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resource, err := venueListResource(filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to prepare venue query.")
		return
	}
	var position venuePagePosition
	if cursor != "" {
		claims, err := e.Cursors.Validate(cursor, sse.CursorExpectation{
			Stream: "venues:list", Resource: resource, Generation: snapshot.State.Generation,
			HighWater: snapshot.State.Sequence,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "The venue list cursor is invalid or expired.")
			return
		}
		if claims.Sequence != snapshot.State.Sequence {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "The venue list changed after this cursor was issued.")
			return
		}
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(claims.Position)
		if err != nil || json.Unmarshal(decoded, &position) != nil || position.ID == "" {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "The venue list cursor is invalid or expired.")
			return
		}
	}
	filtered := make([]venuecache.VenueProjection, 0, len(snapshot.Venues))
	for _, item := range snapshot.Venues {
		if filters.LocationID != "" && item.LocationID != filters.LocationID {
			continue
		}
		if filters.VenueType != "" && effectiveVenueType(item.VenueType) != filters.VenueType {
			continue
		}
		if filters.Name != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(filters.Name)) {
			continue
		}
		if !matchesOptionalBool(item.Food, filters.Food) || !matchesOptionalBool(item.DogFriendly, filters.DogFriendly) || !matchesOptionalBool(item.BeerGarden, filters.BeerGarden) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool { return venueLess(filtered[i], filtered[j]) })
	if position.ID != "" {
		start := sort.Search(len(filtered), func(i int) bool { return venueAfter(filtered[i], position) })
		filtered = filtered[start:]
	}
	hasNext := len(filtered) > filters.Limit
	if hasNext {
		filtered = filtered[:filters.Limit]
	}
	items := make([]venue, 0, len(filtered))
	for _, item := range filtered {
		result, err := e.represent(item)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue response.")
			return
		}
		items = append(items, result)
	}
	var nextCursor *string
	if hasNext && len(filtered) > 0 {
		last := filtered[len(filtered)-1]
		encodedPosition, err := json.Marshal(venuePagePosition{Name: strings.ToLower(last.Name), ID: last.ID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue cursor.")
			return
		}
		sequence := base64.RawURLEncoding.EncodeToString(encodedPosition)
		token, err := e.Cursors.Encode(sse.CursorClaims{Stream: "venues:list", Resource: resource,
			Generation: snapshot.State.Generation, Sequence: snapshot.State.Sequence, Position: sequence})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue cursor.")
			return
		}
		nextCursor = &token
	}
	writeJSON(w, http.StatusOK, listResponse{Venues: items, NextCursor: nextCursor})
}

func parseVenueListFilters(r *http.Request) (venueListFilters, string, error) {
	filters := venueListFilters{Limit: 50}
	query := r.URL.Query()
	for key, target := range map[string]*string{"location_id": &filters.LocationID, "venue_type": &filters.VenueType, "name": &filters.Name} {
		values, exists := query[key]
		if !exists {
			continue
		}
		if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			return venueListFilters{}, "", fmt.Errorf("%s must be supplied once with a non-empty value", key)
		}
		*target = values[0]
	}
	if filters.VenueType != "" && filters.VenueType != "pub" && filters.VenueType != "restaurant" && filters.VenueType != "event" {
		return venueListFilters{}, "", errors.New("venue_type is invalid")
	}
	for key, target := range map[string]**bool{"food": &filters.Food, "dog_friendly": &filters.DogFriendly, "beer_garden": &filters.BeerGarden} {
		values, exists := query[key]
		if !exists {
			continue
		}
		if len(values) != 1 {
			return venueListFilters{}, "", fmt.Errorf("%s may only be supplied once", key)
		}
		if values[0] != "true" && values[0] != "false" {
			return venueListFilters{}, "", fmt.Errorf("%s must be true or false", key)
		}
		value := values[0] == "true"
		*target = &value
	}
	if values, exists := query["limit"]; exists {
		if len(values) != 1 {
			return venueListFilters{}, "", errors.New("limit may only be supplied once")
		}
		limit, err := strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > 100 {
			return venueListFilters{}, "", errors.New("limit must be between 1 and 100")
		}
		filters.Limit = limit
	}
	var cursor string
	if values, exists := query["cursor"]; exists {
		if len(values) != 1 || values[0] == "" {
			return venueListFilters{}, "", errors.New("cursor may only be supplied once and must not be empty")
		}
		cursor = values[0]
	}
	return filters, cursor, nil
}

func venueListResource(filters venueListFilters) (string, error) {
	encoded, err := json.Marshal(filters)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("filter:%x", sum[:]), nil
}

func matchesOptionalBool(actual, expected *bool) bool {
	if expected == nil {
		return true
	}
	value := actual != nil && *actual
	return value == *expected
}

func effectiveVenueType(value string) string {
	if value == "" {
		return "pub"
	}
	return value
}

func venueLess(left, right venuecache.VenueProjection) bool {
	leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
	if leftName != rightName {
		return leftName < rightName
	}
	return left.ID < right.ID
}

func venueAfter(item venuecache.VenueProjection, position venuePagePosition) bool {
	name := strings.ToLower(item.Name)
	return name > position.Name || (name == position.Name && item.ID > position.ID)
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
	result, err := e.represent(item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create venue response.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (e *Endpoint) snapshot(ctx context.Context, _ sse.CursorInput) (sse.ViewSnapshot, error) {
	state, err := e.Reader.ListSnapshot(ctx)
	if err != nil {
		return sse.ViewSnapshot{}, err
	}
	sort.Slice(state.Venues, func(i, j int) bool { return venueLess(state.Venues[i], state.Venues[j]) })
	cursor, err := e.Cursors.Encode(sse.CursorClaims{Stream: "venues", Resource: "all", Generation: state.State.Generation, Sequence: state.State.Sequence})
	if err != nil {
		return sse.ViewSnapshot{}, err
	}
	items := make([]venue, 0, len(state.Venues))
	for _, item := range state.Venues {
		result, err := e.represent(item)
		if err != nil {
			return sse.ViewSnapshot{}, err
		}
		items = append(items, result)
	}
	data, err := json.Marshal(streamResponse{Venues: items, Cursor: cursor})
	if err != nil {
		return sse.ViewSnapshot{}, err
	}
	return sse.ViewSnapshot{Data: data, Cursor: cursor, Sequence: state.State.Sequence}, nil
}

func fromProjection(item venuecache.VenueProjection) venue {
	result := venue{ID: item.ID, Name: item.Name, VenueType: item.VenueType, WebsiteURL: optionalString(item.Website),
		MapURL: optionalString(item.Map), Address: item.Address, LocationID: optionalString(item.LocationID), ImageURL: optionalString(item.PhotoURL), NextOccurrenceDate: optionalString(item.NextOccurrenceDate),
		Notes: pointerText(item.Notes), Parking: pointerBool(item.Parking), Food: pointerBool(item.Food), DogFriendly: pointerBool(item.DogFriendly),
		BeerGarden: pointerBool(item.BeerGarden), OutOfTown: pointerBool(item.OutOfTown), Banned: pointerBool(item.Banned)}
	result.VisibleGroupIDs = item.VisibleGroupIDs
	if result.VisibleGroupIDs == nil {
		result.VisibleGroupIDs = []string{}
	}
	result.PreviousVenueIDs = item.PreviousVenueIDs
	if result.PreviousVenueIDs == nil {
		result.PreviousVenueIDs = []string{}
	}
	result.VenueType = effectiveVenueType(result.VenueType)
	if item.RecurrenceJSON != "" {
		result.Recurrence = json.RawMessage(item.RecurrenceJSON)
	}
	return result
}

func (e *Endpoint) represent(item venuecache.VenueProjection) (venue, error) {
	result := fromProjection(item)
	if e.DeleteTokens != nil {
		deleteToken, err := e.DeleteTokens.Issue(item.ID, item.UpdateTime)
		if err != nil {
			return venue{}, err
		}
		result.DeleteToken = deleteToken
	}
	if e.EditTokens == nil {
		return result, nil
	}
	values := venueedit.CanonicalAPIValues(item)
	result.EditTokens = make(map[string]string, len(venueFields))
	for field := range venueFields {
		token, err := e.EditTokens.Issue(item.ID, field, values[field])
		if err != nil {
			return venue{}, err
		}
		result.EditTokens[field] = token
	}
	return result, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pointerBool(value *bool) bool {
	return value != nil && *value
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
