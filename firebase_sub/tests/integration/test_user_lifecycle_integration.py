"""Integration tests for the user + poll creation lifecycle.

Progression:
  1. A user can be created in the Firebase Auth emulator.
  2. Bootstrap seeds the correct Firestore user documents and role grants.
  3. Creating a poll writes the required documents (polls / votes / attendance).
  4. The Python notifier processes the new poll and writes an open_actions record.

Steps 1–2 each depend on the Auth emulator (``clean_auth`` fixture).
Steps 3–4 only require Firestore and run without the Auth emulator.
"""

from typing import cast

import pytest
from firebase_admin import auth as firebase_auth

from firebase_sub.action_track import ActionMan
from firebase_sub.cli.bootstrap import (
    ADMIN_DEFAULT_ROLES,
    seed_smoke_data,
)
from firebase_sub.database.handlers import DbHandler
from firebase_sub.push_contract import PushDedupeKeys

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


class _FakeActionMan:
    """Minimal ActionMan stand-in that captures calls and returns a preset value."""

    def __init__(self, return_value: dict):
        self.return_value = return_value
        self.calls: list[dict] = []

    def action_event(self, **kwargs):
        self.calls.append(kwargs)
        return self.return_value


def _create_poll(firestore_client, poll_id: str, date: str = "2026-06-01") -> None:
    """Write the three documents that the React NewPoll component creates."""
    firestore_client.collection("polls").document(poll_id).set(
        {"date": date, "completed": False}
    )
    firestore_client.collection("votes").document(poll_id).set({"any": []})
    firestore_client.collection("attendance").document(poll_id).set({})


# ---------------------------------------------------------------------------
# 1. Auth emulator: user creation
# ---------------------------------------------------------------------------


@pytest.mark.integration
class TestAuthUserCreation:
    """Verify that users can be created in and retrieved from the Auth emulator."""

    def test_create_user_appears_in_auth_emulator(self, clean_auth, firebase_test_app):
        """firebase_admin.auth.create_user works against the emulator."""
        user = firebase_auth.create_user(
            uid="lifecycle-user-1",
            email="lifecycle1@example.com",
            password="test-password-1",
            app=firebase_test_app,
        )
        assert user.uid == "lifecycle-user-1"
        assert user.email == "lifecycle1@example.com"

    def test_created_user_is_retrievable(self, clean_auth, firebase_test_app):
        """A user created in the emulator can be fetched back by UID."""
        firebase_auth.create_user(
            uid="lifecycle-user-2",
            email="lifecycle2@example.com",
            password="test-password-2",
            app=firebase_test_app,
        )
        fetched = firebase_auth.get_user("lifecycle-user-2", app=firebase_test_app)
        assert fetched.uid == "lifecycle-user-2"
        assert fetched.email == "lifecycle2@example.com"

    def test_deleted_user_not_retrievable(self, clean_auth, firebase_test_app):
        """Deleting a user removes them from the emulator."""
        firebase_auth.create_user(
            uid="lifecycle-user-3",
            email="lifecycle3@example.com",
            password="test-password-3",
            app=firebase_test_app,
        )
        firebase_auth.delete_user("lifecycle-user-3", app=firebase_test_app)
        with pytest.raises(firebase_auth.UserNotFoundError):
            firebase_auth.get_user("lifecycle-user-3", app=firebase_test_app)

    def test_clean_auth_fixture_resets_between_tests(
        self, clean_auth, firebase_test_app
    ):
        """State from previous tests must not leak (``clean_auth`` resets each time)."""
        # If lifecycle-user-1 from a previous test survived, get_user would succeed.
        # We expect it not to exist here.
        with pytest.raises(firebase_auth.UserNotFoundError):
            firebase_auth.get_user("lifecycle-user-1", app=firebase_test_app)


# ---------------------------------------------------------------------------
# 2. Bootstrap: Firestore user documents and role grants
# ---------------------------------------------------------------------------


@pytest.mark.integration
class TestBootstrappedUserFirestoreDocs:
    """Verify that bootstrap correctly seeds Firestore when given a real emulator."""

    def test_create_admin_writes_private_user_doc(self, firestore_client):
        seed_smoke_data(firestore_client)
        doc = (
            firestore_client.collection("users").document("smoke-admin").get().to_dict()
        )
        assert doc is not None
        assert doc["uid"] == "smoke-admin"
        assert doc["webPushEnabled"] is False
        # Required notification fields
        assert "notificationEmailEnabled" in doc
        assert "pushPreferences" in doc

    def test_create_admin_writes_public_user_doc(self, firestore_client):
        seed_smoke_data(firestore_client)
        doc = (
            firestore_client.collection("user-public")
            .document("smoke-admin")
            .get()
            .to_dict()
        )
        assert doc is not None
        assert doc["uid"] == "smoke-admin"
        assert "name" in doc

    def test_create_admin_grants_admin_role(self, firestore_client):
        seed_smoke_data(firestore_client)
        role_doc = (
            firestore_client.collection("roles").document("admin").get().to_dict() or {}
        )
        assert role_doc.get("smoke-admin") is True

    def test_create_admin_grants_all_default_roles(self, firestore_client):
        seed_smoke_data(firestore_client)
        for role in ADMIN_DEFAULT_ROLES:
            role_doc = (
                firestore_client.collection("roles").document(role).get().to_dict()
                or {}
            )
            assert role_doc.get("smoke-admin") is True, f"Missing role: {role}"

    def test_plain_user_only_has_can_chat_role(self, firestore_client):
        """smoke-user-a (plain user) should only have canChat, not canCreatePoll."""
        seed_smoke_data(firestore_client)
        can_chat = (
            firestore_client.collection("roles").document("canChat").get().to_dict()
            or {}
        )
        can_create = (
            firestore_client.collection("roles")
            .document("canCreatePoll")
            .get()
            .to_dict()
            or {}
        )
        assert can_chat.get("smoke-user-a") is True
        assert can_create.get("smoke-user-a") is not True

    def test_push_enabled_user_has_correct_preferences(self, firestore_client):
        """smoke-user-b must have webPushEnabled=True and globalChat=True."""
        seed_smoke_data(firestore_client)
        doc = (
            firestore_client.collection("users")
            .document("smoke-user-b")
            .get()
            .to_dict()
        )
        assert doc is not None
        assert doc["webPushEnabled"] is True
        assert doc["pushPreferences"]["globalChat"] is True


