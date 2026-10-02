package notificationprofile

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"last_orders/internal/lastorders/basestore"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	store, err := New(base)
	if err != nil {
		t.Fatalf("new notification profile store: %v", err)
	}
	return store
}

func seedUser(t *testing.T, store *Store, preferences UserPreferences) {
	t.Helper()
	if err := store.PutPreferences(context.Background(), preferences); err != nil {
		t.Fatalf("put preferences for %q: %v", preferences.UserID, err)
	}
}

func seedEndpoint(t *testing.T, store *Store, endpoint Endpoint) {
	t.Helper()
	if endpoint.URL == "" {
		endpoint.URL = "https://push.test/" + endpoint.EndpointID
	}
	if endpoint.P256DH == "" {
		endpoint.P256DH = "p256dh"
	}
	if endpoint.Auth == "" {
		endpoint.Auth = "auth"
	}
	if err := store.PutEndpoint(context.Background(), endpoint); err != nil {
		t.Fatalf("put endpoint %q: %v", endpoint.EndpointID, err)
	}
}

func endpointKeys(endpoints []Endpoint) []string {
	keys := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		keys = append(keys, endpoint.Key())
	}
	return keys
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestEligibleEndpointsFilteringMatrix(t *testing.T) {
	store := newTestStore(t)

	// Fully opted in.
	seedUser(t, store, UserPreferences{UserID: "keen", WebPushEnabled: true, PollOpens: true, PollCompletes: true, GlobalChat: true, EventChat: true})
	seedEndpoint(t, store, Endpoint{UserID: "keen", EndpointID: "a", Active: true})
	// Second device for the same user.
	seedEndpoint(t, store, Endpoint{UserID: "keen", EndpointID: "b", Active: true})
	// Master switch off.
	seedUser(t, store, UserPreferences{UserID: "master-off", WebPushEnabled: false, PollOpens: true, PollCompletes: true})
	seedEndpoint(t, store, Endpoint{UserID: "master-off", EndpointID: "a", Active: true})
	// Opted out of poll opens only.
	seedUser(t, store, UserPreferences{UserID: "no-opens", WebPushEnabled: true, PollOpens: false, PollCompletes: true})
	seedEndpoint(t, store, Endpoint{UserID: "no-opens", EndpointID: "a", Active: true})
	// Opted in but the endpoint is inactive.
	seedUser(t, store, UserPreferences{UserID: "stale", WebPushEnabled: true, PollOpens: true, PollCompletes: true})
	seedEndpoint(t, store, Endpoint{UserID: "stale", EndpointID: "a", Active: false})
	// Endpoint with no projected preferences at all.
	seedEndpoint(t, store, Endpoint{UserID: "unknown", EndpointID: "a", Active: true})

	tests := []struct {
		name     string
		selector Selector
		want     []string
	}{
		{
			name:     "poll opens excludes opt-outs, inactive endpoints and unknown users",
			selector: Selector{Kind: KindPollOpens},
			want:     []string{"keen/a", "keen/b"},
		},
		{
			name:     "poll completes includes the poll-opens opt-out",
			selector: Selector{Kind: KindPollCompletes},
			want:     []string{"keen/a", "keen/b", "no-opens/a"},
		},
		{
			name:     "chat defaults leave only the explicitly opted-in user",
			selector: Selector{Kind: KindGlobalChat},
			want:     []string{"keen/a", "keen/b"},
		},
		{
			name:     "diagnostic ignores type preferences but requires the master switch and an active endpoint",
			selector: Selector{Kind: KindDiagnostic, UserIDs: []string{"keen", "no-opens", "master-off", "stale", "unknown"}},
			want:     []string{"keen/a", "keen/b", "no-opens/a"},
		},
		{
			name:     "explicit audience narrows an otherwise eligible set",
			selector: Selector{Kind: KindPollCompletes, UserIDs: []string{"no-opens"}},
			want:     []string{"no-opens/a"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := store.EligibleEndpoints(context.Background(), test.selector)
			if err != nil {
				t.Fatalf("eligible endpoints: %v", err)
			}
			if !equalStrings(endpointKeys(got), test.want) {
				t.Fatalf("endpoints = %v; want %v", endpointKeys(got), test.want)
			}
		})
	}
}

