package venuecache

import (
	"context"
	"database/sql"
	"fmt"

	"last_orders/internal/lastorders/basestore"
)

func migrate(ctx context.Context, base *basestore.Store) error {
	if _, err := base.DB().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS venue_cache_schema (
			singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
			version INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create venue cache migration metadata: %w", err)
	}
	tx, err := base.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin venue cache migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO venue_cache_schema(singleton, version) VALUES(1, 0)
		ON CONFLICT(singleton) DO NOTHING
	`); err != nil {
		return fmt.Errorf("initialise venue cache migration metadata: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM venue_cache_schema WHERE singleton = 1`).Scan(&version); err != nil {
		return fmt.Errorf("read venue cache schema version: %w", err)
	}
	if version != 0 && version != 1 {
		return fmt.Errorf("unsupported venue cache schema version %d", version)
	}
	if version == 0 {
		if err := migrateToV1(ctx, base, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE venue_cache_schema SET version = 1 WHERE singleton = 1`); err != nil {
			return fmt.Errorf("record venue cache schema version: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit venue cache migration: %w", err)
	}
	return nil
}

func migrateToV1(ctx context.Context, base *basestore.Store, tx *sql.Tx) error {
	var existed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'venue_cache')
	`).Scan(&existed); err != nil {
		return fmt.Errorf("inspect venue cache schema: %w", err)
	}
	if existed {
		if _, err := tx.ExecContext(ctx, `DROP TABLE venue_cache`); err != nil {
			return fmt.Errorf("discard legacy venue cache: %w", err)
		}
		if _, err := base.ChangeGeneration(ctx, tx); err != nil {
			return fmt.Errorf("invalidate legacy venue cache generation: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE venue_cache (
			venue_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			venue_type TEXT NOT NULL DEFAULT '',
			website TEXT NOT NULL DEFAULT '',
			map TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			photo_url TEXT NOT NULL DEFAULT '',
			recurrence_json TEXT NOT NULL DEFAULT '',
			next_occurrence_date TEXT NOT NULL DEFAULT '',
			notes TEXT,
			parking INTEGER CHECK(parking IN (0, 1)),
			food INTEGER CHECK(food IN (0, 1)),
			dog_friendly INTEGER CHECK(dog_friendly IN (0, 1)),
			beer_garden INTEGER CHECK(beer_garden IN (0, 1)),
			out_of_town INTEGER CHECK(out_of_town IN (0, 1)),
			banned INTEGER CHECK(banned IN (0, 1)),
			change_version INTEGER NOT NULL CHECK(change_version > 0),
			deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0, 1))
		);
		CREATE INDEX venue_cache_changes ON venue_cache(change_version, venue_id);
	`); err != nil {
		return fmt.Errorf("create venue_cache schema: %w", err)
	}
	return nil
}
