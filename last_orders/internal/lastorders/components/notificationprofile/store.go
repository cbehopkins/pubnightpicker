package notificationprofile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
	return s.ApplyPreferences(ctx, []UserPreferencesMutation{{UserID: preferences.UserID, Preferences: &preferences}}, false)
}

// ApplyPreferences applies one user-preference source batch atomically. A
// baseline replaces the complete preference projection.
func (s *Store) ApplyPreferences(ctx context.Context, mutations []UserPreferencesMutation, baseline bool) error {
	userIDs := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.UserID == "" || (mutation.Preferences != nil && mutation.Preferences.UserID != mutation.UserID) {
			return fmt.Errorf("preference mutation requires matching user IDs")
		}
		userIDs = append(userIDs, mutation.UserID)
	}
	return s.applyUserBatch(ctx, "notification_user_prefs", userIDs, baseline, func(tx *sql.Tx) error {
		for _, mutation := range mutations {
			if mutation.Preferences == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM notification_user_prefs WHERE user_id = ?`, mutation.UserID); err != nil {
					return err
				}
			} else if err := writePreferences(ctx, tx, *mutation.Preferences); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) DeletePreferences(ctx context.Context, userID string) error {
	return s.ApplyPrivateUsers(ctx, []PrivateUserMutation{{UserID: userID}}, false)
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
	return s.ApplyEndpoints(ctx, []EndpointMutation{{UserID: endpoint.UserID, EndpointID: endpoint.EndpointID, Endpoint: &endpoint}}, false)
}

// ApplyEndpoints applies one endpoint source batch atomically. A baseline
// replaces the complete endpoint projection.
func (s *Store) ApplyEndpoints(ctx context.Context, mutations []EndpointMutation, baseline bool) error {
	contributions := make([]EndpointContributionMutation, len(mutations))
	for index, mutation := range mutations {
		contributions[index] = EndpointContributionMutation{UserID: mutation.UserID, EndpointID: mutation.EndpointID}
		if mutation.Endpoint != nil {
			endpoint := mutation.Endpoint
			contributions[index].Endpoint = &EndpointContribution{
				UserID: endpoint.UserID, EndpointID: endpoint.EndpointID, URL: endpoint.URL,
				P256DH: endpoint.P256DH, Auth: endpoint.Auth, Active: &endpoint.Active,
			}
		}
	}
	return s.ApplyEndpointContributions(ctx, contributions, baseline)
}

func (s *Store) DeleteEndpoint(ctx context.Context, userID, endpointID string) error {
	return s.ApplyEndpointContributions(ctx, []EndpointContributionMutation{{UserID: userID, EndpointID: endpointID}}, false)
}

func (s *Store) SetEndpointActive(ctx context.Context, userID, endpointID string, active bool) error {
	if endpointID == "" {
		return fmt.Errorf("endpoint ID is required")
	}
	return s.writeProfiles(ctx, []string{userID}, func(tx *sql.Tx) error {
		endpoint, _, err := readEndpointContribution(ctx, tx, userID, endpointID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		endpoint.Active = &active
		return writeEndpointContribution(ctx, tx, endpoint)
	})
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
