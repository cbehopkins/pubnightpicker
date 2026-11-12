package notificationprofile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

type profileState struct {
	private   *PrivateUserContribution
	public    *PublicUserContribution
	endpoints []EndpointContribution
}

func (state profileState) live() bool {
	return state.private != nil || state.public != nil
}

func (state profileState) equal(other profileState) bool {
	if !state.live() && !other.live() {
		return true
	}
	if (state.private == nil) != (other.private == nil) || (state.public == nil) != (other.public == nil) {
		return false
	}
	if state.private != nil && !state.private.Equal(*other.private) {
		return false
	}
	if state.public != nil && !state.public.Equal(*other.public) {
		return false
	}
	return slices.EqualFunc(state.endpoints, other.endpoints, EndpointContribution.Equal)
}

func readProfileState(ctx context.Context, tx *sql.Tx, userID string) (profileState, error) {
	var state profileState
	private, _, err := readPrivateUser(ctx, tx, userID)
	if err == nil {
		state.private = &private
	} else if !errors.Is(err, sql.ErrNoRows) {
		return profileState{}, err
	}
	public, err := readPublicUser(ctx, tx, userID)
	if err == nil {
		state.public = &public
	} else if !errors.Is(err, sql.ErrNoRows) {
		return profileState{}, err
	}
	if !state.live() {
		return state, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT user_id, endpoint_id, endpoint_url, p256dh, auth, active
		FROM notification_endpoints WHERE user_id = ? ORDER BY endpoint_id
	`, userID)
	if err != nil {
		return profileState{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var endpoint EndpointContribution
		if err := rows.Scan(&endpoint.UserID, &endpoint.EndpointID, &endpoint.URL,
			&endpoint.P256DH, &endpoint.Auth, &endpoint.Active); err != nil {
			return profileState{}, err
		}
		state.endpoints = append(state.endpoints, endpoint)
	}
	if err := rows.Err(); err != nil {
		return profileState{}, err
	}
	return state, nil
}

func (s *Store) writeProfiles(ctx context.Context, userIDs []string, write func(*sql.Tx) error) error {
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.writeProfilesInTransaction(ctx, tx, userIDs, write); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) writeProfilesInTransaction(ctx context.Context, tx *sql.Tx, userIDs []string, write func(*sql.Tx) error) error {
	userIDs = normaliseStringSet(userIDs)
	before := make([]profileState, len(userIDs))
	for index, userID := range userIDs {
		if userID == "" {
			return fmt.Errorf("user ID is required")
		}
		state, err := readProfileState(ctx, tx, userID)
		if err != nil {
			return err
		}
		before[index] = state
	}
	if err := write(tx); err != nil {
		return err
	}
	for index, userID := range userIDs {
		after, err := readProfileState(ctx, tx, userID)
		if err != nil {
			return err
		}
		if before[index].equal(after) {
			continue
		}
		sequence, err := s.base.NextSequence(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_profiles(user_id, change_version, deleted) VALUES(?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET change_version = excluded.change_version, deleted = excluded.deleted
		`, userID, sequence, !after.live()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PutPrivateUser(ctx context.Context, user PrivateUserContribution) error {
	return s.ApplyPrivateUsers(ctx, []PrivateUserMutation{{UserID: user.UserID, User: &user}}, false)
}

type PrivateUserMutation struct {
	UserID string
	User   *PrivateUserContribution
}

type PublicUserMutation struct {
	UserID string
	User   *PublicUserContribution
}

type EndpointContributionMutation struct {
	UserID     string
	EndpointID string
	Endpoint   *EndpointContribution
}

func writePrivateUser(ctx context.Context, tx *sql.Tx, user PrivateUserContribution) error {
	current, _, err := readPrivateUser(ctx, tx, user.UserID)
	if err == nil && current.Equal(user) {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return putPrivateUser(ctx, tx, user)
}

func writePublicUser(ctx context.Context, tx *sql.Tx, user PublicUserContribution) error {
	current, err := readPublicUser(ctx, tx, user.UserID)
	if err == nil && current.Equal(user) {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return putPublicUser(ctx, tx, user)
}

func writeEndpointContribution(ctx context.Context, tx *sql.Tx, endpoint EndpointContribution) error {
	current, _, err := readEndpointContribution(ctx, tx, endpoint.UserID, endpoint.EndpointID)
	if err == nil && current.Equal(endpoint) {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return putEndpointContribution(ctx, tx, endpoint)
}

func (s *Store) applyUserBatch(ctx context.Context, table string, userIDs []string, baseline bool, write func(*sql.Tx) error) error {
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	owners := slices.Clone(userIDs)
	var removed []string
	if baseline {
		retained := make(map[string]bool, len(userIDs))
		for _, userID := range userIDs {
			if retained[userID] {
				return fmt.Errorf("duplicate baseline user ID %q", userID)
			}
			retained[userID] = true
		}
		rows, err := tx.QueryContext(ctx, `SELECT user_id FROM `+table+` ORDER BY user_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				return err
			}
			if !retained[userID] {
				removed = append(removed, userID)
				owners = append(owners, userID)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	if err := s.writeProfilesInTransaction(ctx, tx, owners, func(tx *sql.Tx) error {
		for _, userID := range removed {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE user_id = ?`, userID); err != nil {
				return err
			}
		}
		return write(tx)
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ApplyPrivateUsers(ctx context.Context, mutations []PrivateUserMutation, baseline bool) error {
	userIDs := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.UserID == "" || (mutation.User != nil && mutation.User.UserID != mutation.UserID) {
			return fmt.Errorf("private user mutation requires matching user IDs")
		}
		userIDs = append(userIDs, mutation.UserID)
	}
	return s.applyUserBatch(ctx, "notification_user_prefs", userIDs, baseline, func(tx *sql.Tx) error {
		for _, mutation := range mutations {
			if mutation.User == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM notification_user_prefs WHERE user_id = ?`, mutation.UserID); err != nil {
					return err
				}
			} else if err := writePrivateUser(ctx, tx, *mutation.User); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) PutPublicUser(ctx context.Context, user PublicUserContribution) error {
	return s.ApplyPublicUsers(ctx, []PublicUserMutation{{UserID: user.UserID, User: &user}}, false)
}

func (s *Store) DeletePublicUser(ctx context.Context, userID string) error {
	return s.ApplyPublicUsers(ctx, []PublicUserMutation{{UserID: userID}}, false)
}

func (s *Store) ApplyPublicUsers(ctx context.Context, mutations []PublicUserMutation, baseline bool) error {
	userIDs := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.UserID == "" || (mutation.User != nil && mutation.User.UserID != mutation.UserID) {
			return fmt.Errorf("public user mutation requires matching user IDs")
		}
		userIDs = append(userIDs, mutation.UserID)
	}
	return s.applyUserBatch(ctx, "notification_public_users", userIDs, baseline, func(tx *sql.Tx) error {
		for _, mutation := range mutations {
			if mutation.User == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM notification_public_users WHERE user_id = ?`, mutation.UserID); err != nil {
					return err
				}
			} else if err := writePublicUser(ctx, tx, *mutation.User); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) PutEndpointContribution(ctx context.Context, endpoint EndpointContribution) error {
	return s.ApplyEndpointContributions(ctx, []EndpointContributionMutation{{UserID: endpoint.UserID, EndpointID: endpoint.EndpointID, Endpoint: &endpoint}}, false)
}

type endpointKey struct {
	userID     string
	endpointID string
}

func (s *Store) ApplyEndpointContributions(ctx context.Context, mutations []EndpointContributionMutation, baseline bool) error {
	owners := make([]string, 0, len(mutations))
	retained := make(map[endpointKey]bool, len(mutations))
	for _, mutation := range mutations {
		if mutation.UserID == "" || mutation.EndpointID == "" {
			return fmt.Errorf("endpoint user ID and endpoint ID are required")
		}
		if mutation.Endpoint != nil && (mutation.Endpoint.UserID != mutation.UserID || mutation.Endpoint.EndpointID != mutation.EndpointID) {
			return fmt.Errorf("endpoint mutation requires matching owner and endpoint IDs")
		}
		key := endpointKey{mutation.UserID, mutation.EndpointID}
		if baseline && retained[key] {
			return fmt.Errorf("duplicate baseline endpoint %q/%q", mutation.UserID, mutation.EndpointID)
		}
		retained[key] = true
		owners = append(owners, mutation.UserID)
	}
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var removed []endpointKey
	if baseline {
		rows, err := tx.QueryContext(ctx, `SELECT user_id, endpoint_id FROM notification_endpoints ORDER BY user_id, endpoint_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key endpointKey
			if err := rows.Scan(&key.userID, &key.endpointID); err != nil {
				return err
			}
			if !retained[key] {
				removed = append(removed, key)
				owners = append(owners, key.userID)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	if err := s.writeProfilesInTransaction(ctx, tx, owners, func(tx *sql.Tx) error {
		for _, key := range removed {
			if _, err := tx.ExecContext(ctx, `DELETE FROM notification_endpoints WHERE user_id = ? AND endpoint_id = ?`, key.userID, key.endpointID); err != nil {
				return err
			}
		}
		for _, mutation := range mutations {
			if mutation.Endpoint == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM notification_endpoints WHERE user_id = ? AND endpoint_id = ?`, mutation.UserID, mutation.EndpointID); err != nil {
					return err
				}
			} else if err := writeEndpointContribution(ctx, tx, *mutation.Endpoint); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func writePreferences(ctx context.Context, tx *sql.Tx, preferences UserPreferences) error {
	user, _, err := readPrivateUser(ctx, tx, preferences.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		user = PrivateUserContribution{UserID: preferences.UserID}
	} else if err != nil {
		return err
	}
	user.WebPushEnabled = &preferences.WebPushEnabled
	user.PollOpens = &preferences.PollOpens
	user.PollCompletes = &preferences.PollCompletes
	user.GlobalChat = &preferences.GlobalChat
	user.EventChat = &preferences.EventChat
	user.NotificationEmail = &preferences.NotificationEmail
	user.OpenPollEmailEnabled = &preferences.OpenPollEmailEnabled
	user.NotificationEmailEnabled = &preferences.NotificationEmailEnabled
	user.EventChatMutedPollIDs = normaliseStringSet(preferences.EventChatMutedPollIDs)
	return writePrivateUser(ctx, tx, user)
}
