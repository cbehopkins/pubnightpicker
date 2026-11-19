package groupcache

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"last_orders/internal/lastorders/basestore"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestListPublicGroupsUsesExactCaseSensitiveNameFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	groups := []Group{
		{ID: "north-id", Name: "North", Visibility: VisibilityPublic},
		{ID: "lower-id", Name: "north", Visibility: VisibilityPublic},
		{ID: "private-id", Name: "North private", Visibility: VisibilityPrivate},
	}
	mutations := make([]Mutation, 0, len(groups))
	for index := range groups {
		mutations = append(mutations, Mutation{ID: groups[index].ID, Group: &groups[index]})
	}
	if err := store.ApplyBatch(ctx, mutations, true); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		want []Group
	}{
		{name: "North", want: []Group{{ID: "north-id", Name: "North", Visibility: VisibilityPublic}}},
		{name: "north", want: []Group{{ID: "lower-id", Name: "north", Visibility: VisibilityPublic}}},
		{name: "not found", want: []Group{}},
	} {
		got, err := store.ListPublic(ctx, &test.name)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(test.want) {
			t.Fatalf("ListPublic(%q) returned %+v, want %+v", test.name, got, test.want)
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Fatalf("ListPublic(%q) returned %+v, want %+v", test.name, got, test.want)
			}
		}
	}

	all, err := store.ListPublic(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered public list returned %+v, want both public case variants", all)
	}
}

func TestBaselineReplacesStaleGroups(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	stale := Group{ID: "stale-id", Name: "Stale", Visibility: VisibilityPublic}
	if err := store.ApplyBatch(ctx, []Mutation{{ID: stale.ID, Group: &stale}}, false); err != nil {
		t.Fatal(err)
	}
	current := Group{ID: "fresh-id", Name: "Fresh", Visibility: VisibilityPublic}
	if err := store.ApplyBatch(ctx, []Mutation{{ID: current.ID, Group: &current}}, true); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListPublic(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != current {
		t.Fatalf("baseline list = %+v, want only %+v", got, current)
	}
}
