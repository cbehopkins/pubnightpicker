package venues

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
)

type testReader struct {
	mu         sync.Mutex
	snapshot   venuecache.Snapshot
	canManage  bool
	writeCalls int
}

func (r *testReader) Get(_ context.Context, id string) (venuecache.VenueProjection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.snapshot.Venues {
		if item.ID == id {
			return item, nil
		}
	}
	return venuecache.VenueProjection{}, venuecache.ErrNotFound
}
func (r *testReader) ListSnapshot(context.Context) (venuecache.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := r.snapshot
	snapshot.Venues = append([]venuecache.VenueProjection(nil), r.snapshot.Venues...)
	return snapshot, nil
}

func (r *testReader) CanManage(context.Context, string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canManage, nil
}
func (r *testReader) Create(_ context.Context, fields map[string]any) (venuecache.VenueProjection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeCalls++
	item := venuecache.VenueProjection{ID: fmt.Sprintf("created-%d", r.writeCalls), Name: fields["name"].(string), VenueType: fields["venueType"].(string), Address: fields["address"].(string)}
	r.snapshot.Venues = append(r.snapshot.Venues, item)
	r.snapshot.State.Sequence++
	return item, nil
}
func (r *testReader) Patch(_ context.Context, id string, fields map[string]any) (venuecache.VenueProjection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeCalls++
	for index, item := range r.snapshot.Venues {
		if item.ID == id {
			if name, ok := fields["name"].(string); ok {
				item.Name = name
			}
			r.snapshot.Venues[index] = item
			r.snapshot.State.Sequence++
			return item, nil
		}
	}
	return venuecache.VenueProjection{}, venuecache.ErrNotFound
}
func (r *testReader) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeCalls++
	for index, item := range r.snapshot.Venues {
		if item.ID == id {
			r.snapshot.Venues = append(r.snapshot.Venues[:index], r.snapshot.Venues[index+1:]...)
			r.snapshot.State.Sequence++
			return nil
		}
	}
	return venuecache.ErrNotFound
}

type testVerifier struct{}

func (testVerifier) Verify(_ context.Context, token string) (firebaseauth.Principal, error) {
	if token != "valid" {
		return firebaseauth.Principal{}, firebaseauth.ErrUnauthenticated
	}
	return firebaseauth.Principal{UID: "user-1"}, nil
}

func TestListPaginatesWithFilterBoundCursor(t *testing.T) {
	codec, err := sse.NewCursorCodec([]byte("test-only-cursor-signing-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	reader := &testReader{snapshot: venuecache.Snapshot{
		State: basestore.SyncState{Generation: "generation-1", Sequence: 7},
		Venues: []venuecache.VenueProjection{
			{ID: "z", Name: "Beta"},
			{ID: "b", Name: "ALPHA"},
			{ID: "a", Name: "Alpha"},
		},
	}}
	endpoint := &Endpoint{Reader: reader, Cursors: codec}
	first := httptest.NewRecorder()
	endpoint.list(first, httptest.NewRequest("GET", "/api/venues?limit=2", nil))
	if first.Code != 200 {
		t.Fatalf("first page status=%d body=%s", first.Code, first.Body)
	}
	var page listResponse
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Venues) != 2 || page.Venues[0].ID != "a" || page.Venues[1].ID != "b" || page.NextCursor == nil {
		t.Fatalf("first page=%+v", page)
	}
	second := httptest.NewRecorder()
	endpoint.list(second, httptest.NewRequest("GET", "/api/venues?limit=2&cursor="+*page.NextCursor, nil))
	if second.Code != 200 {
		t.Fatalf("second page status=%d body=%s", second.Code, second.Body)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Venues) != 1 || page.Venues[0].ID != "z" || page.NextCursor != nil {
		t.Fatalf("second page=%+v", page)
	}
	changedFilter := httptest.NewRecorder()
	endpoint.list(changedFilter, httptest.NewRequest("GET", "/api/venues?limit=2&name=beta&cursor="+*mustCursor(t, endpoint, "limit=2"), nil))
	if changedFilter.Code != 400 {
		t.Fatalf("changed-filter cursor status=%d body=%s", changedFilter.Code, changedFilter.Body)
	}
	oldCursor := mustCursorForSnapshot(t, endpoint)
	reader.snapshot.State.Sequence++
	staleCursor := httptest.NewRecorder()
	endpoint.list(staleCursor, httptest.NewRequest("GET", "/api/venues?limit=2&cursor="+*oldCursor, nil))
	if staleCursor.Code != 400 {
		t.Fatalf("stale cursor status=%d body=%s", staleCursor.Code, staleCursor.Body)
	}
}

