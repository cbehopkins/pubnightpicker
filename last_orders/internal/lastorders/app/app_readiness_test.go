package app_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/notificationprofile"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func TestFirebaseClientCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("FIRESTORE_EMULATOR_HOST", "")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "")
	for _, client := range []string{"firestore", "api", "admin-delete", "emulators", "injected"} {
		for _, explicit := range []bool{false, true} {
			name := client + "/default"
			if explicit {
				name = client + "/explicit"
			}
			t.Run(name, func(t *testing.T) {
				cfg := testConfig(t, filepath.Join(t.TempDir(), "credentials.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
				cfg.Logger = testLogger()
				cfg.FirestoreProjectID = "credential-project"
				cfg.AuthProjectID = "credential-project"
				if explicit {
					cfg.FirebaseCredentials = &google.Credentials{
						ProjectID:   "credential-project",
						TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"}),
					}
				}
				if client == "firestore" || client == "emulators" {
					cfg.EnableFirestore = true
				}
				if client == "api" || client == "emulators" || client == "injected" {
					cfg.HTTPAddr = "127.0.0.1:0"
					if client != "injected" {
						cfg.AuthVerifier = nil
					}
				}
				if client == "admin-delete" || client == "emulators" || client == "injected" {
					source := &adminDeleteTestSource{status: "pending"}
					cfg.AdminDeleteEnabled = true
					cfg.EnableRealAuthDelete = true
					cfg.AdminDeleteSource = source
					cfg.AdminDeleteRepository = adminDeleteTestRepository{source: source}
					if client == "injected" {
						cfg.AdminDeleteAuthClient = &adminDeleteTestAuth{}
					}
				}
				if client == "emulators" {
					t.Setenv("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8080")
					t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
					cfg.AllowAuthEmulator = true
				}
				application, err := app.New(cfg)
				if !explicit && client != "emulators" && client != "injected" {
					if err == nil {
						application.Close()
						t.Fatal("expected unavailable default credentials to fail construction")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := application.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// neverReadySource models Firestore not delivering an initial snapshot.
type neverReadySource struct{}

func (neverReadySource) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return blockedStream{ctx: ctx}, nil
}

func (neverReadySource) WatchPublicUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return blockedStream{ctx: ctx}, nil
}

func (neverReadySource) WatchEndpoints(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return blockedStream{ctx: ctx}, nil
}

func (neverReadySource) DeactivateEndpoint(context.Context, string, string) error { return nil }

type blockedStream struct{ ctx context.Context }

func (s blockedStream) Next() ([]notificationprofile.Change, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (blockedStream) Stop() {}

type persistenceFailureSource struct{ neverReadySource }

func (persistenceFailureSource) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return &projectionRemovalStream{blockedStream: blockedStream{ctx: ctx}}, nil
}

type projectionRemovalStream struct {
	blockedStream
	sent bool
}

func (stream *projectionRemovalStream) Next() ([]notificationprofile.Change, error) {
	if !stream.sent {
		stream.sent = true
		return []notificationprofile.Change{{
			Kind: notificationprofile.ChangeRemoved,
			Doc:  notificationprofile.Document{ID: "user"},
		}}, nil
	}
	return stream.blockedStream.Next()
}

func TestRunFailsOnProjectionPersistenceErrorBeforeReadiness(t *testing.T) {
	cfg := testConfig(t, filepath.Join(t.TempDir(), "projection-failure.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = testLogger()
	cfg.NotificationProfileSource = persistenceFailureSource{}
	application := newApp(t, cfg)
	defer application.Close()
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE notification_user_prefs"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = application.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "notification profile projection failed") ||
		!strings.Contains(err.Error(), "notification_user_prefs") {
		t.Fatalf("expected fatal projection persistence error, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("startup waited for cancellation instead of reporting the database failure")
	}
}

func newApp(t *testing.T, cfg app.Config) *app.App {
	t.Helper()
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return a
}

func TestRunWaitsForNotificationProfileBeforeProcessingWork(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, filepath.Join(t.TempDir(), "not-ready.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = testLogger()
	cfg.NotificationProfileSource = neverReadySource{}
	a := newApp(t, cfg)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-waiting"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 1 {
		t.Fatalf("active work cells = %d, want the queued cell untouched before readiness", len(active))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after cancellation while waiting for readiness")
	}
}

func TestCloseReleasesRunWaitingForNotificationProfile(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, filepath.Join(t.TempDir(), "close-not-ready.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = testLogger()
	cfg.NotificationProfileSource = neverReadySource{}
	a := newApp(t, cfg)

	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background()) }()

	time.Sleep(50 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after close while waiting for readiness")
	}
}