func TestEligibleEndpointsRejectsUnknownKind(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.EligibleEndpoints(context.Background(), Selector{Kind: "nonsense"}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("error = %v; want ErrUnknownKind", err)
	}
}

func TestPutIsIdempotentAndUpdatesInPlace(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	preferences := UserPreferences{UserID: "user", WebPushEnabled: true, PollOpens: true, EventChatMutedPollIDs: []string{"poll-1"}}
	seedUser(t, store, preferences)
	seedUser(t, store, preferences)

	endpoint := Endpoint{UserID: "user", EndpointID: "a", URL: "https://push.test/a", P256DH: "p", Auth: "x", Active: true}
	seedEndpoint(t, store, endpoint)
	seedEndpoint(t, store, endpoint)

	got, err := store.EligibleEndpoints(ctx, Selector{Kind: KindPollOpens})
	if err != nil {
		t.Fatalf("eligible endpoints: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("endpoints = %d; want 1 after repeated writes", len(got))
	}

	// A re-projection of the same document replaces rather than duplicates.
	endpoint.URL = "https://push.test/rotated"
	seedEndpoint(t, store, endpoint)
	got, err = store.EligibleEndpoints(ctx, Selector{Kind: KindPollOpens})
	if err != nil {
		t.Fatalf("eligible endpoints: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://push.test/rotated" {
		t.Fatalf("endpoints = %+v; want a single rotated URL", got)
	}

	stored, err := store.Preferences(ctx, "user")
	if err != nil {
		t.Fatalf("preferences: %v", err)
	}
	if !equalStrings(stored.EventChatMutedPollIDs, []string{"poll-1"}) {
		t.Fatalf("muted poll ids = %v; want [poll-1]", stored.EventChatMutedPollIDs)
	}
}

func TestSetEndpointActiveAndDeletes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedUser(t, store, UserPreferences{UserID: "user", WebPushEnabled: true, PollOpens: true})
	seedEndpoint(t, store, Endpoint{UserID: "user", EndpointID: "a", Active: true})
	seedEndpoint(t, store, Endpoint{UserID: "user", EndpointID: "b", Active: true})

	if err := store.SetEndpointActive(ctx, "user", "a", false); err != nil {
		t.Fatalf("set endpoint active: %v", err)
	}
	got, err := store.EligibleEndpoints(ctx, Selector{Kind: KindPollOpens})
	if err != nil {
		t.Fatalf("eligible endpoints: %v", err)
	}
	if !equalStrings(endpointKeys(got), []string{"user/b"}) {
		t.Fatalf("endpoints = %v; want [user/b]", endpointKeys(got))
	}

	if err := store.DeleteEndpoint(ctx, "user", "b"); err != nil {
		t.Fatalf("delete endpoint: %v", err)
	}
	if got, err = store.EligibleEndpoints(ctx, Selector{Kind: KindPollOpens}); err != nil || len(got) != 0 {
		t.Fatalf("endpoints = %v, err = %v; want none", got, err)
	}

	if err := store.DeletePreferences(ctx, "user"); err != nil {
		t.Fatalf("delete preferences: %v", err)
	}
	if _, err := store.Preferences(ctx, "user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v; want ErrNotFound", err)
	}
}

func TestPreferencesRoundTripsEmptyMutedList(t *testing.T) {
	store := newTestStore(t)
	seedUser(t, store, UserPreferences{UserID: "user", WebPushEnabled: true})
	stored, err := store.Preferences(context.Background(), "user")
	if err != nil {
		t.Fatalf("preferences: %v", err)
	}
	if len(stored.EventChatMutedPollIDs) != 0 {
		t.Fatalf("muted poll ids = %v; want empty", stored.EventChatMutedPollIDs)
	}
}

func TestEmailRecipientsSelectsOptedInUsersWithAddresses(t *testing.T) {
	store := newTestStore(t)
	seedUser(t, store, UserPreferences{UserID: "both", NotificationEmail: "both@example.com", OpenPollEmailEnabled: true, NotificationEmailEnabled: true})
	seedUser(t, store, UserPreferences{UserID: "opens", NotificationEmail: "opens@example.com", OpenPollEmailEnabled: true, WebPushEnabled: true})
	seedUser(t, store, UserPreferences{UserID: "completes", NotificationEmail: "completes@example.com", NotificationEmailEnabled: true})
	seedUser(t, store, UserPreferences{UserID: "blank", OpenPollEmailEnabled: true, NotificationEmailEnabled: true})
	seedUser(t, store, UserPreferences{UserID: "push-only", NotificationEmail: "push@example.com", WebPushEnabled: true, PollOpens: true})

	tests := []struct {
		kind EmailKind
		want []string
	}{
		{kind: EmailPollOpens, want: []string{"both@example.com", "opens@example.com"}},
		{kind: EmailPollCompletes, want: []string{"both@example.com", "completes@example.com"}},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			recipients, err := store.EmailRecipients(context.Background(), test.kind)
			if err != nil {
				t.Fatalf("email recipients: %v", err)
			}
			got := make([]string, 0, len(recipients))
			for _, recipient := range recipients {
				got = append(got, recipient.Email)
			}
			if !equalStrings(got, test.want) {
				t.Fatalf("recipients = %v; want %v", got, test.want)
			}
		})
	}

	if _, err := store.EmailRecipients(context.Background(), "nonsense"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("error = %v; want ErrUnknownKind", err)
	}
}