func TestListFiltersCollection(t *testing.T) {
	food := true
	noFood := false
	dogFriendly := true
	beerGarden := false
	endpoint := &Endpoint{Reader: &testReader{snapshot: venuecache.Snapshot{Venues: []venuecache.VenueProjection{
		{ID: "pub-1", Name: "The Crown", VenueType: "pub", LocationID: "north", Food: &food, DogFriendly: &dogFriendly, BeerGarden: &beerGarden},
		{ID: "pub-2", Name: "Crown Inn", VenueType: "pub", LocationID: "south", Food: &food, DogFriendly: &dogFriendly},
		{ID: "restaurant-1", Name: "The Crown Kitchen", VenueType: "restaurant", LocationID: "north", Food: &food, DogFriendly: &dogFriendly},
		{ID: "default-pub", Name: "Other", LocationID: "north", Food: &noFood},
	}}}}

	request := httptest.NewRequest(http.MethodGet, "/api/venues?location_id=north&venue_type=pub&name=CROWN&food=true&dog_friendly=true&beer_garden=false", nil)
	response := httptest.NewRecorder()
	endpoint.list(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	var page listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Venues) != 1 || page.Venues[0].ID != "pub-1" {
		t.Fatalf("filtered venues=%+v, want only pub-1", page.Venues)
	}
}

func TestListRejectsInvalidQueryParameters(t *testing.T) {
	endpoint := &Endpoint{Reader: &testReader{}}
	for _, query := range []string{
		"limit=0", "limit=101", "limit=1&limit=2", "name=", "name=a&name=b",
		"venue_type=club", "food=yes", "food=true&food=false", "cursor=",
	} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			endpoint.list(response, httptest.NewRequest(http.MethodGet, "/api/venues?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("query %q status=%d body=%s, want 400", query, response.Code, response.Body)
			}
		})
	}
}

func TestVenueRoutesKeepReadsPublicAndProtectWrites(t *testing.T) {
	reader := &testReader{snapshot: venuecache.Snapshot{Venues: []venuecache.VenueProjection{{ID: "venue-1", Name: "Pub"}}}}
	endpoint := &Endpoint{Reader: reader, Auth: testVerifier{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	responses := make([]string, 0, 2)
	for _, authorization := range []string{"", "Bearer valid"} {
		request := httptest.NewRequest(http.MethodGet, "/api/venues", nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("read with Authorization %q status=%d body=%s", authorization, response.Code, response.Body)
		}
		responses = append(responses, response.Body.String())
	}
	if responses[0] != responses[1] {
		t.Fatalf("anonymous and authenticated reads differ: anonymous=%s authenticated=%s", responses[0], responses[1])
	}

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/venues", `{"name":"Pub","venue_type":"pub","address":"Road"}`},
		{http.MethodPatch, "/api/venues/venue-1", `{"name":"New name"}`},
		{http.MethodDelete, "/api/venues/venue-1", ""},
	} {
		t.Run(test.method, func(t *testing.T) {
			for _, authorization := range []string{"", "Bearer valid"} {
				request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
				if authorization != "" {
					request.Header.Set("Authorization", authorization)
				}
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)
				want := http.StatusUnauthorized
				if authorization != "" {
					want = http.StatusForbidden
				}
				if response.Code != want {
					t.Errorf("Authorization %q status=%d body=%s, want %d", authorization, response.Code, response.Body, want)
				}
			}
		})
	}
	if reader.writeCalls != 0 {
		t.Fatalf("unauthorized writes reached the reader %d times", reader.writeCalls)
	}
}

