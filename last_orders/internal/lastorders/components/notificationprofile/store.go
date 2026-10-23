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

// Store provides access to notification profile data in the database.
// FIXME arguably this package belongs under database/projections/notificationprofile
type Store struct {
	base *basestore.Store
}

type UserPreferencesMutation struct {
	UserID      string
	Preferences *UserPreferences
}

type EndpointMutation struct {
	UserID     string
	EndpointID string
	Endpoint   *Endpoint
}

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if err := migrate(context.Background(), base); err != nil {
		return nil, err
	}
	return &Store{base: base}, nil
}

func (s *Store) PutPreferences(ctx context.Context, preferences UserPreferences) error {
	return putPreferences(ctx, s.base.DB(), preferences)
}

// ApplyPreferences applies one user-preference source batch atomically. A
// baseline replaces the complete preference projection.
func (s *Store) ApplyPreferences(ctx context.Context, mutations []UserPreferencesMutation, baseline bool) error {
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if baseline {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_user_prefs`); err != nil {
			return err
		}
	}
	for _, mutation := range mutations {
		if mutation.UserID == "" {
			return fmt.Errorf("user ID is required")
		}
		if mutation.Preferences == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM notification_user_prefs WHERE user_id = ?`, mutation.UserID); err != nil {
				return err
			}
			continue
		}
		if mutation.Preferences.UserID != mutation.UserID {
			return fmt.Errorf("user mutation ID %q does not match preferences ID %q", mutation.UserID, mutation.Preferences.UserID)
		}
		if err := putPreferences(ctx, tx, *mutation.Preferences); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func putPreferences(ctx context.Context, execer contextExecer, preferences UserPreferences) error {
	if preferences.UserID == "" {
		return fmt.Errorf("user ID is required")
	}
	muted, err := json.Marshal(nonNilStrings(normaliseStringSet(preferences.EventChatMutedPollIDs)))
	if err != nil {
		return fmt.Errorf("encode muted poll ids: %w", err)
	}
	_, err = execer.ExecContext(ctx, `
		INSERT INTO notification_user_prefs(
			user_id, web_push_enabled, poll_opens, poll_completes,
			global_chat, event_chat, muted_poll_ids_json,
			notification_email, open_poll_email, notification_email_enabled, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			web_push_enabled = excluded.web_push_enabled,
			poll_opens = excluded.poll_opens,
			poll_completes = excluded.poll_completes,
			global_chat = excluded.global_chat,
			event_chat = excluded.event_chat,
			muted_poll_ids_json = excluded.muted_poll_ids_json,
			notification_email = excluded.notification_email,
			open_poll_email = excluded.open_poll_email,
			notification_email_enabled = excluded.notification_email_enabled,
			updated_at = excluded.updated_at
	`, preferences.UserID, preferences.WebPushEnabled, preferences.PollOpens,
		preferences.PollCompletes, preferences.GlobalChat, preferences.EventChat,
		string(muted), preferences.NotificationEmail, preferences.OpenPollEmailEnabled,
		preferences.NotificationEmailEnabled, time.Now().UTC())
	return err
}

func (s *Store) DeletePreferences(ctx context.Context, userID string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM notification_user_prefs WHERE user_id = ?`, userID)
	return err
}

func (s *Store) Preferences(ctx context.Context, userID string) (UserPreferences, error) {
	user, updatedAt, err := readPrivateUser(ctx, s.base.DB(), userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return UserPreferences{}, ErrNotFound
		}
		return UserPreferences{}, err
	}
	preferences := user.Preferences()
	preferences.UpdatedAt = updatedAt
	return preferences, nil
}

func (s *Store) PutEndpoint(ctx context.Context, endpoint Endpoint) error {
	return putEndpoint(ctx, s.base.DB(), endpoint)
}

// ApplyEndpoints applies one endpoint source batch atomically. A baseline
// replaces the complete endpoint projection.
func (s *Store) ApplyEndpoints(ctx context.Context, mutations []EndpointMutation, baseline bool) error {
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if baseline {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_endpoints`); err != nil {
			return err
		}
	}
	for _, mutation := range mutations {
		if mutation.UserID == "" || mutation.EndpointID == "" {
			return fmt.Errorf("endpoint user ID and endpoint ID are required")
		}
		if mutation.Endpoint == nil {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM notification_endpoints WHERE user_id = ? AND endpoint_id = ?
			`, mutation.UserID, mutation.EndpointID); err != nil {
				return err
			}
			continue
		}
		if mutation.Endpoint.UserID != mutation.UserID || mutation.Endpoint.EndpointID != mutation.EndpointID {
			return fmt.Errorf("endpoint mutation key %q/%q does not match endpoint key %q/%q",
				mutation.UserID, mutation.EndpointID, mutation.Endpoint.UserID, mutation.Endpoint.EndpointID)
		}
		if err := putEndpoint(ctx, tx, *mutation.Endpoint); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func putEndpoint(ctx context.Context, execer contextExecer, endpoint Endpoint) error {
	return putEndpointContribution(ctx, execer, EndpointContribution{
		UserID: endpoint.UserID, EndpointID: endpoint.EndpointID,
		URL: endpoint.URL, P256DH: endpoint.P256DH, Auth: endpoint.Auth, Active: &endpoint.Active,
	})
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
			WHERE e.active = 1 AND p.web_push_enabled = 1 AND COALESCE(p.%s, ?) = 1
		`, column)
		args = append(args, selector.Kind == KindPollOpens || selector.Kind == KindPollCompletes)
	} else {
		query.WriteString(`
			JOIN notification_user_prefs p ON p.user_id = e.user_id
			WHERE e.active = 1 AND p.web_push_enabled = 1
		`)
	}

	if len(selector.UserIDs) > 0 {
		query.WriteString(` AND e.user_id IN (`)
		query.WriteString(placeholders(len(selector.UserIDs)))
		query.WriteString(`)`)
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

// EmailRecipients returns users opted in to kind with a usable address.
func (s *Store) EmailRecipients(ctx context.Context, kind EmailKind) ([]EmailRecipient, error) {
	column, err := emailPreferenceColumn(kind)
	if err != nil {
		return nil, err
	}
	rows, err := s.base.DB().QueryContext(ctx, fmt.Sprintf(`
		SELECT user_id, notification_email
		FROM notification_user_prefs
		WHERE %s = 1 AND notification_email <> ''
		ORDER BY user_id
	`, column))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := []EmailRecipient{}
	for rows.Next() {
		var recipient EmailRecipient
		if err := rows.Scan(&recipient.UserID, &recipient.Email); err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

// emailPreferenceColumn must only return fixed literals because its result is
// interpolated into SQL.
func emailPreferenceColumn(kind EmailKind) (string, error) {
	switch kind {
	case EmailPollOpens:
		return "open_poll_email", nil
	case EmailPollCompletes:
		return "notification_email_enabled", nil
	default:
		return "", fmt.Errorf("%w: email %q", ErrUnknownKind, kind)
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
