package groupcache

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"last_orders/internal/lastorders/basestore"
	component "last_orders/internal/lastorders/components/groupcache"
	"last_orders/internal/lastorders/components/sse"

	_ "modernc.org/sqlite"
)

type authorizationInvalidatorSpy struct {
	groupInvalidations int
	allInvalidations   int
}

func (s *authorizationInvalidatorSpy) InvalidateGroup(string) {
	s.groupInvalidations++
}

func (s *authorizationInvalidatorSpy) InvalidateAllGroups() {
	s.allInvalidations++
}

func TestMalformedBaselineInvalidatesGroupAuthorization(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := basestore.New(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	store, err := component.NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	invalidator := &authorizationInvalidatorSpy{}
	hub := sse.NewHub()
	subscription := hub.Register("current-meets:group-a", "current-meets-group:group-a", "user-a")
	defer subscription.Unregister()
	listener := &Listener{store: store, logger: slog.Default(), authorization: invalidator, hub: hub}
	err = listener.applyBatch(context.Background(), []component.Change{{
		Kind: component.ChangeAdded,
		Doc:  component.Document{ID: "group-a", Data: map[string]any{"visibility": "private"}},
	}}, true)
	if !errors.Is(err, ErrMalformedDocument) {
		t.Fatalf("malformed baseline error=%v, want %v", err, ErrMalformedDocument)
	}
	if invalidator.allInvalidations != 1 || invalidator.groupInvalidations != 0 {
		t.Fatalf("invalidations all=%d group=%d, want all=1 group=0", invalidator.allInvalidations, invalidator.groupInvalidations)
	}
	select {
	case <-subscription.Invalidated():
	default:
		t.Fatal("malformed reconnect baseline did not reset authenticated streams")
	}
}
