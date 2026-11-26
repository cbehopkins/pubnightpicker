package groups

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFirestoreSelfJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("Firestore emulator is not configured")
	}
	hostname, _, err := net.SplitHostPort(host)
	if err != nil || (hostname != "localhost" && !net.ParseIP(hostname).IsLoopback()) {
		t.Fatal("self-join tests require a loopback Firestore emulator")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, "demo-membership-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reference := client.Collection("groups").Doc("public-group")
	var invalidations atomic.Int32
	writer := NewFirestoreMembership(client, func(groupID string) {
		if groupID != reference.ID {
			t.Errorf("invalidated unexpected Group %s", groupID)
		}
		invalidations.Add(1)
	})
	t.Run("missing membership is healed and repeated joins do not write", func(t *testing.T) {
		if _, err := reference.Set(ctx, map[string]any{"name": "ampubnight", "visibility": "public", "config": map[string]any{"retained": true}}); err != nil {
			t.Fatal(err)
		}
		if err := writer.EnsureMember(ctx, reference.ID, "caller.with.dot"); err != nil {
			t.Fatal(err)
		}
		before, err := reference.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if before.Data()["members"].(map[string]any)["caller.with.dot"] != true || before.Data()["config"].(map[string]any)["retained"] != true {
			t.Fatal("self-join did not preserve the Group and add the exact UID")
		}
		if err := writer.EnsureMember(ctx, reference.ID, "caller.with.dot"); err != nil {
			t.Fatal(err)
		}
		after, err := reference.Get(ctx)
		if err != nil || !before.UpdateTime.Equal(after.UpdateTime) {
			t.Fatalf("repeat join wrote the Group: %v", err)
		}
		if invalidations.Load() != 2 {
			t.Fatal("both successful joins must invalidate cached authority")
		}
	})
	t.Run("concurrent joins preserve other members and grants", func(t *testing.T) {
		if _, err := reference.Set(ctx, map[string]any{
			"visibility": "public", "members": map[string]any{"existing": true, "denied": false, "wrong-type": "true"},
			"permissions": map[string]any{"admin": map[string]any{"existing": true}},
		}); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 6)
		for index := range 6 {
			go func() { results <- writer.EnsureMember(ctx, reference.ID, fmt.Sprintf("member-%d", index)) }()
		}
		for range 6 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		for _, uid := range []string{"denied", "wrong-type"} {
			if err := writer.EnsureMember(ctx, reference.ID, uid); err != nil {
				t.Fatal(err)
			}
		}
		document, err := reference.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		members := document.Data()["members"].(map[string]any)
		if len(members) != 9 || members["existing"] != true || members["denied"] != true || members["wrong-type"] != true {
			t.Fatalf("membership grants not preserved: %v", members)
		}
		for index := range 6 {
			if members[fmt.Sprintf("member-%d", index)] != true {
				t.Fatal("concurrent join lost a member")
			}
		}
		if document.Data()["permissions"].(map[string]any)["admin"].(map[string]any)["existing"] != true {
			t.Fatal("self-join changed permissions")
		}
	})
	t.Run("site administrators can join private Groups", func(t *testing.T) {
		adminRole := client.Collection("roles").Doc("admin")
		for _, test := range []struct {
			name    string
			grants  map[string]any
			allowed bool
		}{
			{name: "missing role"},
			{name: "missing grant", grants: map[string]any{"another-admin": true}},
			{name: "false grant", grants: map[string]any{"caller": false}},
			{name: "wrongly typed grant", grants: map[string]any{"caller": "true"}},
			{name: "true grant", grants: map[string]any{"caller": true}, allowed: true},
		} {
			t.Run(test.name, func(t *testing.T) {
				if _, err := reference.Set(ctx, map[string]any{"visibility": "private", "members": map[string]any{"existing": true}}); err != nil {
					t.Fatal(err)
				}
				if test.grants == nil {
					if _, err := adminRole.Delete(ctx); err != nil {
						t.Fatal(err)
					}
				} else if _, err := adminRole.Set(ctx, test.grants); err != nil {
					t.Fatal(err)
				}
				before := invalidations.Load()
				err := writer.EnsureMember(ctx, reference.ID, "caller")
				if test.allowed && err != nil {
					t.Fatalf("site administrator could not join a private Group: %v", err)
				}
				if !test.allowed && !errors.Is(err, errPrivateGroup) {
					t.Fatalf("non-administrator private join error=%v", err)
				}
				document, err := reference.Get(ctx)
				if err != nil {
					t.Fatal(err)
				}
				members := document.Data()["members"].(map[string]any)
				member, _ := members["caller"].(bool)
				if member != test.allowed || members["existing"] != true || document.Data()["visibility"] != "private" {
					t.Fatalf("unexpected Group after join: %v", document.Data())
				}
				wantInvalidations := before
				if test.allowed {
					wantInvalidations++
				}
				if invalidations.Load() != wantInvalidations {
					t.Fatal("only an authorised join should invalidate cached authority")
				}
			})
		}
		if _, err := adminRole.Delete(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("private and missing Groups cannot be joined", func(t *testing.T) {
		if _, err := reference.Set(ctx, map[string]any{"visibility": "private"}); err != nil {
			t.Fatal(err)
		}
		before := invalidations.Load()
		if err := writer.EnsureMember(ctx, reference.ID, "caller"); !errors.Is(err, errPrivateGroup) {
			t.Fatalf("private join error=%v", err)
		}
		if _, err := reference.Delete(ctx); err != nil {
			t.Fatal(err)
		}
		if err := writer.EnsureMember(ctx, reference.ID, "caller"); !errors.Is(err, errGroupNotFound) {
			t.Fatalf("missing join error=%v", err)
		}
		if _, err := reference.Get(ctx); status.Code(err) != codes.NotFound || invalidations.Load() != before {
			t.Fatal("refused joins must not create Groups or report success")
		}
	})
}
