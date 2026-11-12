package notificationprofile

import (
	"context"
	"database/sql"
	"fmt"

	"last_orders/internal/lastorders/basestore"
)

func migrate(ctx context.Context, base *basestore.Store) error {
	if _, err := base.DB().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS notification_profile_schema (
			singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
			version INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create notification profile migration metadata: %w", err)
	}
	tx, err := base.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notification profile migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_profile_schema(singleton, version) VALUES(1, 0)
		ON CONFLICT(singleton) DO NOTHING
	`); err != nil {
		return fmt.Errorf("initialise notification profile migration metadata: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM notification_profile_schema WHERE singleton = 1`).Scan(&version); err != nil {
		return fmt.Errorf("read notification profile schema version: %w", err)
	}
	if version != 0 && version != 1 {
		return fmt.Errorf("unsupported notification profile schema version %d", version)
	}
	if version == 0 {
		if err := migrateToV1(ctx, base, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE notification_profile_schema SET version = 1 WHERE singleton = 1`); err != nil {
			return fmt.Errorf("record notification profile schema version: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification profile migration: %w", err)
	}
	return nil
}

func migrateToV1(ctx context.Context, base *basestore.Store, tx *sql.Tx) error {
	var existed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sqlite_master
			WHERE type = 'table' AND name IN ('notification_user_prefs', 'notification_endpoints'))
	`).Scan(&existed); err != nil {
		return fmt.Errorf("inspect legacy notification profile schema: %w", err)
	}
	if existed {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS notification_user_prefs; DROP TABLE IF EXISTS notification_endpoints;`); err != nil {
			return fmt.Errorf("discard legacy notification projection: %w", err)
		}
		if _, err := base.ChangeGeneration(ctx, tx); err != nil {
			return fmt.Errorf("invalidate legacy notification projection generation: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE notification_user_prefs (
			user_id TEXT PRIMARY KEY,
			email TEXT,
			default_arrival_time TEXT,
			test_email_req TEXT,
			test_email_ack TEXT,
			web_push_enabled INTEGER CHECK(web_push_enabled IN (0, 1)),
			poll_opens INTEGER CHECK(poll_opens IN (0, 1)),
			poll_completes INTEGER CHECK(poll_completes IN (0, 1)),
			global_chat INTEGER CHECK(global_chat IN (0, 1)),
			event_chat INTEGER CHECK(event_chat IN (0, 1)),
			muted_poll_ids_json TEXT NOT NULL DEFAULT '[]',
			notification_email TEXT,
			open_poll_email INTEGER CHECK(open_poll_email IN (0, 1)),
			notification_email_enabled INTEGER CHECK(notification_email_enabled IN (0, 1)),
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE notification_public_users (
			user_id TEXT PRIMARY KEY,
			name_present INTEGER NOT NULL CHECK(name_present IN (0, 1)),
			name TEXT,
			photo_url_present INTEGER NOT NULL CHECK(photo_url_present IN (0, 1)),
			photo_url TEXT,
			custom_photo_url TEXT,
			votes_visible_present INTEGER NOT NULL CHECK(votes_visible_present IN (0, 1)),
			votes_visible INTEGER CHECK(votes_visible IN (0, 1)),
			CHECK(name_present = 1 OR name IS NULL),
			CHECK(photo_url_present = 1 OR photo_url IS NULL),
			CHECK(votes_visible_present = 1 OR votes_visible IS NULL)
		);
		CREATE TABLE notification_endpoints (
			user_id TEXT NOT NULL,
			endpoint_id TEXT NOT NULL,
			endpoint_url TEXT NOT NULL,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			active INTEGER CHECK(active IN (0, 1)),
			updated_at DATETIME NOT NULL,
			PRIMARY KEY(user_id, endpoint_id)
		);
		CREATE INDEX notification_endpoints_user ON notification_endpoints(user_id);
		CREATE TABLE notification_profiles (
			user_id TEXT PRIMARY KEY,
			change_version INTEGER NOT NULL CHECK(change_version > 0),
			deleted INTEGER NOT NULL CHECK(deleted IN (0, 1))
		);
		CREATE INDEX notification_profile_changes ON notification_profiles(change_version, user_id);
	`); err != nil {
		return fmt.Errorf("create notification profile schema: %w", err)
	}
	return nil
}
