package app_test

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/groupcache"
	"last_orders/internal/lastorders/components/groupcache/groupcachetest"
)

func TestRunValidatesInitialGroupsForEveryComponent(t *testing.T) {
	invalidSnapshots := []struct {
		name string
		docs []groupcache.Document
		want string
	}{
		{name: "empty collection", want: "authoritative Groups collection is empty"},
		{name: "missing name", docs: []groupcache.Document{{ID: "missing-name", Data: map[string]any{"visibility": "public"}}}, want: "non-empty string"},
		{name: "empty name", docs: []groupcache.Document{{ID: "empty-name", Data: map[string]any{"name": "", "visibility": "public"}}}, want: "non-empty string"},
		{name: "missing visibility", docs: []groupcache.Document{{ID: "missing-visibility", Data: map[string]any{"name": "North"}}}, want: "visibility"},
		{name: "unsupported visibility", docs: []groupcache.Document{{ID: "unsupported", Data: map[string]any{"name": "North", "visibility": "members"}}}, want: "unsupported value"},
		{name: "duplicate exact names", docs: []groupcache.Document{
			{ID: "north-1", Data: map[string]any{"name": "North", "visibility": "public"}},
			{ID: "north-2", Data: map[string]any{"name": "North", "visibility": "private"}},
		}, want: "duplicate exact Group name"},
	}
	components := []string{"all", "landlord", "handpump"}
	for _, component := range components {
		for _, snapshot := range invalidSnapshots {
			t.Run(component+"/"+snapshot.name, func(t *testing.T) {
				config := testConfig(t, filepath.Join(t.TempDir(), "invalid-groups.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
				config.Component = component
				config.GroupSource = groupcachetest.NewWithDocuments(snapshot.docs...)
				configureComponentForStartupTest(&config)
				application, err := app.New(config)
				if err != nil {
					t.Fatal(err)
				}
				defer application.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				err = application.Run(ctx)
				if err == nil || !strings.Contains(err.Error(), "group cache projection failed") || !strings.Contains(err.Error(), snapshot.want) {
					t.Fatalf("Run error = %v, want Group baseline failure containing %q", err, snapshot.want)
				}
			})
		}
	}
}

func configureComponentForStartupTest(config *app.Config) {
	token := strings.Repeat("t", 48)
	switch config.Component {
	case "landlord":
		config.InternalHTTPAddr = "127.0.0.1:0"
		config.InternalAPIToken = token
	case "handpump":
		config.LandlordInternalURL = "http://127.0.0.1:8080"
		config.InternalAPIToken = token
	}
}

func TestRunDoesNotUseStaleLocalGroupsAsAuthoritativeBaseline(t *testing.T) {
	config := testConfig(t, filepath.Join(t.TempDir(), "stale-groups.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.HTTPAddr = "127.0.0.1:0"
	config.GroupSource = groupcachetest.NewWithDocuments()

	db, err := sql.Open("sqlite", config.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := groupcache.NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	stale := groupcache.Group{ID: "stale-id", Name: "Stale", Visibility: groupcache.VisibilityPublic}
	if err := store.ApplyBatch(context.Background(), []groupcache.Mutation{{ID: stale.ID, Group: &stale}}, false); err != nil {
		t.Fatal(err)
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}

	application, err := app.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := application.Run(ctx); err == nil || !strings.Contains(err.Error(), "authoritative Groups collection is empty") {
		t.Fatalf("Run error = %v, want empty authoritative Groups failure", err)
	}
	client := &http.Client{Timeout: 100 * time.Millisecond}
	response, requestErr := client.Get("http://" + application.HTTPAddr() + "/healthz")
	if requestErr == nil {
		response.Body.Close()
		t.Fatalf("health endpoint returned %d before the Group baseline was validated", response.StatusCode)
	}
}

func TestDifferentCaseGroupNamesAreValidAtStartup(t *testing.T) {
	config := testConfig(t, filepath.Join(t.TempDir(), "case-groups.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.GroupSource = groupcachetest.NewWithDocuments(
		groupcache.Document{ID: "upper", Data: map[string]any{"name": "North", "visibility": "public"}},
		groupcache.Document{ID: "lower", Data: map[string]any{"name": "north", "visibility": "public"}},
	)
	application, err := app.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = application.Run(ctx)
	if err != nil {
		t.Fatalf("Run error = %v, want startup to accept different-case names", err)
	}
}
