package notificationprofile

import (
	"database/sql"
	"errors"
	"testing"
)

func TestPrivateContributionStoragePreservesPresence(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	for _, data := range []map[string]any{
		{},
		{
			"email": "", "notificationEmail": "", "defaultArrivalTime": "19:30",
			"testEmailReq": "request", "testEmailAck": "", "webPushEnabled": false,
			"openPollEmailEnabled": false, "notificationEmailEnabled": true,
			"pushPreferences": map[string]any{
				"pollOpens": false, "pollCompletes": true, "globalChat": false, "eventChat": true,
				"eventChatMutedPollIds": []any{"poll-2", "poll-1", "poll-2"},
			},
		},
		{},
	} {
		want, err := PrivateUserFromDocument(Document{ID: "user", Data: data})
		if err != nil {
			t.Fatal(err)
		}
		if err := putPrivateUser(ctx, store.base.DB(), want); err != nil {
			t.Fatal(err)
		}
		got, updatedAt, err := readPrivateUser(ctx, store.base.DB(), want.UserID)
		if err != nil || !got.Equal(want) || updatedAt.IsZero() {
			t.Fatalf("private round trip = %+v, %v, %v; want %+v", got, updatedAt, err, want)
		}
		preferences, err := store.Preferences(ctx, want.UserID)
		if err != nil || !preferences.Equal(want.Preferences()) || !preferences.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("effective preferences = %+v, %v", preferences, err)
		}
	}
	if _, _, err := readPrivateUser(ctx, store.base.DB(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing private contribution = %v", err)
	}
}

func TestPublicContributionStoragePreservesThreeStates(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	for _, data := range []map[string]any{
		{},
		{"name": nil, "photoUrl": nil, "votesVisible": nil},
		{"name": "", "photoUrl": "", "customPhotoUrl": "", "votesVisible": false},
		{"name": "Public name", "photoUrl": "photo", "customPhotoUrl": "custom", "votesVisible": true},
		{},
	} {
		want, err := PublicUserFromDocument(Document{ID: "user", Data: data})
		if err != nil {
			t.Fatal(err)
		}
		if err := putPublicUser(ctx, store.base.DB(), want); err != nil {
			t.Fatal(err)
		}
		got, err := readPublicUser(ctx, store.base.DB(), want.UserID)
		if err != nil || !got.Equal(want) {
			t.Fatalf("public round trip = %+v, %v; want %+v", got, err, want)
		}
	}
	if _, err := readPublicUser(ctx, store.base.DB(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing public contribution = %v", err)
	}
	if _, _, err := readPrivateUser(ctx, store.base.DB(), "user"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("public contribution established a private row: %v", err)
	}
}

func TestEndpointContributionStoragePreservesPresenceAndOwnerKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	peer := EndpointContribution{UserID: "peer", EndpointID: "device", URL: "peer-url", P256DH: "peer-key", Auth: "peer-auth"}
	if err := putEndpointContribution(ctx, store.base.DB(), peer); err != nil {
		t.Fatal(err)
	}
	enabled, disabled := true, false
	for _, active := range []*bool{nil, &disabled, &enabled, nil} {
		want := EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth", Active: active}
		if err := putEndpointContribution(ctx, store.base.DB(), want); err != nil {
			t.Fatal(err)
		}
		got, updatedAt, err := readEndpointContribution(ctx, store.base.DB(), want.UserID, want.EndpointID)
		if err != nil || !got.Equal(want) || updatedAt.IsZero() {
			t.Fatalf("endpoint round trip = %+v, %v, %v; want %+v", got, updatedAt, err, want)
		}
		other, _, err := readEndpointContribution(ctx, store.base.DB(), peer.UserID, peer.EndpointID)
		if err != nil || !other.Equal(peer) {
			t.Fatalf("endpoint replacement affected another owner: %+v, %v", other, err)
		}
	}
	if _, _, err := readEndpointContribution(ctx, store.base.DB(), "missing", "device"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing endpoint contribution = %v", err)
	}
	var profiles int
	if err := store.base.DB().QueryRow(`SELECT COUNT(*) FROM notification_profiles`).Scan(&profiles); err != nil || profiles != 0 {
		t.Fatalf("orphan endpoints created profile rows: %d, %v", profiles, err)
	}
}