func TestVenueCRUDRoutesUpdateCollection(t *testing.T) {
	reader := &testReader{
		canManage: true,
		snapshot:  venuecache.Snapshot{State: basestore.SyncState{Generation: "generation-1", Sequence: 1}},
	}
	endpoint := &Endpoint{Reader: reader, Auth: testVerifier{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	create := httptest.NewRequest(http.MethodPost, "/api/venues", strings.NewReader(`{"name":"The Crown","venue_type":"pub","address":"High Street"}`))
	create.Header.Set("Authorization", "Bearer valid")
	created := httptest.NewRecorder()
	mux.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || created.Header().Get("Location") != "/api/venues/created-1" {
		t.Fatalf("create status=%d location=%q body=%s", created.Code, created.Header().Get("Location"), created.Body)
	}
	var createdVenue venue
	if err := json.Unmarshal(created.Body.Bytes(), &createdVenue); err != nil || createdVenue.ID != "created-1" {
		t.Fatalf("create response venue=%+v err=%v", createdVenue, err)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/venues/created-1", strings.NewReader(`{"name":"The New Crown"}`))
	patch.Header.Set("Authorization", "Bearer valid")
	patched := httptest.NewRecorder()
	mux.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patched.Code, patched.Body)
	}
	var patchedVenue venue
	if err := json.Unmarshal(patched.Body.Bytes(), &patchedVenue); err != nil || patchedVenue.Name != "The New Crown" {
		t.Fatalf("patch response venue=%+v err=%v", patchedVenue, err)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/venues/created-1", nil)
	deleteRequest.Header.Set("Authorization", "Bearer valid")
	deleted := httptest.NewRecorder()
	mux.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body)
	}

	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/venues", nil))
	var page listResponse
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if list.Code != http.StatusOK || len(page.Venues) != 0 || reader.writeCalls != 3 {
		t.Fatalf("after CRUD list status=%d venues=%+v writes=%d", list.Code, page.Venues, reader.writeCalls)
	}
}

func TestVenueRoutesReturnNotFoundForMissingItems(t *testing.T) {
	reader := &testReader{canManage: true}
	endpoint := &Endpoint{Reader: reader, Auth: testVerifier{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/venues/missing", ""},
		{http.MethodPatch, "/api/venues/missing", `{"name":"Renamed"}`},
		{http.MethodDelete, "/api/venues/missing", ""},
	} {
		t.Run(test.method, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.method != http.MethodGet {
				request.Header.Set("Authorization", "Bearer valid")
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s, want 404", response.Code, response.Body)
			}
		})
	}
}

