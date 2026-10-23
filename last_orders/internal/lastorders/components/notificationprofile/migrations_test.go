package notificationprofile

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/venuecache"
)

func profileSyncState(t *testing.T, base *basestore.Store) basestore.SyncState {
	t.Helper()
	tx, err := base.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state, err := base.ReadSyncState(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestMigrationRebuildsOnlyLegacyNotificationProjection(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := venues.Put(ctx, venuecache.VenueProjection{ID: "venue", Name: "Keep venue"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE notification_user_prefs(user_id TEXT PRIMARY KEY, updated_at DATETIME NOT NULL);
		INSERT INTO notification_user_prefs VALUES('legacy', CURRENT_TIMESTAMP);
		CREATE TABLE notification_endpoints(user_id TEXT, endpoint_id TEXT);
		INSERT INTO notification_endpoints VALUES('legacy', 'device');
		CREATE TABLE unrelated(value TEXT NOT NULL);
		INSERT INTO unrelated VALUES('durable work');
	`); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, base)
	store, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	after := profileSyncState(t, base)
	if before.Generation == after.Generation || before.Sequence != after.Sequence {
		t.Fatalf("migration metadata = %+v; previous %+v", after, before)
	}
	if _, err := store.Preferences(ctx, "legacy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy preferences error = %v", err)
	}
	for _, table := range []string{"notification_user_prefs", "notification_public_users", "notification_endpoints", "notification_profiles"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s row count = %d, %v; want 0", table, count, err)
		}
	}
	if got, err := venues.Get(ctx, "venue"); err != nil || got.Name != "Keep venue" {
		t.Fatalf("venue after migration = %+v, %v", got, err)
	}
	var version int64
	if err := db.QueryRow(`SELECT change_version FROM venue_cache WHERE venue_id = 'venue'`).Scan(&version); err != nil || version != before.Sequence {
		t.Fatalf("venue version = %d, %v; want %d", version, err, before.Sequence)
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM unrelated`).Scan(&value); err != nil || value != "durable work" {
		t.Fatalf("unrelated data = %q, %v", value, err)
	}
	seedUser(t, store, UserPreferences{UserID: "new", WebPushEnabled: true})
	if _, err := New(base); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, base); got != after {
		t.Fatalf("repeat migration changed metadata: %+v; want %+v", got, after)
	}
	if _, err := store.Preferences(ctx, "new"); err != nil {
		t.Fatalf("repeat migration cleared new data: %v", err)
	}
}

func TestMigrationRollsBackNotificationRowsSchemaAndGeneration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE notification_user_prefs(user_id TEXT PRIMARY KEY);
		INSERT INTO notification_user_prefs VALUES('legacy');
		CREATE TABLE notification_profile_schema(singleton INTEGER PRIMARY KEY, version INTEGER NOT NULL);
		CREATE TRIGGER reject_profile_migration BEFORE UPDATE OF version ON notification_profile_schema
		BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, base)
	if _, err := New(base); err == nil {
		t.Fatal("migration succeeded despite injected failure")
	}
	if got := profileSyncState(t, base); got != before {
		t.Fatalf("failed migration changed metadata: %+v; want %+v", got, before)
	}
	var userID string
	if err := db.QueryRow(`SELECT user_id FROM notification_user_prefs`).Scan(&userID); err != nil || userID != "legacy" {
		t.Fatalf("legacy row after rollback = %q, %v", userID, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_profile_schema`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("committed migration markers = %d, %v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'notification_public_users'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("new tables survived rollback: %d, %v", count, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_profile_migration`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(base); err != nil {
		t.Fatalf("migration retry: %v", err)
	}
}

func TestMigrationFreshCreationPreservesGeneration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, base)
	for range 2 {
		if _, err := New(base); err != nil {
			t.Fatal(err)
		}
		if got := profileSyncState(t, base); got != before {
			t.Fatalf("fresh startup changed metadata: %+v; want %+v", got, before)
		}
	}
}

func TestMigrationRejectsUnknownVersionWithoutChangingData(t *testing.T) {
	store := newTestStore(t)
	seedUser(t, store, UserPreferences{UserID: "keep"})
	before := profileSyncState(t, store.base)
	if _, err := store.base.DB().Exec(`UPDATE notification_profile_schema SET version = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(store.base); err == nil {
		t.Fatal("unknown schema version was accepted")
	}
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("rejected migration changed metadata: %+v; want %+v", got, before)
	}
	if _, err := store.Preferences(context.Background(), "keep"); err != nil {
		t.Fatalf("rejected migration changed preferences: %v", err)
	}
}

func TestMigrationReopenRetainsContributionsAndProfileVersions(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "reopen.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	initial := profileSyncState(t, base)
	if _, err := New(base); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, base); got != initial {
		t.Fatalf("fresh migration changed generation: %+v; want %+v", got, initial)
	}
	private := PrivateUserContribution{UserID: "user"}
	public := PublicUserContribution{UserID: "user", Name: NullableField[string]{Present: true}}
	endpoint := EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth"}
	if err := putPrivateUser(ctx, db, private); err != nil {
		t.Fatal(err)
	}
	if err := putPublicUser(ctx, db, public); err != nil {
		t.Fatal(err)
	}
	if err := putEndpointContribution(ctx, db, endpoint); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	version, err := base.NextSequence(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_profiles VALUES('user', ?, 0)`, version); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, base)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	base, err = basestore.New(reopened)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(base); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, base); got != before {
		t.Fatalf("reopen changed metadata: %+v; want %+v", got, before)
	}
	if got, _, err := readPrivateUser(ctx, reopened, "user"); err != nil || !got.Equal(private) {
		t.Fatalf("reopened private contribution = %+v, %v", got, err)
	}
	if got, err := readPublicUser(ctx, reopened, "user"); err != nil || !got.Equal(public) {
		t.Fatalf("reopened public contribution = %+v, %v", got, err)
	}
	if got, _, err := readEndpointContribution(ctx, reopened, "user", "device"); err != nil || !got.Equal(endpoint) {
		t.Fatalf("reopened endpoint contribution = %+v, %v", got, err)
	}
	var gotVersion int64
	var deleted bool
	if err := reopened.QueryRow(`SELECT change_version, deleted FROM notification_profiles WHERE user_id = 'user'`).Scan(&gotVersion, &deleted); err != nil || gotVersion != version || deleted {
		t.Fatalf("reopened profile = version %d, deleted %v, %v", gotVersion, deleted, err)
	}
}
