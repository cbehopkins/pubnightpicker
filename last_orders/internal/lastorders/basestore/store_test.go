package basestore

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func readTestState(t *testing.T, store *Store) SyncState {
	t.Helper()
	tx, err := store.DB().BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state, err := store.ReadSyncState(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSyncStateInitialisationAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store := openTestStore(t, path)
	initial := readTestState(t, store)
	if _, err := uuid.Parse(initial.Generation); err != nil || initial.Sequence != 0 {
		t.Fatalf("initial state = %+v, generation error = %v", initial, err)
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if sequence, err := store.NextSequence(ctx, tx); err != nil || sequence != 1 {
		t.Fatalf("first sequence = %d, %v", sequence, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestStore(t, path)
	want := SyncState{Generation: initial.Generation, Sequence: 1}
	if got := readTestState(t, reopened); got != want {
		t.Fatalf("reopened state = %+v, want %+v", got, want)
	}
	fresh := openTestStore(t, filepath.Join(t.TempDir(), "fresh.db"))
	if got := readTestState(t, fresh); got.Generation == initial.Generation || got.Sequence != 0 {
		t.Fatalf("new database state = %+v; old generation = %s", got, initial.Generation)
	}
}

func TestSyncMetadataRollbackAndCommit(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	initial := readTestState(t, store)
	for _, commit := range []bool{false, true} {
		tx, err := store.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		sequence, err := store.NextSequence(ctx, tx)
		if err != nil || sequence != 1 {
			t.Fatalf("sequence = %d, %v; want 1", sequence, err)
		}
		generation, err := store.ChangeGeneration(ctx, tx)
		if err != nil || generation == initial.Generation {
			t.Fatalf("changed generation = %s, %v", generation, err)
		}
		inside, err := store.ReadSyncState(ctx, tx)
		want := SyncState{Generation: generation, Sequence: 1}
		if err != nil || inside != want {
			t.Fatalf("transaction state = %+v, %v; want %+v", inside, err, want)
		}
		if got := readTestState(t, store); got != initial {
			t.Fatalf("uncommitted metadata visible: %+v", got)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
			want = initial
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := readTestState(t, store); got != want {
			t.Fatalf("state after commit=%v: %+v; want %+v", commit, got, want)
		}
	}
}

func TestSyncStateReadTransactionSnapshot(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	if _, err := store.DB().Exec(`CREATE TABLE resource(value TEXT NOT NULL); INSERT INTO resource VALUES('before')`); err != nil {
		t.Fatal(err)
	}
	reader, err := store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	before, err := store.ReadSyncState(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := store.NextSequence(ctx, writer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ChangeGeneration(ctx, writer); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `UPDATE resource SET value = 'after'`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ReadSyncState(ctx, reader); err != nil || got != before {
		t.Fatalf("snapshot metadata = %+v, %v; want %+v", got, err, before)
	}
	var value string
	if err := reader.QueryRowContext(ctx, `SELECT value FROM resource`).Scan(&value); err != nil || value != "before" {
		t.Fatalf("snapshot resource = %q, %v; want before", value, err)
	}
	if got := readTestState(t, store); got.Generation == before.Generation || got.Sequence != 1 {
		t.Fatalf("new snapshot metadata = %+v; previous = %+v", got, before)
	}
}

func TestSyncStateConcurrentInitialisationAndAllocation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	initial := readTestState(t, store)
	const workers = 8
	start := make(chan struct{})
	results := make(chan int64, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			<-start
			concurrent, err := New(store.DB())
			if err != nil {
				errors <- err
				return
			}
			tx, err := concurrent.DB().BeginTx(ctx, nil)
			if err != nil {
				errors <- err
				return
			}
			defer tx.Rollback()
			sequence, err := concurrent.NextSequence(ctx, tx)
			if err != nil {
				errors <- err
				return
			}
			state, err := concurrent.ReadSyncState(ctx, tx)
			if err != nil || state.Generation != initial.Generation || state.Sequence != sequence {
				errors <- fmt.Errorf("concurrent state = %+v, %v", state, err)
				return
			}
			if err := tx.Commit(); err != nil {
				errors <- err
				return
			}
			results <- sequence
		})
	}
	close(start)
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	var sequences []int64
	for sequence := range results {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(left, right int) bool { return sequences[left] < sequences[right] })
	if len(sequences) != workers {
		t.Fatalf("committed %d allocations, want %d", len(sequences), workers)
	}
	for index, sequence := range sequences {
		if want := int64(index + 1); sequence != want {
			t.Fatalf("sequence[%d] = %d, want %d", index, sequence, want)
		}
	}
	if got := readTestState(t, store); got != (SyncState{Generation: initial.Generation, Sequence: workers}) {
		t.Fatalf("final state = %+v", got)
	}
}

func TestSyncMetadataRequiresTransaction(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	ctx := context.Background()
	if _, err := store.ReadSyncState(ctx, nil); err == nil {
		t.Fatal("read accepted nil transaction")
	}
	if _, err := store.NextSequence(ctx, nil); err == nil {
		t.Fatal("allocation accepted nil transaction")
	}
	if _, err := store.ChangeGeneration(ctx, nil); err == nil {
		t.Fatal("generation change accepted nil transaction")
	}
}

func TestSequenceExhaustionDoesNotChangeMetadata(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	ctx := context.Background()
	if _, err := store.DB().Exec(`UPDATE projection_sync_state SET sequence = ?`, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := store.NextSequence(ctx, tx); err == nil {
		t.Fatal("sequence overflow succeeded")
	}
	state, err := store.ReadSyncState(ctx, tx)
	if err != nil || state.Sequence != math.MaxInt64 {
		t.Fatalf("exhausted state = %+v, %v", state, err)
	}
}