func TestEmailPreferencesRoundTrip(t *testing.T) {
	store := newTestStore(t)
	seedUser(t, store, UserPreferences{UserID: "user", NotificationEmail: "user@example.com", OpenPollEmailEnabled: true})
	stored, err := store.Preferences(context.Background(), "user")
	if err != nil {
		t.Fatalf("preferences: %v", err)
	}
	if stored.NotificationEmail != "user@example.com" || !stored.OpenPollEmailEnabled || stored.NotificationEmailEnabled {
		t.Fatalf("stored = %+v; want email preferences preserved", stored)
	}
}

func TestNewAddsEmailColumnsToExistingSchema(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "profile.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE notification_user_prefs (
			user_id TEXT PRIMARY KEY,
			web_push_enabled INTEGER NOT NULL DEFAULT 0,
			poll_opens INTEGER NOT NULL DEFAULT 1,
			poll_completes INTEGER NOT NULL DEFAULT 1,
			global_chat INTEGER NOT NULL DEFAULT 0,
			event_chat INTEGER NOT NULL DEFAULT 0,
			muted_poll_ids_json TEXT NOT NULL DEFAULT '[]',
			updated_at DATETIME NOT NULL
		);
		INSERT INTO notification_user_prefs(user_id, updated_at) VALUES('existing', CURRENT_TIMESTAMP);
	`); err != nil {
		t.Fatalf("create previous schema: %v", err)
	}
	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}

	for range 2 {
		if _, err := New(base); err != nil {
			t.Fatalf("new notification profile store: %v", err)
		}
	}

	store, err := New(base)
	if err != nil {
		t.Fatalf("new notification profile store: %v", err)
	}
	existing, err := store.Preferences(context.Background(), "existing")
	if err != nil {
		t.Fatalf("existing preferences: %v", err)
	}
	if existing.NotificationEmail != "" || existing.OpenPollEmailEnabled || existing.NotificationEmailEnabled {
		t.Fatalf("existing = %+v; want email defaults", existing)
	}
}
