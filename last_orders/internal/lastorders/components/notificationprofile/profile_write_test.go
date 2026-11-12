package notificationprofile

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/venuecache"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

func TestProfileLifecycleRetainsPartialUsersAndExcludesOrphans(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	enabled := true
	endpoint := EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth", Active: &enabled}
	if err := store.PutEndpointContribution(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.base.DB().QueryRow(`SELECT COUNT(*) FROM notification_profiles`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan endpoint established a profile: %d, %v", count, err)
	}
	if got := profileSyncState(t, store.base).Sequence; got != 0 {
		t.Fatalf("orphan endpoint allocated sequence %d", got)
	}
	public := PublicUserContribution{UserID: "user"}
	if err := store.PutPublicUser(ctx, public); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 1, false)
	if endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: KindDiagnostic}); err != nil || len(endpoints) != 0 {
		t.Fatalf("public-only profile enabled push: %+v, %v", endpoints, err)
	}
	private := PrivateUserContribution{UserID: "user", WebPushEnabled: &enabled}
	if err := store.PutPrivateUser(ctx, private); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 2, false)
	if endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: KindPollOpens}); err != nil || len(endpoints) != 1 {
		t.Fatalf("private contribution did not restore eligible push: %+v, %v", endpoints, err)
	}
	if err := store.DeletePreferences(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 3, false)
	if got, err := readPublicUser(ctx, store.base.DB(), "user"); err != nil || !got.Equal(public) {
		t.Fatalf("private removal erased public contribution: %+v, %v", got, err)
	}
	if endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: KindDiagnostic}); err != nil || len(endpoints) != 0 {
		t.Fatalf("removed private user can receive push: %+v, %v", endpoints, err)
	}
	if err := store.DeletePublicUser(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 4, true)
	endpoint.Auth = "rotated orphan auth"
	if err := store.PutEndpointContribution(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEndpointActive(ctx, "user", "device", false); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePreferences(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePublicUser(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 4, true)
	if got := profileSyncState(t, store.base).Sequence; got != 4 {
		t.Fatalf("orphan changes or repeated removals allocated sequence %d", got)
	}
	if err := store.PutPrivateUser(ctx, private); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 5, false)
	if endpoints, err := store.EligibleEndpoints(ctx, Selector{Kind: KindDiagnostic}); err != nil || len(endpoints) != 0 {
		t.Fatalf("recreation ignored inactive endpoint: %+v, %v", endpoints, err)
	}
	if err := store.SetEndpointActive(ctx, "user", "device", true); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 6, false)
	if err := store.DeleteEndpoint(ctx, "user", "device"); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 7, false)
	if err := store.DeleteEndpoint(ctx, "user", "device"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEndpointActive(ctx, "user", "device", false); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base).Sequence; got != 7 {
		t.Fatalf("absent endpoint writes allocated sequence %d", got)
	}
}

func TestProfileVersionsPublicFieldPresence(t *testing.T) {
	store := newTestStore(t)
	for index, data := range []map[string]any{{}, {"name": nil}, {"name": ""}, {"name": "Name"}, {}} {
		user, err := PublicUserFromDocument(Document{ID: "user", Data: data})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.PutPublicUser(t.Context(), user); err != nil {
			t.Fatal(err)
		}
		assertProfileVersion(t, store, "user", int64(index+1), false)
		if err := store.PutPublicUser(t.Context(), user); err != nil {
			t.Fatal(err)
		}
		if got := profileSyncState(t, store.base).Sequence; got != int64(index+1) {
			t.Fatalf("public replay advanced sequence to %d", got)
		}
	}
}