func TestVenueStreamRouteSnapshotsChangesAndReconnects(t *testing.T) {
	codec, err := sse.NewCursorCodec([]byte("test-only-cursor-signing-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	close(ready)
	reader := &testReader{canManage: true, snapshot: venuecache.Snapshot{
		State:  basestore.SyncState{Generation: "generation-1", Sequence: 1},
		Venues: []venuecache.VenueProjection{{ID: "venue-1", Name: "Initial Pub"}},
	}}
	hub := sse.NewHub()
	endpoint := &Endpoint{Reader: reader, Ready: ready, Hub: hub, Cursors: codec, Auth: testVerifier{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	defer hub.Shutdown()

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodGet, server.URL+"/api/venues/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamHTTPResponse, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	if streamHTTPResponse.StatusCode != http.StatusOK {
		t.Fatalf("stream status=%d", streamHTTPResponse.StatusCode)
	}
	streamReader := bufio.NewReader(streamHTTPResponse.Body)
	initialEvent := readSSEEvent(t, streamReader)
	if initialEvent.name != "venues" {
		t.Fatalf("initial event name=%q, want venues", initialEvent.name)
	}
	initialCursor, err := eventCursor(codec, initialEvent.id)
	if err != nil || initialCursor.Sequence != 1 {
		t.Fatalf("initial cursor claims=%+v err=%v", initialCursor, err)
	}
	var initial streamResponse
	if err := json.Unmarshal([]byte(initialEvent.data), &initial); err != nil || len(initial.Venues) != 1 || initial.Venues[0].ID != "venue-1" {
		t.Fatalf("initial collection=%+v err=%v", initial, err)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/venues", strings.NewReader(`{"name":"New Pub","venue_type":"pub","address":"Main Road"}`))
	create.Header.Set("Authorization", "Bearer valid")
	created := httptest.NewRecorder()
	mux.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body)
	}
	changeEvent := readSSEEvent(t, streamReader)
	if changeEvent.name != "venues" {
		t.Fatalf("change event name=%q, want venues", changeEvent.name)
	}
	changeCursor, err := eventCursor(codec, changeEvent.id)
	if err != nil || changeCursor.Sequence != 2 {
		t.Fatalf("changed cursor claims=%+v err=%v", changeCursor, err)
	}
	var changed streamResponse
	if err := json.Unmarshal([]byte(changeEvent.data), &changed); err != nil || len(changed.Venues) != 2 {
		t.Fatalf("changed collection=%+v err=%v", changed, err)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/venues/created-1", strings.NewReader(`{"name":"Renamed Pub"}`))
	patch.Header.Set("Authorization", "Bearer valid")
	patched := httptest.NewRecorder()
	mux.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patched.Code, patched.Body)
	}
	patchEvent := readSSEEvent(t, streamReader)
	var afterPatch streamResponse
	if err := json.Unmarshal([]byte(patchEvent.data), &afterPatch); err != nil || len(afterPatch.Venues) != 2 || afterPatch.Venues[1].Name != "Renamed Pub" {
		t.Fatalf("patched collection=%+v err=%v", afterPatch, err)
	}
	patchCursor, err := eventCursor(codec, patchEvent.id)
	if err != nil || patchCursor.Sequence != 3 {
		t.Fatalf("patch cursor claims=%+v err=%v", patchCursor, err)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/venues/created-1", nil)
	deleteRequest.Header.Set("Authorization", "Bearer valid")
	deleted := httptest.NewRecorder()
	mux.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body)
	}
	deleteEvent := readSSEEvent(t, streamReader)
	var afterDelete streamResponse
	if err := json.Unmarshal([]byte(deleteEvent.data), &afterDelete); err != nil || len(afterDelete.Venues) != 1 || afterDelete.Venues[0].ID != "venue-1" {
		t.Fatalf("deleted collection=%+v err=%v", afterDelete, err)
	}
	deleteCursor, err := eventCursor(codec, deleteEvent.id)
	if err != nil || deleteCursor.Sequence != 4 {
		t.Fatalf("delete cursor claims=%+v err=%v", deleteCursor, err)
	}
	cancelStream()
	_ = streamHTTPResponse.Body.Close()

	reconnectCtx, cancelReconnect := context.WithCancel(context.Background())
	defer cancelReconnect()
	reconnectRequest, err := http.NewRequestWithContext(reconnectCtx, http.MethodGet, server.URL+"/api/venues/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	reconnectRequest.Header.Set("Last-Event-ID", initialEvent.id)
	reconnected, err := http.DefaultClient.Do(reconnectRequest)
	if err != nil {
		t.Fatal(err)
	}
	if reconnected.StatusCode != http.StatusOK {
		t.Fatalf("reconnect with valid Last-Event-ID status=%d", reconnected.StatusCode)
	}
	reconnectEvent := readSSEEvent(t, bufio.NewReader(reconnected.Body))
	if reconnectEvent.name != "venues" {
		t.Fatalf("reconnect event name=%q, want venues", reconnectEvent.name)
	}
	resumedCursor, err := eventCursor(codec, reconnectEvent.id)
	if err != nil || resumedCursor.Sequence != 4 {
		t.Fatalf("reconnect cursor claims=%+v err=%v", resumedCursor, err)
	}
	var resumed streamResponse
	if err := json.Unmarshal([]byte(reconnectEvent.data), &resumed); err != nil || len(resumed.Venues) != 1 || resumed.Venues[0].ID != "venue-1" {
		t.Fatalf("reconnected collection=%+v err=%v", resumed, err)
	}
	cancelReconnect()
	_ = reconnected.Body.Close()
}

func TestVenueStreamRouteRejectsInvalidCursorAndWaitsForReadiness(t *testing.T) {
	codec, err := sse.NewCursorCodec([]byte("test-only-cursor-signing-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	reader := &testReader{snapshot: venuecache.Snapshot{State: basestore.SyncState{Generation: "generation-1", Sequence: 1}}}
	hub := sse.NewHub()
	defer hub.Shutdown()
	ready := make(chan struct{})
	endpoint := &Endpoint{Reader: reader, Ready: ready, Hub: hub, Cursors: codec}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/venues/stream", nil)
	invalidRequest.Header.Set("Last-Event-ID", "not-a-signed-cursor")
	invalid := httptest.NewRecorder()
	mux.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d body=%s, want 400", invalid.Code, invalid.Body)
	}

	starting := httptest.NewRecorder()
	mux.ServeHTTP(starting, httptest.NewRequest(http.MethodGet, "/api/venues/stream", nil))
	if starting.Code != http.StatusServiceUnavailable || starting.Header().Get("Retry-After") != "1" {
		t.Fatalf("not-ready stream status=%d retry-after=%q body=%s", starting.Code, starting.Header().Get("Retry-After"), starting.Body)
	}
}

type sseEvent struct {
	id   string
	name string
	data string
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) sseEvent {
	t.Helper()
	var event sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return event
		}
		field, value, _ := strings.Cut(line, ": ")
		switch field {
		case "id":
			event.id = value
		case "event":
			event.name = value
		case "data":
			event.data = value
		}
	}
}