func TestContributionStorageAppliesDefaultsOnlyWhenReading(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	enabled := true
	user := PrivateUserContribution{UserID: "user", WebPushEnabled: &enabled}
	if err := putPrivateUser(ctx, store.base.DB(), user); err != nil {
		t.Fatal(err)
	}
	if err := putPublicUser(ctx, store.base.DB(), PublicUserContribution{UserID: "public-only"}); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"user", "public-only", "orphan"} {
		if err := putEndpointContribution(ctx, store.base.DB(), EndpointContribution{
			UserID: userID, EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth", Active: &enabled,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []Kind{KindPollOpens, KindPollCompletes, KindGlobalChat, KindEventChat, KindDiagnostic} {
		endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if kind == KindGlobalChat || kind == KindEventChat {
			want = 0
		}
		if len(endpoints) != want || (want == 1 && endpoints[0].UserID != "user") {
			t.Fatalf("%s default selection = %+v; want %d endpoints for private user", kind, endpoints, want)
		}
	}
	for _, kind := range []EmailKind{EmailPollOpens, EmailPollCompletes} {
		if recipients, err := store.EmailRecipients(ctx, kind); err != nil || len(recipients) != 0 {
			t.Fatalf("missing email settings enabled delivery: %+v, %v", recipients, err)
		}
	}
	got, _, err := readPrivateUser(ctx, store.base.DB(), user.UserID)
	if err != nil || !got.Equal(user) || got.PollOpens != nil || got.PollCompletes != nil {
		t.Fatalf("reads persisted effective defaults: %+v, %v", got, err)
	}
	disabled := false
	user.PollOpens = &disabled
	if err := putPrivateUser(ctx, store.base.DB(), user); err != nil {
		t.Fatal(err)
	}
	if endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: KindPollOpens}); err != nil || len(endpoints) != 0 {
		t.Fatalf("explicit opt-out ignored: %+v, %v", endpoints, err)
	}
	if err := putEndpointContribution(ctx, store.base.DB(), EndpointContribution{
		UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth",
	}); err != nil {
		t.Fatal(err)
	}
	if endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: KindDiagnostic}); err != nil || len(endpoints) != 0 {
		t.Fatalf("absent endpoint active flag enabled delivery: %+v, %v", endpoints, err)
	}
}

