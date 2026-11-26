package attendancecleanup

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestRemoveUIDPreservesOtherValues(t *testing.T) {
	filtered, changed := removeUID([]any{"target", "other", 42, "target"}, "target")
	values, ok := filtered.([]any)
	if !ok || !changed || len(values) != 2 || values[0] != "other" || values[1] != 42 {
		t.Fatalf("filtered=%v changed=%t", filtered, changed)
	}
	_, changed = removeUID([]string{"other"}, "target")
	if changed {
		t.Fatal("unmatched UID changed the array")
	}
}

func TestFirestoreCleanerRemovesAttendanceIdempotently(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	host := strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST"))
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST must be set")
	}
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		t.Fatal(err)
	}
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		t.Fatalf("Firestore emulator host %q is not loopback", host)
	}
	const projectID = "demo-attendance-cleanup-local"
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ref := client.Collection("attendance").Doc("cleanup-test")
	if _, err := ref.Set(ctx, map[string]any{
		"venue.with.dot": map[string]any{
			"canCome": []string{"target", "other"}, "cannotCome": []string{"target"},
			"eta": map[string]any{"target": "19:00", "other": "19:30"}, "extra": "preserved",
		},
		"any": map[string]any{"canCome": []string{"target"}},
	}); err != nil {
		t.Fatal(err)
	}
	defer ref.Delete(context.Background())
	cleaner, err := NewFirestoreCleaner(client)
	if err != nil {
		t.Fatal(err)
	}
	first, err := cleaner.RemoveUser(ctx, "target")
	if err != nil || first.UpdatedDocuments != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := cleaner.RemoveUser(ctx, "target")
	if err != nil || second.UpdatedDocuments != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	snapshot, err := ref.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	venue := snapshot.Data()["venue.with.dot"].(map[string]any)
	canCome := venue["canCome"].([]any)
	cannotCome := venue["cannotCome"].([]any)
	eta := venue["eta"].(map[string]any)
	anyEntry := snapshot.Data()["any"].(map[string]any)
	if len(canCome) != 1 || canCome[0] != "other" || len(cannotCome) != 0 || len(eta) != 1 || eta["other"] != "19:30" || venue["extra"] != "preserved" || len(anyEntry["canCome"].([]any)) != 0 {
		t.Fatalf("attendance not preserved: %v", snapshot.Data())
	}
}