func TestEndpointBatchAllocatesOncePerChangedOwner(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	for _, userID := range []string{"alice", "bob"} {
		if err := store.PutPrivateUser(ctx, PrivateUserContribution{UserID: userID}); err != nil {
			t.Fatal(err)
		}
	}
	mutations := []EndpointContributionMutation{}
	for _, userID := range []string{"bob", "alice"} {
		for _, endpointID := range []string{"first", "second"} {
			endpoint := EndpointContribution{UserID: userID, EndpointID: endpointID, URL: "url", P256DH: "key", Auth: "auth"}
			mutations = append(mutations, EndpointContributionMutation{UserID: userID, EndpointID: endpointID, Endpoint: &endpoint})
		}
	}
	if err := store.ApplyEndpointContributions(ctx, mutations, true); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "alice", 3, false)
	assertProfileVersion(t, store, "bob", 4, false)
	before := profileSyncState(t, store.base)
	_, updatedAt, err := readEndpointContribution(ctx, store.base.DB(), "alice", "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyEndpointContributions(ctx, mutations, true); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("identical endpoint baseline changed sequence: %+v; want %+v", got, before)
	}
	if _, got, err := readEndpointContribution(ctx, store.base.DB(), "alice", "first"); err != nil || !got.Equal(updatedAt) {
		t.Fatalf("identical baseline rewrote endpoint timestamp: %v, %v", got, err)
	}
	original := *mutations[2].Endpoint
	intermediate := original
	intermediate.Auth = "temporary"
	if err := store.ApplyEndpointContributions(ctx, []EndpointContributionMutation{
		{UserID: "alice", EndpointID: "first", Endpoint: &intermediate},
		{UserID: "alice", EndpointID: "first", Endpoint: &original},
	}, false); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("net-zero endpoint batch changed sequence: %+v; want %+v", got, before)
	}
	if err := store.ApplyEndpointContributions(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "alice", 5, false)
	assertProfileVersion(t, store, "bob", 6, false)
	if err := store.ApplyEndpointContributions(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base).Sequence; got != 6 {
		t.Fatalf("empty endpoint replay allocated sequence %d", got)
	}
}

