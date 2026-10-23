package notificationprofile

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type contextQueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func putPrivateUser(ctx context.Context, execer contextExecer, user PrivateUserContribution) error {
	if user.UserID == "" {
		return fmt.Errorf("user ID is required")
	}
	muted, err := json.Marshal(nonNilStrings(normaliseStringSet(user.EventChatMutedPollIDs)))
	if err != nil {
		return fmt.Errorf("encode muted poll ids: %w", err)
	}
	_, err = execer.ExecContext(ctx, `
		INSERT INTO notification_user_prefs(
			user_id, email, default_arrival_time, test_email_req, test_email_ack,
			web_push_enabled, poll_opens, poll_completes, global_chat, event_chat,
			muted_poll_ids_json, notification_email, open_poll_email,
			notification_email_enabled, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			email = excluded.email,
			default_arrival_time = excluded.default_arrival_time,
			test_email_req = excluded.test_email_req,
			test_email_ack = excluded.test_email_ack,
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
	`, user.UserID, user.Email, user.DefaultArrivalTime, user.TestEmailReq, user.TestEmailAck,
		user.WebPushEnabled, user.PollOpens, user.PollCompletes, user.GlobalChat, user.EventChat,
		string(muted), user.NotificationEmail, user.OpenPollEmailEnabled, user.NotificationEmailEnabled,
		time.Now().UTC())
	return err
}

func readPrivateUser(ctx context.Context, queryer contextQueryRower, userID string) (PrivateUserContribution, time.Time, error) {
	var user PrivateUserContribution
	var updatedAt time.Time
	var muted string
	err := queryer.QueryRowContext(ctx, `
		SELECT user_id, email, default_arrival_time, test_email_req, test_email_ack,
			web_push_enabled, poll_opens, poll_completes, global_chat, event_chat,
			muted_poll_ids_json, notification_email, open_poll_email,
			notification_email_enabled, updated_at
		FROM notification_user_prefs WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Email, &user.DefaultArrivalTime, &user.TestEmailReq,
		&user.TestEmailAck, &user.WebPushEnabled, &user.PollOpens, &user.PollCompletes,
		&user.GlobalChat, &user.EventChat, &muted, &user.NotificationEmail,
		&user.OpenPollEmailEnabled, &user.NotificationEmailEnabled, &updatedAt)
	if err != nil {
		return PrivateUserContribution{}, time.Time{}, err
	}
	if err := json.Unmarshal([]byte(muted), &user.EventChatMutedPollIDs); err != nil {
		return PrivateUserContribution{}, time.Time{}, fmt.Errorf("decode muted poll ids for %q: %w", userID, err)
	}
	user.EventChatMutedPollIDs = normaliseStringSet(user.EventChatMutedPollIDs)
	return user, updatedAt, nil
}

func nullableStorageValue[Value comparable](field NullableField[Value]) *Value {
	if !field.Present {
		return nil
	}
	return field.Value
}

func putPublicUser(ctx context.Context, execer contextExecer, user PublicUserContribution) error {
	if user.UserID == "" {
		return fmt.Errorf("user ID is required")
	}
	_, err := execer.ExecContext(ctx, `
		INSERT INTO notification_public_users(
			user_id, name_present, name, photo_url_present, photo_url,
			custom_photo_url, votes_visible_present, votes_visible
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			name_present = excluded.name_present,
			name = excluded.name,
			photo_url_present = excluded.photo_url_present,
			photo_url = excluded.photo_url,
			custom_photo_url = excluded.custom_photo_url,
			votes_visible_present = excluded.votes_visible_present,
			votes_visible = excluded.votes_visible
	`, user.UserID, user.Name.Present, nullableStorageValue(user.Name), user.PhotoURL.Present,
		nullableStorageValue(user.PhotoURL), user.CustomPhotoURL, user.VotesVisible.Present,
		nullableStorageValue(user.VotesVisible))
	return err
}

func readPublicUser(ctx context.Context, queryer contextQueryRower, userID string) (PublicUserContribution, error) {
	var user PublicUserContribution
	err := queryer.QueryRowContext(ctx, `
		SELECT user_id, name_present, name, photo_url_present, photo_url,
			custom_photo_url, votes_visible_present, votes_visible
		FROM notification_public_users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Name.Present, &user.Name.Value, &user.PhotoURL.Present,
		&user.PhotoURL.Value, &user.CustomPhotoURL, &user.VotesVisible.Present, &user.VotesVisible.Value)
	return user, err
}

func putEndpointContribution(ctx context.Context, execer contextExecer, endpoint EndpointContribution) error {
	if endpoint.UserID == "" || endpoint.EndpointID == "" {
		return fmt.Errorf("endpoint user ID and endpoint ID are required")
	}
	_, err := execer.ExecContext(ctx, `
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

func readEndpointContribution(ctx context.Context, queryer contextQueryRower, userID, endpointID string) (EndpointContribution, time.Time, error) {
	var endpoint EndpointContribution
	var updatedAt time.Time
	err := queryer.QueryRowContext(ctx, `
		SELECT user_id, endpoint_id, endpoint_url, p256dh, auth, active, updated_at
		FROM notification_endpoints WHERE user_id = ? AND endpoint_id = ?
	`, userID, endpointID).Scan(&endpoint.UserID, &endpoint.EndpointID, &endpoint.URL,
		&endpoint.P256DH, &endpoint.Auth, &endpoint.Active, &updatedAt)
	return endpoint, updatedAt, err
}
