package app_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/groupcache/groupcachetest"
	emailplugin "last_orders/internal/lastorders/plugins/email"
	pushplugin "last_orders/internal/lastorders/plugins/push"
	admindeletesvc "last_orders/internal/lastorders/services/admindelete"
	"last_orders/internal/lastorders/truths"
)

func TestAdminDeleteDryRunPreservesInterruptedSequenceStepAgainstEmulator(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("Firestore emulator is required")
	}
	assertLoopbackEmulator(t, "Firestore", host)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	config := testConfig(t, filepath.Join(t.TempDir(), "interrupted-admin-delete.db"), nil)
	config.EnableFirestore = true
	config.FirestoreProjectID = fmt.Sprintf("demo-admin-retry-%d", time.Now().UnixNano())
	config.AdminDeleteEnabled = true
	config.AdminDeleteDryRun = true
	application, err := app.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	truth := truths.AdminDeleteRequested{Request: truths.AdminDeleteRequestSnapshot{RequestID: "request-1", TargetUID: "target-1", SchemaVersion: 1}}
	envelope, err := truths.NewEnvelope(truths.AdminDeleteRequestedFanout, truth)
	if err != nil {
		t.Fatal(err)
	}
	request, err := firebaseidempotency.NewCellRequest("AdminDeleteRequested.real_delete", truth.Identity(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.AddCell(request); err != nil {
		t.Fatal(err)
	}
	store, ok := application.CellarStore().(cellar.DebuggableStore)
	if !ok {
		t.Fatal("Cellar store does not support offline inspection")
	}
	cell, err := store.Get(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	cell.CurrentStep = 1
	if err := store.ForceUpdate(cell); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		cell, err = store.Get(request.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cell.NotBefore != nil {
			if cell.CurrentStep != 1 {
				t.Fatalf("dry-run restarted interrupted live sequence at step %d", cell.CurrentStep)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("app stopped before deferring the sequence: %v", err)
		case <-ctx.Done():
			t.Fatal("timed out waiting for the interrupted sequence")
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAdminDeleteDryRunCanRepeatThenDeleteAgainstEmulators(t *testing.T) {
	firestoreHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	authHost := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	if firestoreHost == "" || authHost == "" {
		t.Skip("Firestore and Auth emulators are required")
	}
	assertLoopbackEmulator(t, "Firestore", firestoreHost)
	assertLoopbackEmulator(t, "Auth", authHost)
	projectID := "demo-admin-delete-local"
	requestID := fmt.Sprintf("request-%d", time.Now().UnixNano())
	staleRequestID := requestID + "-stale"
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	_, uid := createVotingEmulatorAccount(t, authHost, projectID)
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	accountApp, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := accountApp.Auth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seed := map[string]map[string]any{
		"admin_delete_requests/" + requestID:      {"schemaVersion": 1, "targetUid": uid, "targetEmail": nil, "requestedByUid": "admin-1", "requestedByEmail": nil, "reason": "admin_user_delete", "status": "pending", "createdAt": time.Now().UTC()},
		"admin_delete_requests/" + staleRequestID: {"schemaVersion": 1, "targetUid": "stale-user", "targetEmail": nil, "requestedByUid": "admin-1", "requestedByEmail": nil, "reason": "admin_user_delete", "status": "pending", "createdAt": time.Now().UTC()},
		"votes/" + requestID:                      {"venue": []string{uid, "other"}},
		"attendance/" + requestID:                 {"venue": map[string]any{"canCome": []string{uid, "other"}, "eta": map[string]any{uid: "19:00"}}},
		"messages/" + requestID:                   {"uid": uid, "name": "target", "text": "hello"},
		"users/" + uid:                            {"name": "target"},
		"user-public/" + uid:                      {"name": "target"},
		"users/" + uid + "/push_endpoints/device": {"endpoint": "private"},
		"roles/" + requestID:                      {uid: true, "other": true},
	}
	updateTimes := make(map[string]time.Time, len(seed))
	for path, data := range seed {
		result, err := client.Doc(path).Set(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		updateTimes[path] = result.UpdateTime
	}
	t.Cleanup(func() {
		for path := range seed {
			_, _ = client.Doc(path).Delete(context.Background())
		}
	})
	var output bytes.Buffer
	config := testConfig(t, filepath.Join(t.TempDir(), "admin-delete.db"), nil)
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.AuthProjectID = projectID
	config.AllowAuthEmulator = true
	config.AdminDeleteEnabled = true
	config.AdminDeleteDryRun = true
	config.AdminDeleteEvaluateOnce = true
	config.Logger = slog.New(slog.NewTextHandler(&output, nil))
	interrupted, err := app.New(config)
	if err != nil {
		t.Fatal(err)
	}
	truth := truths.AdminDeleteRequested{Request: truths.AdminDeleteRequestSnapshot{
		RequestID: requestID, TargetUID: uid, RequestedByUID: "admin-1", Reason: "admin_user_delete", SchemaVersion: 1, CreatedAt: time.Now().UTC(),
	}}
	envelope, err := truths.NewEnvelope(truths.AdminDeleteRequestedFanout, truth)
	if err != nil {
		t.Fatal(err)
	}
	cell, err := firebaseidempotency.NewCellRequest("AdminDeleteRequested.real_delete", requestID, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := interrupted.AddCell(cell); err != nil {
		t.Fatal(err)
	}
	persistence, err := cellar.NewCellDefinition(admindeletesvc.HandlerPersistOutcome, admindeletesvc.OutcomePayload{
		RequestID: staleRequestID, TargetUID: "stale-user", Outcome: admindeletesvc.OutcomeFailedPrecondition, At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	persistenceCell, err := persistence.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	if err := interrupted.AddCell(persistenceCell); err != nil {
		t.Fatal(err)
	}
	if err := interrupted.Close(); err != nil {
		t.Fatal(err)
	}
	run := func() {
		t.Helper()
		application, err := app.New(config)
		if err != nil {
			t.Fatal(err)
		}
		defer application.Close()
		runCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		defer stop()
		if err := application.Run(runCtx); err != nil {
			t.Fatalf("admin deletion run: %v", err)
		}
	}
	for runNumber := 0; runNumber < 2; runNumber++ {
		output.Reset()
		run()
		for _, path := range []string{"votes/" + requestID, "attendance/" + requestID, "messages/" + requestID, "roles/" + requestID, "users/" + uid, "user-public/" + uid, "push_endpoints/device"} {
			if !strings.Contains(output.String(), path) {
				t.Errorf("dry-run %d missing %q in logs: %s", runNumber, path, output.String())
			}
		}
		for path, updateTime := range updateTimes {
			snapshot, err := client.Doc(path).Get(ctx)
			if err != nil || !snapshot.UpdateTime.Equal(updateTime) {
				t.Fatalf("dry-run changed %s: %v", path, err)
			}
		}
		if _, err := accounts.GetUser(ctx, uid); err != nil {
			t.Fatalf("dry-run removed Auth account: %v", err)
		}
		remote, err := firebaseidempotency.NewFirestoreRemote(client, "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, listener := range []string{"AdminDeleteRequested", "AdminDeleteRequested.real_delete"} {
			if exists, err := remote.HasKey(ctx, listener, requestID); err != nil || exists {
				t.Fatalf("dry-run idempotency key for %s exists=%t err=%v", listener, exists, err)
			}
		}
	}
	config.AdminDeleteDryRun = false
	config.EnableRealAuthDelete = true
	run()
	if _, err := accounts.GetUser(ctx, uid); !firebaseauth.IsUserNotFound(err) {
		t.Fatalf("live Auth deletion error=%v", err)
	}
	for _, path := range []string{"users/" + uid, "user-public/" + uid, "users/" + uid + "/push_endpoints/device"} {
		if _, err := client.Doc(path).Get(ctx); status.Code(err) != codes.NotFound {
			t.Errorf("live deletion left %s: %v", path, err)
		}
	}
	for _, path := range []string{"votes/" + requestID, "attendance/" + requestID, "roles/" + requestID} {
		snapshot, err := client.Doc(path).Get(ctx)
		if err != nil || strings.Contains(fmt.Sprint(snapshot.Data()), uid) {
			t.Errorf("live deletion left user in %s: snapshot=%v err=%v", path, snapshot, err)
		}
	}
	message, err := client.Doc("messages/" + requestID).Get(ctx)
	if err != nil || message.Data()["uid"] != "__deleted_user__" || message.Data()["text"] != "{user deleted}" {
		t.Fatalf("live deletion did not anonymise message: snapshot=%v err=%v", message, err)
	}
}

// The mocked app tests cover behaviour; this covers that the Firestore-backed
// implementations still satisfy the interfaces app.New resolves them into.
func TestAppWiresUpFirestoreCollaboratorsAgainstEmulator(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "last-orders-emulator"
	}

	a, err := app.New(app.Config{
		DBPath:             filepath.Join(t.TempDir(), "emulator.db"),
		PollDelay:          5 * time.Millisecond,
		Logger:             testLogger(),
		EnableFirestore:    true,
		PollsSince:         time.Now().Format(time.DateOnly),
		FirestoreProjectID: projectID,
		EmailClient:        emailplugin.ClientDummy,
		Push:               pushplugin.Options{Client: pushplugin.ClientDummy},
		GroupSource:        groupcachetest.New(),
	})
	if err != nil {
		t.Fatalf("new app against emulator: %v", err)
	}
	defer a.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Run(ctx); err != nil {
		t.Fatalf("run app against emulator: %v", err)
	}
}
