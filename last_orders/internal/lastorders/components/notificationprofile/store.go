package notificationprofile

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"last_orders/internal/lastorders/basestore"
)

type Store struct {
	base *basestore.Store
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if _, err := base.DB().Exec(`
		CREATE TABLE IF NOT EXISTS notification_user_prefs (
			user_id TEXT PRIMARY KEY,
			web_push_enabled INTEGER NOT NULL DEFAULT 0,
			poll_opens INTEGER NOT NULL DEFAULT 1,
			poll_completes INTEGER NOT NULL DEFAULT 1,
			global_chat INTEGER NOT NULL DEFAULT 0,
			event_chat INTEGER NOT NULL DEFAULT 0,
			muted_poll_ids_json TEXT NOT NULL DEFAULT '[]',
			updated_at DATETIME NOT NULL
		);
	`); err != nil {
		return nil, fmt.Errorf("create notification_user_prefs schema: %w", err)
	}
	if _, err := base.DB().Exec(`
		CREATE TABLE IF NOT EXISTS notification_endpoints (
			user_id TEXT NOT NULL,
			endpoint_id TEXT NOT NULL,
			endpoint_url TEXT NOT NULL,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY(user_id, endpoint_id)
		);
	`); err != nil {
		return nil, fmt.Errorf("create notification_endpoints schema: %w", err)
	}
	if _, err := base.DB().Exec(`
		CREATE INDEX IF NOT EXISTS notification_endpoints_user ON notification_endpoints(user_id);
	`); err != nil {
		return nil, fmt.Errorf("create notification_endpoints index: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) PutPreferences(ctx context.Context, preferences UserPreferences) error {
	if preferences.UserID == "" {
		return fmt.Errorf("user ID is required")
	}
	muted, err := json.Marshal(nonNilStrings(preferences.EventChatMutedPollIDs))
	if err != nil {
		return fmt.Errorf("encode muted poll ids: %w", err)
	}
	_, err = s.base.DB().ExecContext(ctx, `
		INSERT INTO notification_user_prefs(
			user_id, web_push_enabled, poll_opens, poll_completes,
			global_chat, event_chat, muted_poll_ids_json, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			web_push_enabled = excluded.web_push_enabled,
			poll_opens = excluded.poll_opens,
			poll_completes = excluded.poll_completes,
			global_chat = excluded.global_chat,
			event_chat = excluded.event_chat,
			muted_poll_ids_json = excluded.muted_poll_ids_json,
			updated_at = excluded.updated_at
	`, preferences.UserID, preferences.WebPushEnabled, preferences.PollOpens,
		preferences.PollCompletes, preferences.GlobalChat, preferences.EventChat,
		string(muted), time.Now().UTC())
	return err
}

func (s *Store) DeletePreferences(ctx context.Context, userID string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM notification_user_prefs WHERE user_id = ?`, userID)
	return err
}

func (s *Store) Preferences(ctx context.Context, userID string) (UserPreferences, error) {
	var preferences UserPreferences
	var muted string
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT user_id, web_push_enabled, poll_opens, poll_completes,
			global_chat, event_chat, muted_poll_ids_json, updated_at
		FROM notification_user_prefs WHERE user_id = ?
	`, userID).Scan(
		&preferences.UserID, &preferences.WebPushEnabled, &preferences.PollOpens,
		&preferences.PollCompletes, &preferences.GlobalChat, &preferences.EventChat,
		&muted, &preferences.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return UserPreferences{}, ErrNotFound
		}
		return UserPreferences{}, err
	}
	if err := json.Unmarshal([]byte(muted), &preferences.EventChatMutedPollIDs); err != nil {
		return UserPreferences{}, fmt.Errorf("decode muted poll ids for %q: %w", userID, err)
	}
	return preferences, nil
}

func (s *Store) PutEndpoint(ctx context.Context, endpoint Endpoint) error {
	if endpoint.UserID == "" || endpoint.EndpointID == "" {
		return fmt.Errorf("endpoint user ID and endpoint ID are required")
	}
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO notification_endpoints(
			user_id, endpoint_id, endpoint_url, p256dh, auth, active, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, endpoint_id) DO UPDATE SET
			endpoint_url = excluded.endpoint_url,
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			active = excluded.active,
			updated_at = excluded.updated_at
	`, endpoint.UserID, endpoint.EndpointID, endpoint.URL, endpoint.P256DH,
		endpoint.Auth, endpoint.Active, time.Now().UTC())
	return err
}

func (s *Store) DeleteEndpoint(ctx context.Context, userID, endpointID string) error {
	_, err := s.base.DB().ExecContext(ctx, `
		DELETE FROM notification_endpoints WHERE user_id = ? AND endpoint_id = ?
	`, userID, endpointID)
	return err
}

func (s *Store) SetEndpointActive(ctx context.Context, userID, endpointID string, active bool) error {
	_, err := s.base.DB().ExecContext(ctx, `
		UPDATE notification_endpoints SET active = ?, updated_at = ?
		WHERE user_id = ? AND endpoint_id = ?
	`, active, time.Now().UTC(), userID, endpointID)
	return err
}

// EligibleEndpoints returns the active endpoints whose owner has opted in to the
// selector's notification kind. It replaces the Python collection-group query.
func (s *Store) EligibleEndpoints(ctx context.Context, selector Selector) ([]Endpoint, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT e.user_id, e.endpoint_id, e.endpoint_url, e.p256dh, e.auth, e.active, e.updated_at
		FROM notification_endpoints e
	`)
	args := []any{}

	if selector.Kind != KindDiagnostic {
		column, err := preferenceColumn(selector.Kind)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&query, `
			JOIN notification_user_prefs p ON p.user_id = e.user_id
			WHERE e.active = 1 AND p.web_push_enabled = 1 AND p.%s = 1
		`, column)
	} else {
		query.WriteString(` WHERE e.active = 1`)
	}

	if len(selector.UserIDs) > 0 {
		query.WriteString(` AND e.user_id IN (` + placeholders(len(selector.UserIDs)) + `)`)
		for _, userID := range selector.UserIDs {
			args = append(args, userID)
		}
	}
	query.WriteString(` ORDER BY e.user_id, e.endpoint_id`)

	rows, err := s.base.DB().QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var endpoint Endpoint
		if err := rows.Scan(&endpoint.UserID, &endpoint.EndpointID, &endpoint.URL,
			&endpoint.P256DH, &endpoint.Auth, &endpoint.Active, &endpoint.UpdatedAt); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, rows.Err()
}

// preferenceColumn maps a Kind onto its column. The result is interpolated into SQL,
// so it must only ever return a fixed literal.
func preferenceColumn(kind Kind) (string, error) {
	switch kind {
	case KindPollOpens:
		return "poll_opens", nil
	case KindPollCompletes:
		return "poll_completes", nil
	case KindGlobalChat:
		return "global_chat", nil
	case KindEventChat:
		return "event_chat", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
