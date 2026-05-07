"""Integration tests for event chat recipient resolution.

Matrix baseline:
- User A: all notifications enabled.
- User B: global chat only.
- User C: no notifications.

Additional user:
- User D: eventChat enabled but not attending the event.

Scenarios:
1) C posts in event chat -> notify A only.
2) A posts in event chat -> no notifications (B/C not eligible, A excluded as author, D not attending).
"""

import pytest

from firebase_sub.database.handlers import DbHandler


def _seed_chat_user(
    firestore_client,
    *,
    uid: str,
    web_push_enabled: bool,
    poll_opens: bool,
    poll_completes: bool,
    global_chat: bool,
    event_chat: bool,
) -> None:
    firestore_client.collection("users").document(uid).set(
        {
            "uid": uid,
            "name": uid,
            "webPushEnabled": web_push_enabled,
            "pushPreferences": {
                "pollOpens": poll_opens,
                "pollCompletes": poll_completes,
                "globalChat": global_chat,
                "eventChat": event_chat,
            },
        }
    )

    firestore_client.collection("users").document(uid).collection("push_endpoints").document(
        f"ep-{uid}"
    ).set(
        {
            "endpoint": f"https://push.example/{uid}",
            "p256dh": "test-p256dh-key",
            "auth": "test-auth-key",
            "active": True,
        }
    )


def _seed_event_chat_matrix(firestore_client) -> None:
    # User A: all notifications on.
    _seed_chat_user(
        firestore_client,
        uid="user-a",
        web_push_enabled=True,
        poll_opens=True,
        poll_completes=True,
        global_chat=True,
        event_chat=True,
    )

    # User B: global chat only (event chat off).
    _seed_chat_user(
        firestore_client,
        uid="user-b",
        web_push_enabled=True,
        poll_opens=False,
        poll_completes=False,
        global_chat=True,
        event_chat=False,
    )

    # User C: no notifications.
    _seed_chat_user(
        firestore_client,
        uid="user-c",
        web_push_enabled=False,
        poll_opens=False,
        poll_completes=False,
        global_chat=False,
        event_chat=False,
    )

    # User D: event chat enabled but not attending this event.
    _seed_chat_user(
        firestore_client,
        uid="user-d",
        web_push_enabled=True,
        poll_opens=False,
        poll_completes=False,
        global_chat=False,
        event_chat=True,
    )


def _seed_attendance_for_poll(firestore_client, *, poll_id: str) -> None:
    # Only A, B, and C are attendees for this poll. D intentionally absent.
    firestore_client.collection("attendance").document(poll_id).set(
        {
            "pub-1": {
                "canCome": ["user-a", "user-b", "user-c"],
            }
        }
    )


def _create_event_message(
    firestore_client, *, message_id: str, author_uid: str, poll_id: str
) -> None:
    firestore_client.collection("messages").document(message_id).set(
        {
            "uid": author_uid,
            "name": author_uid,
            "text": f"message from {author_uid}",
            "scopeType": "event",
            "scopeId": poll_id,
        }
    )


@pytest.mark.integration
class TestEventChatNotifications:
    def test_author_c_notified_users_are_a_only(self, firestore_client):
        """C posts in event chat: only A is eligible and attending."""
        poll_id = "poll-event-1"
        _seed_event_chat_matrix(firestore_client)
        _seed_attendance_for_poll(firestore_client, poll_id=poll_id)

        message_id = "event-msg-c"
        _create_event_message(
            firestore_client,
            message_id=message_id,
            author_uid="user-c",
            poll_id=poll_id,
        )

        handler = DbHandler()
        message_doc = firestore_client.collection("messages").document(message_id).get()
        handler.chat_message_push_handler(message_id, message_doc, dummy_run=True)

        action_doc = (
            firestore_client.collection("chat_push_actions")
            .document(message_id)
            .get()
            .to_dict()
        )
        assert action_doc is not None
        assert action_doc["scopeType"] == "event"
        assert action_doc["scopeId"] == poll_id
        assert set(action_doc["notified"]) == {"user-a"}

    def test_author_a_produces_no_event_chat_notifications(self, firestore_client):
        """A posts in event chat: A excluded as author, B/C preferences off, D not attending."""
        poll_id = "poll-event-2"
        _seed_event_chat_matrix(firestore_client)
        _seed_attendance_for_poll(firestore_client, poll_id=poll_id)

        message_id = "event-msg-a"
        _create_event_message(
            firestore_client,
            message_id=message_id,
            author_uid="user-a",
            poll_id=poll_id,
        )

        handler = DbHandler()
        message_doc = firestore_client.collection("messages").document(message_id).get()
        handler.chat_message_push_handler(message_id, message_doc, dummy_run=True)

        action_doc = (
            firestore_client.collection("chat_push_actions")
            .document(message_id)
            .get()
            .to_dict()
        )
        assert action_doc is None
