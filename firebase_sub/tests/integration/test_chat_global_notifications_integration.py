"""Integration tests for global chat recipient resolution.

Critical matrix covered:
- User A: signed up for all notifications.
- User B: signed up for global chat notifications only.
- User C: signed up for no notifications.

Scenarios:
1) A posts in global chat -> notify B only.
2) C posts in global chat -> notify A and B.
"""

import pytest

from firebase_sub.database.handlers import DbHandler
from firebase_sub.plugins.chat_push import process_chat_message_push


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

    # A valid active endpoint is required for chat_message_push_handler to include a user.
    firestore_client.collection("users").document(uid).collection(
        "push_endpoints"
    ).document(f"ep-{uid}").set(
        {
            "endpoint": f"https://push.example/{uid}",
            "p256dh": "test-p256dh-key",
            "auth": "test-auth-key",
            "active": True,
        }
    )


def _seed_global_chat_matrix(firestore_client) -> None:
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

    # User B: global chat only.
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


def _create_global_message(
    firestore_client, *, message_id: str, author_uid: str
) -> None:
    firestore_client.collection("messages").document(message_id).set(
        {
            "uid": author_uid,
            "name": author_uid,
            "text": f"message from {author_uid}",
            "scopeType": "global",
            "scopeId": "main",
        }
    )


@pytest.mark.integration
class TestGlobalChatNotifications:
    def test_author_a_notified_users_are_b_only(self, firestore_client):
        """A posts: A excluded as author; B notified; C not notified."""
        _seed_global_chat_matrix(firestore_client)
        message_id = "global-msg-a"
        _create_global_message(
            firestore_client,
            message_id=message_id,
            author_uid="user-a",
        )

        handler = DbHandler()
        message_doc = firestore_client.collection("messages").document(message_id).get()
        process_chat_message_push(handler, message_id, message_doc, dummy_run=True)

        action_doc = (
            firestore_client.collection("chat_push_actions")
            .document(message_id)
            .get()
            .to_dict()
        )
        assert action_doc is not None
        assert action_doc["scopeType"] == "global"
        assert action_doc["scopeId"] == "main"
        assert set(action_doc["notified"]) == {"user-b"}

    def test_author_c_notified_users_are_a_and_b(self, firestore_client):
        """C posts: C excluded (no preference); A and B notified."""
        _seed_global_chat_matrix(firestore_client)
        message_id = "global-msg-c"
        _create_global_message(
            firestore_client,
            message_id=message_id,
            author_uid="user-c",
        )

        handler = DbHandler()
        message_doc = firestore_client.collection("messages").document(message_id).get()
        process_chat_message_push(handler, message_id, message_doc, dummy_run=True)

        action_doc = (
            firestore_client.collection("chat_push_actions")
            .document(message_id)
            .get()
            .to_dict()
        )
        assert action_doc is not None
        assert action_doc["scopeType"] == "global"
        assert action_doc["scopeId"] == "main"
        assert set(action_doc["notified"]) == {"user-a", "user-b"}