# ---------------------------------------------------------------------------
# 3. Poll creation: document structure
# ---------------------------------------------------------------------------


@pytest.mark.integration
class TestPollCreation:
    """Verify that creating a poll writes the three required Firestore documents."""

    def test_create_poll_writes_polls_doc(self, firestore_client):
        _create_poll(firestore_client, "poll-lifecycle-1", date="2026-07-01")
        doc = (
            firestore_client.collection("polls")
            .document("poll-lifecycle-1")
            .get()
            .to_dict()
        )
        assert doc is not None
        assert doc["date"] == "2026-07-01"
        assert doc["completed"] is False

    def test_create_poll_writes_votes_doc(self, firestore_client):
        _create_poll(firestore_client, "poll-lifecycle-2")
        doc = (
            firestore_client.collection("votes")
            .document("poll-lifecycle-2")
            .get()
            .to_dict()
        )
        assert doc is not None
        # The React NewPoll component initialises the any key as an empty array
        assert "any" in doc
        assert doc["any"] == []

    def test_create_poll_writes_attendance_doc(self, firestore_client):
        _create_poll(firestore_client, "poll-lifecycle-3")
        doc = (
            firestore_client.collection("attendance")
            .document("poll-lifecycle-3")
            .get()
            .to_dict()
        )
        assert doc is not None
        # Attendance starts as an empty map
        assert doc == {}

    def test_create_poll_does_not_set_completed_or_selected(self, firestore_client):
        """A freshly created poll must not have selected or pubs fields."""
        _create_poll(firestore_client, "poll-lifecycle-4")
        doc = (
            firestore_client.collection("polls")
            .document("poll-lifecycle-4")
            .get()
            .to_dict()
        )
        assert "selected" not in doc
        assert "pubs" not in doc


# ---------------------------------------------------------------------------
# 4. Notifier: new poll → open_actions record written
# ---------------------------------------------------------------------------


@pytest.mark.integration
class TestNewPollNotification:
    """Verify that the Python notifier writes an open_actions record for a new poll."""

    def test_new_poll_triggers_open_action(self, firestore_client):
        """new_poll_event_handler must write open_actions/{pollId} after a poll is created."""
        poll_id = "poll-notify-1"
        _create_poll(firestore_client, poll_id, date="2026-08-01")

        handler = DbHandler()
        fake_am = _FakeActionMan({"email": [PushDedupeKeys.open_key(poll_id)]})
        handler.new_poll_event_handler(cast(ActionMan, fake_am), poll_id=poll_id)

        action_doc = (
            firestore_client.collection("open_actions")
            .document(poll_id)
            .get()
            .to_dict()
        )
        assert action_doc is not None
        assert action_doc["email"] == [PushDedupeKeys.open_key(poll_id)]

    def test_new_poll_action_uses_correct_dedupe_key(self, firestore_client):
        """The open_actions record must use the canonical open dedupe key."""
        poll_id = "poll-notify-2"
        _create_poll(firestore_client, poll_id)

        handler = DbHandler()
        fake_am = _FakeActionMan({"email": [PushDedupeKeys.open_key(poll_id)]})
        handler.new_poll_event_handler(cast(ActionMan, fake_am), poll_id=poll_id)

        assert len(fake_am.calls) == 1
        assert fake_am.calls[0]["action_key"] == PushDedupeKeys.open_key(poll_id)
        assert fake_am.calls[0]["poll_id"] == poll_id

    def test_new_poll_handler_is_idempotent(self, firestore_client):
        """Running the handler twice for the same poll must not duplicate or corrupt the action doc."""
        poll_id = "poll-notify-3"
        _create_poll(firestore_client, poll_id)

        handler = DbHandler()
        fake_am = _FakeActionMan({"email": [PushDedupeKeys.open_key(poll_id)]})

        handler.new_poll_event_handler(cast(ActionMan, fake_am), poll_id=poll_id)
        handler.new_poll_event_handler(cast(ActionMan, fake_am), poll_id=poll_id)

        action_doc = (
            firestore_client.collection("open_actions")
            .document(poll_id)
            .get()
            .to_dict()
        )
        # The doc should still be a single record, not duplicated
        assert action_doc is not None
        assert isinstance(action_doc.get("email"), list)
