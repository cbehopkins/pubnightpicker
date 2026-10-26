package venues

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
)

type testReader struct {
	snapshot venuecache.Snapshot
}

func (r *testReader) Get(context.Context, string) (venuecache.VenueProjection, error) {
	return venuecache.VenueProjection{}, venuecache.ErrNotFound
}
func (r *testReader) ListSnapshot(context.Context) (venuecache.Snapshot, error) {
	return r.snapshot, nil
}
func (r *testReader) CanManage(context.Context, string) (bool, error) { return true, nil }
func (r *testReader) Create(context.Context, map[string]any) (venuecache.VenueProjection, error) {
	return venuecache.VenueProjection{}, nil
}
func (r *testReader) Patch(context.Context, string, map[string]any) (venuecache.VenueProjection, error) {
	return venuecache.VenueProjection{}, nil
}
func (r *testReader) Delete(context.Context, string) error { return nil }

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
