package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/venuecache"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

type baselineThenPauseVenueSource struct {
	venuecache.Source
	release <-chan struct{}
	paused  chan struct{}
}

func (source *baselineThenPauseVenueSource) Watch(ctx context.Context) (venuecache.ChangeStream, error) {
	stream, err := source.Source.Watch(ctx)
	if err != nil {
		return nil, err
	}
	return &baselineThenPauseVenueStream{ctx: ctx, source: stream, release: source.release, paused: source.paused}, nil
}

type baselineThenPauseVenueStream struct {
	ctx      context.Context
	source   venuecache.ChangeStream
	release  <-chan struct{}
	paused   chan struct{}
	baseline bool
	once     sync.Once
}

func (stream *baselineThenPauseVenueStream) Next() ([]venuecache.Change, error) {
	if !stream.baseline {
		changes, err := stream.source.Next()
		if err == nil {
			stream.baseline = true
		}
		return changes, err
	}
	stream.once.Do(func() { close(stream.paused) })
	select {
	case <-stream.release:
		return stream.source.Next()
	case <-stream.ctx.Done():
		return nil, stream.ctx.Err()
	}
}

func (stream *baselineThenPauseVenueStream) Stop() {
	stream.source.Stop()
}

func TestCurrentMeetDoesNotReportVenueMissingWhileWatchLags(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-meet-venue-lag-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })
	group := firestoreClient.Collection("groups").Doc("public-group")
	if _, err := group.Set(ctx, map[string]any{"name": "Public Group", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	venue := firestoreClient.Collection("pubs").Doc("late-venue")
	meet := firestoreClient.Collection("polls").Doc("late-meet")
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = group.Delete(cleanupCtx)
		_, _ = venue.Delete(cleanupCtx)
		_, _ = meet.Delete(cleanupCtx)
	})

	venueSource, err := venuecache.NewFirestoreSource(firestoreClient)
	if err != nil {
		t.Fatal(err)
	}
	releaseWatch, pausedWatch := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWatch) }) }
	t.Cleanup(release)
	config := testConfig(t, t.TempDir()+"/venue-watch-lag.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.Component = "handpump"
	config.LandlordInternalURL = "http://127.0.0.1:8082"
	config.InternalAPIToken = "test-only-internal-token-0123456789"
	config.Clock = &meetTestClock{current: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	config.VenueSource = &baselineThenPauseVenueSource{Source: venueSource, release: releaseWatch, paused: pausedWatch}
	config.GroupSource = nil
	config.HTTPAddr = "127.0.0.1:0"
	config.AllowAuthEmulator = true
	application, err := app.New(config)
	if err != nil {
		t.Fatalf("new HandPump app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	runCtx, stop := context.WithCancel(ctx)
	runDone := make(chan error, 1)
	go func() { runDone <- application.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("stop app: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("app did not stop")
		}
	})

	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	select {
	case <-pausedWatch:
	case <-ctx.Done():
		t.Fatal("Venue watch did not pause after its initial baseline")
	}
	if _, err := venue.Set(ctx, map[string]any{"name": "The Late Crown", "venueType": "pub"}); err != nil {
		t.Fatal(err)
	}
	if _, err := meet.Set(ctx, map[string]any{
		"date": "2026-11-20", "completed": true, "groupId": "public-group", "selected": "late-venue",
	}); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, baseURL+"/api/meets/public-group/current", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer valid-token")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("current-Meets GET while Venue watch lags status=%d body=%s", response.StatusCode, readVenueResponseBody(t, response))
	}
	var snapshot struct {
		Meets []struct {
			ID    string `json:"id"`
			Venue struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"venue"`
		} `json:"meets"`
	}
	if err := json.NewDecoder(bufio.NewReader(response.Body)).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Meets) != 1 || snapshot.Meets[0].ID != "late-meet" ||
		snapshot.Meets[0].Venue.ID != "late-venue" || snapshot.Meets[0].Venue.Name != "The Late Crown" || snapshot.Meets[0].Venue.Status != "active" {
		t.Fatalf("current-Meets snapshot=%+v, want the late Venue details rather than a missing reference", snapshot)
	}
	release()
}