func eventCursor(codec *sse.CursorCodec, token string) (sse.CursorClaims, error) {
	return codec.Validate(token, sse.CursorExpectation{Stream: "venues", Resource: "all", Generation: "generation-1", HighWater: 100})
}

func TestDecodeWriteRejectsInvalidVenueFields(t *testing.T) {
	for _, body := range []string{
		`{"name":"Pub","venue_type":"pub","address":"Road","unknown":true}`,
		`{"name":"Pub","venue_type":"pub","address":"Road","food":"yes"}`,
		`{"name":"Pub","venue_type":"pub","address":"Road","website_url":"/relative"}`,
		`{"name":"Pub","venue_type":"pub","address":"Road","next_occurrence_date":"27/10/2026"}`,
		`{"name":"Pub","venue_type":"pub","address":"Road","recurrence":{"frequency":"invalid"}}`,
		`{"name":"Pub","venue_type":"pub","address":"Road"} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/venues", strings.NewReader(body))
			if _, err := decodeWrite(request, false); err == nil {
				t.Fatalf("decodeWrite accepted invalid body %s", body)
			}
		})
	}
}

func mustCursor(t *testing.T, endpoint *Endpoint, query string) *string {
	t.Helper()
	response := httptest.NewRecorder()
	endpoint.list(response, httptest.NewRequest("GET", "/api/venues?"+query, nil))
	var page listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	return page.NextCursor
}

func mustCursorForSnapshot(t *testing.T, endpoint *Endpoint) *string {
	t.Helper()
	response := httptest.NewRecorder()
	endpoint.list(response, httptest.NewRequest("GET", "/api/venues?limit=2", nil))
	var page listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	return page.NextCursor
}

func TestStreamSnapshotCarriesVenueRelationshipFields(t *testing.T) {
	codec, err := sse.NewCursorCodec([]byte("test-only-cursor-signing-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &Endpoint{Reader: &testReader{snapshot: venuecache.Snapshot{
		State:  basestore.SyncState{Generation: "generation-1", Sequence: 9},
		Venues: []venuecache.VenueProjection{{ID: "venue-1", Name: "Pub", VisibleGroupIDs: []string{"group-1"}, PreviousVenueIDs: []string{"venue-old"}}},
	}}, Cursors: codec}
	snapshot, err := endpoint.snapshot(context.Background(), sse.CursorInput{})
	if err != nil {
		t.Fatal(err)
	}
	var response streamResponse
	if err := json.Unmarshal(snapshot.Data, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Venues) != 1 || response.Venues[0].VisibleGroupIDs[0] != "group-1" || response.Venues[0].PreviousVenueIDs[0] != "venue-old" || response.Cursor != snapshot.Cursor {
		t.Fatalf("stream payload=%+v, event cursor=%q", response, snapshot.Cursor)
	}
}

func TestVenueWriteChangeWakesCollectionStream(t *testing.T) {
	hub := sse.NewHub()
	subscription := hub.Register("venues:all", "venues:public", "")
	defer subscription.Unregister()
	endpoint := &Endpoint{Reader: &testReader{snapshot: venuecache.Snapshot{State: basestore.SyncState{Generation: "generation-1", Sequence: 2}}}, Hub: hub}
	endpoint.publishVenueChange(context.Background())
	select {
	case <-subscription.Changed():
		if sequence := subscription.TakeChanged(); sequence != 2 {
			t.Fatalf("published sequence=%d, want 2", sequence)
		}
	default:
		t.Fatal("venue write did not wake collection stream")
	}
}
