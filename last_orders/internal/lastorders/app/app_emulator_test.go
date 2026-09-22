package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
)

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
		FirestoreProjectID: projectID,
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
