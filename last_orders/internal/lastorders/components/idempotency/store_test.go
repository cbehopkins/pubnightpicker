package idempotency

import (
	"context"
	"database/sql"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}

	store, err := New(base)
	if err != nil {
		t.Fatalf("new idempotency store: %v", err)
	}
	return store
}

func TestExistsFalseForUnclaimedIdentity(t *testing.T) {
	store := newTestStore(t)

	exists, err := store.Exists(context.Background(), "component", "key-1")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Fatal("expected unclaimed identity to not exist")
	}
}

func TestInsertUnlessExistsWorkClaimsOnce(t *testing.T) {
	store := newTestStore(t)
	db := store.base.DB()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := store.InsertUnlessExistsWork("component", "key-1")(sqlTxAdapter{tx}); err != nil {
		t.Fatalf("first claim should succeed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	exists, err := store.Exists(context.Background(), "component", "key-1")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("expected identity to be claimed")
	}

	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx2.Rollback()
	if err := store.InsertUnlessExistsWork("component", "key-1")(sqlTxAdapter{tx2}); err == nil {
		t.Fatal("expected the second claim of the same identity to be rejected")
	}
}

// sqlTxAdapter adapts *sql.Tx to cellar.ApplicationTx for direct store testing.
type sqlTxAdapter struct{ tx *sql.Tx }

func (a sqlTxAdapter) Exec(query string, args ...any) error {
	_, err := a.tx.Exec(query, args...)
	return err
}
func (a sqlTxAdapter) ExecContext(ctx context.Context, query string, args ...any) error {
	_, err := a.tx.ExecContext(ctx, query, args...)
	return err
}
func (a sqlTxAdapter) Query(query string, args ...any) (cellar.ApplicationRows, error) {
	rows, err := a.tx.Query(query, args...)
	return rows, err
}
func (a sqlTxAdapter) QueryContext(ctx context.Context, query string, args ...any) (cellar.ApplicationRows, error) {
	rows, err := a.tx.QueryContext(ctx, query, args...)
	return rows, err
}
func (a sqlTxAdapter) QueryRow(query string, args ...any) cellar.ApplicationRow {
	return a.tx.QueryRow(query, args...)
}
func (a sqlTxAdapter) QueryRowContext(ctx context.Context, query string, args ...any) cellar.ApplicationRow {
	return a.tx.QueryRowContext(ctx, query, args...)
}