func TestProfileBaselinesReconcileOnlyTheirOwnSource(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	private := PrivateUserContribution{UserID: "alice"}
	public := PublicUserContribution{UserID: "alice"}
	if err := store.PutPrivateUser(ctx, private); err != nil {
		t.Fatal(err)
	}
	if err := store.PutPublicUser(ctx, public); err != nil {
		t.Fatal(err)
	}
	endpoint := EndpointContribution{UserID: "alice", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth"}
	if err := store.PutEndpointContribution(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, store.base)
	if err := store.ApplyPrivateUsers(ctx, []PrivateUserMutation{{UserID: "alice", User: &private}}, true); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPublicUsers(ctx, []PublicUserMutation{{UserID: "alice", User: &public}}, true); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("identical user baselines allocated versions: %+v; want %+v", got, before)
	}
	if err := store.ApplyPrivateUsers(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "alice", 4, false)
	if got, err := readPublicUser(ctx, store.base.DB(), "alice"); err != nil || !got.Equal(public) {
		t.Fatalf("private baseline erased public source: %+v, %v", got, err)
	}
	if got, _, err := readEndpointContribution(ctx, store.base.DB(), "alice", "device"); err != nil || !got.Equal(endpoint) {
		t.Fatalf("private baseline erased endpoints: %+v, %v", got, err)
	}
	if err := store.ApplyPublicUsers(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "alice", 5, true)
	if err := store.ApplyPrivateUsers(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPublicUsers(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyEndpointContributions(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base).Sequence; got != 5 {
		t.Fatalf("absent baselines changed tombstone sequence: %d", got)
	}
}

func TestProfileBatchFailureRollsBackEveryOwner(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	for _, userID := range []string{"alice", "bob"} {
		if err := store.PutPrivateUser(ctx, PrivateUserContribution{UserID: userID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.base.DB().Exec(`
		CREATE TRIGGER reject_second_owner BEFORE INSERT ON notification_profiles
		WHEN NEW.user_id = 'bob' BEGIN SELECT RAISE(ABORT, 'injected second owner failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, store.base)
	alice := EndpointContribution{UserID: "alice", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth"}
	bob := alice
	bob.UserID = "bob"
	if err := store.ApplyEndpointContributions(ctx, []EndpointContributionMutation{
		{UserID: "alice", EndpointID: "device", Endpoint: &alice},
		{UserID: "bob", EndpointID: "device", Endpoint: &bob},
	}, false); err == nil {
		t.Fatal("multi-owner batch succeeded despite failure after allocation")
	}
	if got := profileSyncState(t, store.base); got != before {
		t.Fatalf("failed batch advanced global state: %+v; want %+v", got, before)
	}
	assertProfileVersion(t, store, "alice", 1, false)
	assertProfileVersion(t, store, "bob", 2, false)
	for _, userID := range []string{"alice", "bob"} {
		if _, _, err := readEndpointContribution(ctx, store.base.DB(), userID, "device"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("failed batch retained %s endpoint: %v", userID, err)
		}
	}
}

func TestProfilesAndVenuesShareGlobalSequence(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	venues, err := venuecache.New(store.base)
	if err != nil {
		t.Fatal(err)
	}
	if err := venues.Put(ctx, venuecache.VenueProjection{ID: "venue", Name: "The Crown"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutPrivateUser(ctx, PrivateUserContribution{UserID: "user"}); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 2, false)
	if err := venues.Delete(ctx, "venue"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePreferences(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 4, true)
	if got := profileSyncState(t, store.base).Sequence; got != 4 {
		t.Fatalf("shared global sequence = %d; want 4", got)
	}
}

type invalidationSource struct {
	Source
	deactivate func(context.Context, string, string) error
}

func (source invalidationSource) DeactivateEndpoint(ctx context.Context, userID, endpointID string) error {
	return source.deactivate(ctx, userID, endpointID)
}

func TestEndpointInvalidationIsRemoteFirstAndReplaySafe(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	if err := store.PutPrivateUser(ctx, PrivateUserContribution{UserID: "user"}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	endpoint := EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth", Active: &enabled}
	if err := store.PutEndpointContribution(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	reject := true
	calls := 0
	service, err := NewService(store, invalidationSource{deactivate: func(callCtx context.Context, userID, endpointID string) error {
		calls++
		if userID != "user" || endpointID != "device" {
			t.Fatalf("unexpected invalidation key %s/%s", userID, endpointID)
		}
		got, _, err := readEndpointContribution(callCtx, store.base.DB(), userID, endpointID)
		if err != nil || got.Active == nil || !*got.Active {
			t.Fatalf("local invalidation happened before remote update: %+v, %v", got, err)
		}
		if reject {
			return errors.New("remote unavailable")
		}
		return nil
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.InvalidateEndpoint(ctx, "user", "device"); err == nil {
		t.Fatal("invalidation ignored remote failure")
	}
	assertProfileVersion(t, store, "user", 2, false)
	reject = false
	if err := service.InvalidateEndpoint(ctx, "user", "device"); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 3, false)
	disabled := false
	endpoint.Active = &disabled
	if err := store.PutEndpointContribution(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEndpointActive(ctx, "user", "device", false); err != nil {
		t.Fatal(err)
	}
	if got := profileSyncState(t, store.base).Sequence; got != 3 || calls != 2 {
		t.Fatalf("invalidation replay: sequence=%d, remote calls=%d", got, calls)
	}
}

func TestCompatibilityPreferenceWritesAreVersionedAndReplaySafe(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	address, request := "private@example.com", "request"
	private := PrivateUserContribution{UserID: "user", Email: &address, TestEmailReq: &request}
	if err := store.PutPrivateUser(ctx, private); err != nil {
		t.Fatal(err)
	}
	preferences := private.Preferences()
	preferences.WebPushEnabled = true
	preferences.EventChatMutedPollIDs = []string{"second", "first", "second"}
	if err := store.PutPreferences(ctx, preferences); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 2, false)
	got, updatedAt, err := readPrivateUser(ctx, store.base.DB(), "user")
	if err != nil || !equalOptional(got.Email, private.Email) || !equalOptional(got.TestEmailReq, private.TestEmailReq) {
		t.Fatalf("compatibility preference write erased unrelated fields: %+v, %v", got, err)
	}
	preferences.EventChatMutedPollIDs = []string{"first", "second"}
	if err := store.ApplyPreferences(ctx, []UserPreferencesMutation{{UserID: "user", Preferences: &preferences}}, true); err != nil {
		t.Fatal(err)
	}
	if _, actual, err := readPrivateUser(ctx, store.base.DB(), "user"); err != nil || !actual.Equal(updatedAt) {
		t.Fatalf("preference replay rewrote timestamp: %v, %v", actual, err)
	}
	if sequence := profileSyncState(t, store.base).Sequence; sequence != 2 {
		t.Fatalf("preference replay allocated sequence %d", sequence)
	}
	if err := store.ApplyPreferences(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	assertProfileVersion(t, store, "user", 3, true)
}

func TestInvalidProfileBatchesLeaveStateUnchanged(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	user := PrivateUserContribution{UserID: "user"}
	public := PublicUserContribution{UserID: "user"}
	endpoint := EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth"}
	if err := store.PutPrivateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	before := profileSyncState(t, store.base)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"empty private ID", func() error { return store.PutPrivateUser(ctx, PrivateUserContribution{}) }},
		{"private mismatch", func() error {
			return store.ApplyPrivateUsers(ctx, []PrivateUserMutation{{UserID: "different", User: &user}}, true)
		}},
		{"duplicate private baseline", func() error {
			return store.ApplyPrivateUsers(ctx, []PrivateUserMutation{{UserID: "user", User: &user}, {UserID: "user", User: &user}}, true)
		}},
		{"public mismatch", func() error {
			return store.ApplyPublicUsers(ctx, []PublicUserMutation{{UserID: "different", User: &public}}, true)
		}},
		{"duplicate public baseline", func() error {
			return store.ApplyPublicUsers(ctx, []PublicUserMutation{{UserID: "user", User: &public}, {UserID: "user", User: &public}}, true)
		}},
		{"endpoint mismatch", func() error {
			return store.ApplyEndpointContributions(ctx, []EndpointContributionMutation{{UserID: "other", EndpointID: "device", Endpoint: &endpoint}}, true)
		}},
		{"duplicate endpoint baseline", func() error {
			mutation := EndpointContributionMutation{UserID: "user", EndpointID: "device", Endpoint: &endpoint}
			return store.ApplyEndpointContributions(ctx, []EndpointContributionMutation{mutation, mutation}, true)
		}},
		{"empty active owner", func() error { return store.SetEndpointActive(ctx, "", "device", false) }},
		{"empty endpoint ID", func() error { return store.SetEndpointActive(ctx, "user", "", false) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("invalid batch succeeded")
			}
			if got := profileSyncState(t, store.base); got != before {
				t.Fatalf("invalid batch changed global state: %+v; want %+v", got, before)
			}
			actual, _, err := readPrivateUser(ctx, store.base.DB(), "user")
			if err != nil || !actual.Equal(user) {
				t.Fatalf("invalid batch changed private state: %+v, %v", actual, err)
			}
			assertProfileVersion(t, store, "user", 1, false)
		})
	}
}

func TestConcurrentProfileWritesAllocateOncePerCommittedChange(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "profiles.db")+
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const workers = 8
	for _, stage := range []string{"private replay", "distinct endpoints", "private deletion"} {
		start := make(chan struct{})
		failures := make(chan error, workers)
		var group sync.WaitGroup
		for index := range workers {
			group.Go(func() {
				<-start
				var err error
				switch stage {
				case "private replay":
					err = store.PutPrivateUser(ctx, PrivateUserContribution{UserID: "user"})
				case "distinct endpoints":
					err = store.PutEndpointContribution(ctx, EndpointContribution{
						UserID: "user", EndpointID: fmt.Sprintf("device-%d", index), URL: "url", P256DH: "key", Auth: "auth",
					})
				case "private deletion":
					err = store.DeletePreferences(ctx, "user")
				}
				if err != nil {
					failures <- err
				}
			})
		}
		close(start)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		want := int64(1)
		deleted := false
		if stage == "distinct endpoints" {
			want = workers + 1
		}
		if stage == "private deletion" {
			want = workers + 2
			deleted = true
		}
		assertProfileVersion(t, store, "user", want, deleted)
		if got := profileSyncState(t, base).Sequence; got != want {
			t.Fatalf("%s sequence = %d; want %d", stage, got, want)
		}
	}
	var endpoints int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_endpoints`).Scan(&endpoints); err != nil || endpoints != workers {
		t.Fatalf("concurrent endpoint writes lost rows: %d, %v; want %d", endpoints, err, workers)
	}
}

// A paused real transaction must expose neither partial contributions nor an
// advanced version; old snapshots stay stable after either commit or rollback.
func TestCombinedProfileReadersObserveAtomicVersions(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%v", rollback), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			paused, release := make(chan struct{}), make(chan struct{})
			releaseWriter := sync.OnceFunc(func() { close(release) })
			defer releaseWriter()
			function := "pause_profile_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if err := sqlite.RegisterScalarFunction(function, 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
				close(paused)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				if rollback {
					return nil, errors.New("injected profile rollback")
				}
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "atomic-profile.db")
			db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			base, err := basestore.New(db)
			if err != nil {
				t.Fatal(err)
			}
			store, err := New(base)
			if err != nil {
				t.Fatal(err)
			}
			enabled, disabled := true, false
			oldName, newName, newEmail := "Old name", "New name", "new@example.com"
			private := PrivateUserContribution{UserID: "user", WebPushEnabled: &enabled}
			public := PublicUserContribution{UserID: "user", Name: NullableField[string]{Present: true, Value: &oldName}}
			endpoint := EndpointContribution{UserID: "user", EndpointID: "device", URL: "url", P256DH: "key", Auth: "auth", Active: &enabled}
			if err := store.PutPrivateUser(ctx, private); err != nil {
				t.Fatal(err)
			}
			if err := store.PutPublicUser(ctx, public); err != nil {
				t.Fatal(err)
			}
			if err := store.PutEndpointContribution(ctx, endpoint); err != nil {
				t.Fatal(err)
			}
			before := profileSyncState(t, base)
			oldState := profileState{private: &private, public: &public, endpoints: []EndpointContribution{endpoint}}
			changedPrivate, changedPublic, changedEndpoint := private, public, endpoint
			changedPrivate.Email = &newEmail
			changedPublic.Name.Value = &newName
			changedEndpoint.Active = &disabled
			newState := profileState{private: &changedPrivate, public: &changedPublic, endpoints: []EndpointContribution{changedEndpoint}}
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`
				CREATE TRIGGER pause_profile BEFORE INSERT ON notification_profiles
				WHEN NEW.user_id = 'user' BEGIN SELECT %s(); END;
			`, function)); err != nil {
				t.Fatal(err)
			}
			reader, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			done := make(chan error, 1)
			go func() {
				done <- store.writeProfiles(ctx, []string{"user"}, func(tx *sql.Tx) error {
					if err := writePrivateUser(ctx, tx, changedPrivate); err != nil {
						return err
					}
					if err := writePublicUser(ctx, tx, changedPublic); err != nil {
						return err
					}
					return writeEndpointContribution(ctx, tx, changedEndpoint)
				})
			}()
			select {
			case <-paused:
			case err := <-done:
				t.Fatalf("write finished before test pause: %v", err)
			case <-ctx.Done():
				t.Fatal("write did not reach the version boundary")
			}
			assertSnapshot := func(tx *sql.Tx, want profileState, wantVersion int64) {
				t.Helper()
				got, err := readProfileState(ctx, tx, "user")
				if err != nil || !got.equal(want) {
					t.Fatalf("inconsistent contribution snapshot: %+v, %v", got, err)
				}
				var version int64
				var deleted bool
				if err := tx.QueryRowContext(ctx, `SELECT change_version, deleted FROM notification_profiles WHERE user_id = 'user'`).Scan(&version, &deleted); err != nil || version != wantVersion || deleted {
					t.Fatalf("inconsistent profile version: %d, deleted=%v, %v; want %d", version, deleted, err, wantVersion)
				}
				state, err := base.ReadSyncState(ctx, tx)
				if err != nil || state.Generation != before.Generation || state.Sequence != wantVersion {
					t.Fatalf("inconsistent sequence snapshot: %+v, %v; want %d", state, err, wantVersion)
				}
			}
			oldSnapshot, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer oldSnapshot.Rollback()
			assertSnapshot(oldSnapshot, oldState, before.Sequence)
			releaseWriter()
			select {
			case err := <-done:
				if (err != nil) != rollback {
					t.Fatalf("write error = %v; rollback=%v", err, rollback)
				}
			case <-ctx.Done():
				t.Fatal("write did not finish")
			}
			assertSnapshot(oldSnapshot, oldState, before.Sequence)
			freshSnapshot, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer freshSnapshot.Rollback()
			if rollback {
				assertSnapshot(freshSnapshot, oldState, before.Sequence)
			} else {
				assertSnapshot(freshSnapshot, newState, before.Sequence+1)
			}
		})
	}
}