func TestContributionStorageUsesCallerTransaction(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	original := PrivateUserContribution{UserID: "user"}
	if err := putPrivateUser(ctx, store.base.DB(), original); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, store.base)
	tx, err := store.base.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	address := "user@example.com"
	changed := PrivateUserContribution{UserID: "user", Email: &address}
	if err := putPrivateUser(ctx, tx, changed); err != nil {
		t.Fatal(err)
	}
	if err := putPublicUser(ctx, tx, PublicUserContribution{UserID: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := putEndpointContribution(ctx, tx, EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth"}); err != nil {
		t.Fatal(err)
	}
	sequence, err := store.base.NextSequence(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_profiles VALUES('user', ?, 0)`, sequence); err != nil {
		t.Fatal(err)
	}
	if got, _, err := readPrivateUser(ctx, tx, "user"); err != nil || !got.Equal(changed) {
		t.Fatalf("transaction cannot read private write: %+v, %v", got, err)
	}
	if _, err := readPublicUser(ctx, tx, "user"); err != nil {
		t.Fatalf("transaction cannot read public write: %v", err)
	}
	if _, _, err := readEndpointContribution(ctx, tx, "user", "device"); err != nil {
		t.Fatalf("transaction cannot read endpoint write: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got, _, err := readPrivateUser(ctx, store.base.DB(), "user"); err != nil || !got.Equal(original) {
		t.Fatalf("rollback changed private contribution: %+v, %v", got, err)
	}
	if _, err := readPublicUser(ctx, store.base.DB(), "user"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rollback retained public contribution: %v", err)
	}
	if _, _, err := readEndpointContribution(ctx, store.base.DB(), "user", "device"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rollback retained endpoint contribution: %v", err)
	}
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("rollback changed sequence state: %+v; want %+v", got, before)
	}
	var count int
	if err := store.base.DB().QueryRow(`SELECT COUNT(*) FROM notification_profiles`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback retained profile version: %d, %v", count, err)
	}
}

func TestPreferenceUpdatePreservesOtherPrivateFields(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	original, err := PrivateUserFromDocument(Document{ID: "user", Data: map[string]any{
		"email": "private@example.com", "defaultArrivalTime": "19:30",
		"testEmailReq": "request", "testEmailAck": "ack",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := putPrivateUser(ctx, store.base.DB(), original); err != nil {
		t.Fatal(err)
	}
	preferences := original.Preferences()
	preferences.WebPushEnabled = true
	preferences.NotificationEmail = "notify@example.com"
	if err := store.PutPreferences(ctx, preferences); err != nil {
		t.Fatal(err)
	}
	got, _, err := readPrivateUser(ctx, store.base.DB(), original.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if !equalOptional(got.Email, original.Email) || !equalOptional(got.DefaultArrivalTime, original.DefaultArrivalTime) ||
		!equalOptional(got.TestEmailReq, original.TestEmailReq) || !equalOptional(got.TestEmailAck, original.TestEmailAck) {
		t.Fatalf("preference update erased unrelated private fields: %+v", got)
	}
	if !got.Preferences().Equal(preferences) {
		t.Fatalf("preference update was not applied: %+v", got.Preferences())
	}
}

func TestContributionSchemaRejectsInvalidStates(t *testing.T) {
	store := newTestStore(t)
	for _, statement := range []string{
		`INSERT INTO notification_user_prefs(user_id, web_push_enabled, updated_at) VALUES('invalid', 2, CURRENT_TIMESTAMP)`,
		`INSERT INTO notification_endpoints(user_id, endpoint_id, endpoint_url, p256dh, auth, active, updated_at) VALUES('user', 'invalid', 'url', 'key', 'auth', 2, CURRENT_TIMESTAMP)`,
		`INSERT INTO notification_public_users(user_id, name_present, name, photo_url_present, votes_visible_present) VALUES('invalid', 0, 'hidden value', 0, 0)`,
		`INSERT INTO notification_public_users(user_id, name_present, photo_url_present, votes_visible_present, votes_visible) VALUES('invalid', 0, 0, 1, 2)`,
		`INSERT INTO notification_profiles(user_id, change_version, deleted) VALUES('invalid', 0, 0)`,
		`INSERT INTO notification_profiles(user_id, change_version, deleted) VALUES('invalid', 1, 2)`,
	} {
		if _, err := store.base.DB().Exec(statement); err == nil {
			t.Fatalf("schema accepted invalid state: %s", statement)
		}
	}
	if _, err := store.base.DB().Exec(`INSERT INTO notification_profiles(user_id, change_version, deleted) VALUES('deleted', 1, 1)`); err != nil {
		t.Fatalf("schema cannot retain a profile tombstone without contribution rows: %v", err)
	}
}

func assertProfileVersion(t *testing.T, store *Store, userID string, want int64, wantDeleted bool) {
	t.Helper()
	var version int64
	var deleted bool
	err := store.base.DB().QueryRow(`SELECT change_version, deleted FROM notification_profiles WHERE user_id = ?`, userID).Scan(&version, &deleted)
	if err != nil || version != want || deleted != wantDeleted {
		t.Fatalf("%s profile version=%d, deleted=%v, err=%v; want %d, %v", userID, version, deleted, err, want, wantDeleted)
	}
}

func TestPrivateUserWritesVersionOnlyRealChanges(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	user := PrivateUserContribution{UserID: "user"}
	if err := store.PutPrivateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 1, false)
	_, updatedAt, err := readPrivateUser(ctx, store.base.DB(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutPrivateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base).Sequence; got != 1 {
		t.Fatalf("identical private replay advanced sequence to %d", got)
	}
	if _, got, err := readPrivateUser(ctx, store.base.DB(), "user"); err != nil || !got.Equal(updatedAt) {
		t.Fatalf("identical replay rewrote ingestion timestamp: %v, %v", got, err)
	}
	disabled := false
	user.WebPushEnabled = &disabled
	if err := store.PutPrivateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 2, false)
	if _, err := store.base.DB().Exec(`
		CREATE TRIGGER reject_profile_version BEFORE UPDATE ON notification_profiles
		BEGIN SELECT RAISE(ABORT, 'injected profile version failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, store.base)
	enabled := true
	user.WebPushEnabled = &enabled
	if err := store.PutPrivateUser(ctx, user); err == nil {
		t.Fatal("write succeeded despite profile-version failure")
	}
	assertProfileVersion(t, store, "user", 2, false)
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("failed version write advanced metadata: %+v; want %+v", got, before)
	}
	got, _, err := readPrivateUser(ctx, store.base.DB(), "user")
	if err != nil || got.WebPushEnabled == nil || *got.WebPushEnabled {
		t.Fatalf("failed version write retained changed contribution: %+v, %v", got, err)
	}
}
