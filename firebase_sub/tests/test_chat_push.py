"""Tests for chat push notification handling."""

from types import SimpleNamespace
from unittest.mock import ANY, MagicMock, call, patch

import pytest
from pywebpush import WebPushException

from firebase_sub.action_track import CallbackExceptionRetry
from firebase_sub.database.handlers import DbHandler
from firebase_sub.push_contract import (
    PUSH_EVENT_CHAT_MESSAGE_EVENT,
    PUSH_EVENT_CHAT_MESSAGE_GLOBAL,
    PUSH_PREFERENCE_DEFAULTS,
    PUSH_PREFERENCE_FIELD,
)
from firebase_sub.send_push import (
    ValidPushEndpoint,
    _build_chat_event_payload,
    _build_chat_global_payload,
    _endpoint_hash,
    send_chat_push,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _user_doc(
    uid: str, web_push_enabled: bool = True, push_prefs: dict | None = None
) -> MagicMock:
    """Build a mock Firestore user document snapshot."""
    doc = MagicMock()
    doc.id = uid
    data: dict = {"webPushEnabled": web_push_enabled}
    if push_prefs is not None:
        data["pushPreferences"] = push_prefs
    doc.to_dict.return_value = data
    return doc


def _message_doc(
    message_id: str,
    uid: str = "author-1",
    scope_type: str | None = None,
    scope_id: str | None = None,
    text: str = "Hello world",
    display_name: str = "Alice",
) -> MagicMock:
    doc = MagicMock()
    doc.id = message_id
    data: dict = {
        "uid": uid,
        "displayName": display_name,
        "text": text,
    }
    if scope_type is not None:
        data["scopeType"] = scope_type
    if scope_id is not None:
        data["scopeId"] = scope_id
    doc.to_dict.return_value = data
    return doc


def _endpoint_doc(uid: str, endpoint: str = "https://push.example/a") -> MagicMock:
    parent = MagicMock()
    parent.parent = MagicMock(id=uid)
    ref = MagicMock()
    ref.parent = parent
    doc = MagicMock()
    doc.id = f"ep-{uid}"
    doc.reference = ref
    doc.to_dict.return_value = {
        "endpoint": endpoint,
        "p256dh": "p256dh-value",
        "auth": "auth-value",
        "active": True,
    }
    return doc


def _valid_endpoint(
    uid: str, endpoint: str = "https://push.example/a"
) -> ValidPushEndpoint:
    ref = MagicMock()
    ref.set = MagicMock()
    doc = MagicMock()
    doc.reference = ref
    return ValidPushEndpoint(
        endpoint=endpoint,
        p256dh="p256dh-value",
        auth="auth-value",
        user_id=uid,
        document=doc,
    )


def _make_db_handler(
    user_docs: list,
    attendance_data: dict | None = None,
    event_chat_participant_uids: list[str] | None = None,
    chat_actions_exists: bool = False,
    chat_actions_notified: list | None = None,
    chat_actions_delivered_endpoints: list | None = None,
    endpoint_docs: dict[str, list] | None = None,
) -> DbHandler:
    """Build a DbHandler with a mocked Firestore client."""
    db = MagicMock()

    # users collection
    db.collection.return_value.stream.return_value = user_docs

    # attendance document
    attendance_snap = MagicMock()
    attendance_snap.to_dict.return_value = attendance_data or {}
    db.collection.return_value.document.return_value.get.return_value = attendance_snap

    # chat_push_actions document
    actions_snap = MagicMock()
    actions_snap.exists = chat_actions_exists
    actions_snap.to_dict.return_value = (
        {
            "notified": chat_actions_notified or [],
            "delivered_endpoints": chat_actions_delivered_endpoints or [],
        }
        if chat_actions_exists
        else {}
    )
    actions_ref = MagicMock()
    actions_ref.get.return_value = actions_snap
    actions_ref.set = MagicMock()
    actions_ref.update = MagicMock()

    # Wire collection("chat_push_actions").document(id) → actions_ref
    # We use side_effect to route different collection names.
    def collection_router(name):
        col = MagicMock()
        if name == "chat_push_actions":
            col.document.return_value = actions_ref
        elif name == "attendance":
            attendance_col_doc = MagicMock()
            attendance_col_doc.get.return_value = attendance_snap
            col.document.return_value = attendance_col_doc
        elif name == "users":
            col.stream.return_value = user_docs

            # per-user endpoint sub-collection
            def user_doc_router(uid):
                user_doc_ref = MagicMock()
                user_doc_ref.get.return_value = next(
                    (u for u in user_docs if u.id == uid), MagicMock(to_dict=lambda: {})
                )
                ep_col = MagicMock()
                ep_col.stream.return_value = (endpoint_docs or {}).get(uid, [])
                user_doc_ref.collection.return_value = ep_col
                return user_doc_ref

            col.document.side_effect = user_doc_router
        elif name == "messages":
            first_query = MagicMock()
            second_query = MagicMock()

            participant_docs = []
            for uid in event_chat_participant_uids or []:
                participant_doc = MagicMock()
                participant_doc.to_dict.return_value = {
                    "uid": uid,
                    "scopeType": "event",
                }
                participant_docs.append(participant_doc)

            col.where.return_value = first_query
            first_query.where.return_value = second_query
            second_query.stream.return_value = participant_docs
        return col

    db.collection.side_effect = collection_router

    handler = DbHandler.__new__(DbHandler)
    handler.db = db
    handler.okay = True
    handler.poll_repo = MagicMock()
    handler.user_repo = MagicMock()
    return handler, actions_ref


# ---------------------------------------------------------------------------
# push_contract tests
# ---------------------------------------------------------------------------


def test_preference_field_mapping_covers_all_chat_types():
    assert PUSH_PREFERENCE_FIELD[PUSH_EVENT_CHAT_MESSAGE_GLOBAL] == "globalChat"
    assert PUSH_PREFERENCE_FIELD[PUSH_EVENT_CHAT_MESSAGE_EVENT] == "eventChat"


def test_preference_defaults_global_chat_is_off():
    assert PUSH_PREFERENCE_DEFAULTS["globalChat"] is False


def test_preference_defaults_event_chat_is_off():
    assert PUSH_PREFERENCE_DEFAULTS["eventChat"] is False


def test_preference_defaults_poll_opens_is_on():
    assert PUSH_PREFERENCE_DEFAULTS["pollOpens"] is True


def test_handle_chat_message_delegates_to_chat_message_push_handler():
    handler = DbHandler.__new__(DbHandler)
    handler.chat_message_push_handler = MagicMock()
    message_doc = _message_doc("msg-handle")

    handler.handle_chat_message(message_doc, MagicMock(), dummy_run=True)

    handler.chat_message_push_handler.assert_called_once_with(
        "msg-handle",
        message_doc,
        dummy_run=True,
    )


def test_handle_chat_message_ignores_none_document():
    handler = DbHandler.__new__(DbHandler)
    handler.chat_message_push_handler = MagicMock()

    handler.handle_chat_message(None, MagicMock(), dummy_run=True)

    handler.chat_message_push_handler.assert_not_called()


# ---------------------------------------------------------------------------
# send_push: payload builders
# ---------------------------------------------------------------------------


def test_build_chat_global_payload_shape():
    payload = _build_chat_global_payload("msg-1", "Alice", "Hello there")

    assert payload["eventType"] == PUSH_EVENT_CHAT_MESSAGE_GLOBAL
    assert payload["messageId"] == "msg-1"
    assert payload["pollId"] is None
    assert payload["title"] == "Alice in Global Chat"
    assert payload["body"] == "Hello there"
    assert payload["tag"] == "chat:main"
    assert "/chat" in payload["url"]


def test_build_chat_event_payload_shape():
    payload = _build_chat_event_payload("msg-2", "poll-99", "Bob", "See you there")

    assert payload["eventType"] == PUSH_EVENT_CHAT_MESSAGE_EVENT
    assert payload["messageId"] == "msg-2"
    assert payload["pollId"] == "poll-99"
    assert payload["title"] == "Bob in Event Chat"
    assert payload["body"] == "See you there"
    assert payload["tag"] == "chat:poll-99"
    assert payload["url"].endswith("/chat/event/poll-99")


def test_build_chat_global_payload_truncates_body():
    long_text = "x" * 200
    payload = _build_chat_global_payload("msg-3", "Alice", long_text)
    assert len(payload["body"]) <= 100


# ---------------------------------------------------------------------------
# send_push: send_chat_push
# ---------------------------------------------------------------------------


def test_send_chat_push_dummy_run_skips_webpush_call():
    endpoints = [_valid_endpoint("user-1"), _valid_endpoint("user-2")]
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    with patch("firebase_sub.send_push.webpush") as mock_wp:
        result = send_chat_push(
            endpoints=endpoints,
            payload=payload,
            actions_ref=MagicMock(),
            actions_doc_exists=False,
            scope_type="global",
            scope_id="main",
            dummy_run=True,
        )

    mock_wp.assert_not_called()
    assert set(result) == {"user-1", "user-2"}


def test_send_chat_push_delivers_to_all_endpoints_in_dummy_mode():
    endpoints = [_valid_endpoint("user-1"), _valid_endpoint("user-2")]
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    result = send_chat_push(
        endpoints=endpoints,
        payload=payload,
        actions_ref=MagicMock(),
        actions_doc_exists=False,
        scope_type="global",
        scope_id="main",
        dummy_run=True,
    )

    assert set(result) == {"user-1", "user-2"}


def test_send_chat_push_deduplicates_uid_when_user_has_multiple_endpoints():
    """A user with 3 active endpoints must appear only once in the returned uid list."""
    endpoints = [
        _valid_endpoint("user-1", "https://push.example/ep1"),
        _valid_endpoint("user-1", "https://push.example/ep2"),
        _valid_endpoint("user-1", "https://push.example/ep3"),
    ]
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    result = send_chat_push(
        endpoints=endpoints,
        payload=payload,
        actions_ref=MagicMock(),
        actions_doc_exists=False,
        scope_type="global",
        scope_id="main",
        dummy_run=True,
    )

    assert result == ["user-1"]


# ---------------------------------------------------------------------------
# send_push: send_chat_push — inline Firestore writes
# ---------------------------------------------------------------------------


def test_send_chat_push_creates_doc_on_first_delivery():
    """When actions doc does not yet exist, the first successful delivery calls set(merge=True)."""
    ep = _valid_endpoint("user-1")
    actions_ref = MagicMock()
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    result = send_chat_push(
        endpoints=[ep],
        payload=payload,
        actions_ref=actions_ref,
        actions_doc_exists=False,
        scope_type="global",
        scope_id="main",
        dummy_run=True,
    )

    assert result == ["user-1"]
    actions_ref.set.assert_called_once()
    set_data = actions_ref.set.call_args[0][0]
    assert set_data["scopeType"] == "global"
    assert set_data["scopeId"] == "main"
    assert "delivered_endpoints" in set_data
    assert "notified" in set_data
    assert actions_ref.set.call_args[1] == {"merge": True}


def test_send_chat_push_uses_update_when_doc_already_exists():
    """When actions doc already exists, all deliveries use update()."""
    ep = _valid_endpoint("user-1")
    actions_ref = MagicMock()
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    send_chat_push(
        endpoints=[ep],
        payload=payload,
        actions_ref=actions_ref,
        actions_doc_exists=True,
        scope_type="global",
        scope_id="main",
        dummy_run=True,
    )

    actions_ref.update.assert_called_once()
    actions_ref.set.assert_not_called()


def test_send_chat_push_writes_partial_delivery_before_retryable_failure():
    """ep1 succeeds and is written; ep2 fails retryably; CallbackExceptionRetry raised."""
    ep1 = _valid_endpoint("user-1", "https://push.example/ep1")
    ep2 = _valid_endpoint("user-2", "https://push.example/ep2")
    actions_ref = MagicMock()
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    call_count = 0

    def fake_webpush(**kwargs):
        nonlocal call_count
        call_count += 1
        if call_count > 1:
            exc = WebPushException("server error")
            exc.response = SimpleNamespace(status_code=500, text="Server Error")
            raise exc

    with (
        patch("firebase_sub.send_push.webpush", side_effect=fake_webpush),
        patch("firebase_sub.send_push._vapid_private_key", return_value="fake-key"),
        patch(
            "firebase_sub.send_push._vapid_claims",
            return_value={"sub": "mailto:t@e.com"},
        ),
    ):
        with pytest.raises(CallbackExceptionRetry):
            send_chat_push(
                endpoints=[ep1, ep2],
                payload=payload,
                actions_ref=actions_ref,
                actions_doc_exists=False,
                scope_type="global",
                scope_id="main",
                dummy_run=False,
            )

    # ep1's delivery was written before ep2 failed.
    actions_ref.set.assert_called_once()  # created doc for ep1
    actions_ref.update.assert_not_called()  # ep2 never reached its write


def test_send_chat_push_deactivates_stale_endpoint_and_continues():
    stale = _valid_endpoint("user-1", "https://push.example/stale")
    good = _valid_endpoint("user-2", "https://push.example/good")

    def fake_webpush(**kwargs):
        if "stale" in kwargs["subscription_info"]["endpoint"]:
            exc = WebPushException("gone")
            exc.response = SimpleNamespace(status_code=410, text="Gone")
            raise exc

    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    with patch("firebase_sub.send_push.webpush", side_effect=fake_webpush), patch(
        "firebase_sub.send_push._vapid_private_key", return_value="fake-key"
    ), patch(
        "firebase_sub.send_push._vapid_claims",
        return_value={"sub": "mailto:test@example.com"},
    ):
        result = send_chat_push(
            endpoints=[stale, good],
            payload=payload,
            actions_ref=MagicMock(),
            actions_doc_exists=False,
            scope_type="global",
            scope_id="main",
            dummy_run=False,
        )

    stale.document.reference.set.assert_called_once()
    assert "user-2" in result
    assert "user-1" not in result


def test_send_chat_push_raises_retry_on_retryable_failure():
    endpoint = _valid_endpoint("user-1")
    payload = _build_chat_global_payload("msg-1", "Alice", "hi")

    def fake_webpush(**kwargs):
        exc = WebPushException("server error")
        exc.response = SimpleNamespace(status_code=500, text="Server Error")
        raise exc

    with patch("firebase_sub.send_push.webpush", side_effect=fake_webpush), patch(
        "firebase_sub.send_push._vapid_private_key", return_value="fake-key"
    ), patch(
        "firebase_sub.send_push._vapid_claims",
        return_value={"sub": "mailto:test@example.com"},
    ):
        with pytest.raises(CallbackExceptionRetry):
            send_chat_push(
                endpoints=[endpoint],
                payload=payload,
                actions_ref=MagicMock(),
                actions_doc_exists=False,
                scope_type="global",
                scope_id="main",
                dummy_run=False,
            )


# ---------------------------------------------------------------------------
# DbHandler._users_with_push_preference
# ---------------------------------------------------------------------------


def test_users_with_push_preference_excludes_web_push_disabled():
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": True}),
        _user_doc("u2", web_push_enabled=False, push_prefs={"globalChat": True}),
    ]
    handler, _ = _make_db_handler(users)

    result = handler._users_with_push_preference("globalChat")

    assert "u1" in result
    assert "u2" not in result


def test_users_with_push_preference_applies_migration_default_global_chat_off():
    """When pushPreferences is absent, globalChat defaults to False."""
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs=None),
    ]
    handler, _ = _make_db_handler(users)

    result = handler._users_with_push_preference("globalChat")

    assert "u1" not in result


def test_users_with_push_preference_applies_migration_default_poll_opens_on():
    """When pushPreferences is absent, pollOpens defaults to True."""
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs=None),
    ]
    handler, _ = _make_db_handler(users)

    result = handler._users_with_push_preference("pollOpens")

    assert "u1" in result


def test_users_with_push_preference_explicit_false_excludes_user():
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": False}),
    ]
    handler, _ = _make_db_handler(users)

    result = handler._users_with_push_preference("globalChat")

    assert "u1" not in result


# ---------------------------------------------------------------------------
# DbHandler._attendee_uids
# ---------------------------------------------------------------------------


def test_attendee_uids_returns_can_come_across_venues():
    attendance = {
        "venue-A": {"canCome": ["u1", "u2"], "cannotCome": ["u3"]},
        "venue-B": {"canCome": ["u4"], "cannotCome": []},
    }
    handler, _ = _make_db_handler([], attendance_data=attendance)

    result = handler._attendee_uids("poll-1")

    assert result == {"u1", "u2", "u4"}


def test_attendee_uids_empty_when_no_document():
    handler, _ = _make_db_handler([], attendance_data={})

    result = handler._attendee_uids("poll-missing")

    assert result == set()


# ---------------------------------------------------------------------------
# DbHandler.chat_message_push_handler — global chat
# ---------------------------------------------------------------------------


def test_chat_handler_global_sends_to_eligible_users(monkeypatch):
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": True}),
        _user_doc("author", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    ep_u1 = _endpoint_doc("u1")
    handler, actions_ref = _make_db_handler(users, endpoint_docs={"u1": [ep_u1]})

    sent_payloads = []

    def fake_send_chat_push(endpoints, *, payload, **kwargs):
        sent_payloads.append(payload)
        return [ep.user_id for ep in endpoints]

    monkeypatch.setattr("firebase_sub.send_push.webpush", MagicMock())

    msg = _message_doc("msg-1", uid="author", scope_type="global")

    with patch(
        "firebase_sub.database.handlers.send_chat_push", side_effect=fake_send_chat_push
    ):
        handler.chat_message_push_handler("msg-1", msg)

    assert len(sent_payloads) == 1
    assert sent_payloads[0]["eventType"] == PUSH_EVENT_CHAT_MESSAGE_GLOBAL
    # Author must not receive the push
    assert sent_payloads[0]["tag"] == "chat:main"


def test_chat_handler_global_excludes_author():
    users = [
        _user_doc("author", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    handler, actions_ref = _make_db_handler(users)
    msg = _message_doc("msg-2", uid="author")

    with patch("firebase_sub.database.handlers.send_chat_push") as mock_send:
        handler.chat_message_push_handler("msg-2", msg)

    mock_send.assert_not_called()
    actions_ref.set.assert_not_called()


def test_chat_handler_global_missing_scope_type_treated_as_global():
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    ep_u1 = _endpoint_doc("u1")
    handler, _ = _make_db_handler(users, endpoint_docs={"u1": [ep_u1]})

    # No scopeType field in the message doc
    msg = _message_doc("msg-3", uid="author", scope_type=None)
    payloads_sent = []

    def fake_send(endpoints, *, payload, **kwargs):
        payloads_sent.append(payload)
        return [ep.user_id for ep in endpoints]

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        handler.chat_message_push_handler("msg-3", msg)

    assert payloads_sent[0]["eventType"] == PUSH_EVENT_CHAT_MESSAGE_GLOBAL


# ---------------------------------------------------------------------------
# DbHandler.chat_message_push_handler — event chat
# ---------------------------------------------------------------------------


def test_chat_handler_event_filters_to_attendees():
    users = [
        _user_doc("attendee", web_push_enabled=True, push_prefs={"eventChat": True}),
        _user_doc(
            "non-attendee", web_push_enabled=True, push_prefs={"eventChat": True}
        ),
    ]
    attendance = {"venue-A": {"canCome": ["attendee"], "cannotCome": ["non-attendee"]}}
    ep = _endpoint_doc("attendee")
    handler, _ = _make_db_handler(
        users, attendance_data=attendance, endpoint_docs={"attendee": [ep]}
    )

    msg = _message_doc("msg-4", uid="author", scope_type="event", scope_id="poll-5")
    recipient_uids = []

    def fake_send(endpoints, *, payload, **kwargs):
        recipient_uids.extend(ep.user_id for ep in endpoints)
        return recipient_uids[:]

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        handler.chat_message_push_handler("msg-4", msg)

    assert "attendee" in recipient_uids
    assert "non-attendee" not in recipient_uids


def test_chat_handler_event_payload_includes_poll_id():
    users = [
        _user_doc("attendee", web_push_enabled=True, push_prefs={"eventChat": True}),
    ]
    attendance = {"venue-A": {"canCome": ["attendee"]}}
    ep = _endpoint_doc("attendee")
    handler, _ = _make_db_handler(
        users, attendance_data=attendance, endpoint_docs={"attendee": [ep]}
    )

    msg = _message_doc("msg-5", uid="author", scope_type="event", scope_id="poll-42")
    payloads = []

    def fake_send(endpoints, *, payload, **kwargs):
        payloads.append(payload)
        return [ep.user_id for ep in endpoints]

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        handler.chat_message_push_handler("msg-5", msg)

    assert payloads[0]["pollId"] == "poll-42"
    assert payloads[0]["tag"] == "chat:poll-42"
    assert payloads[0]["eventType"] == PUSH_EVENT_CHAT_MESSAGE_EVENT


def test_chat_handler_event_includes_prior_chat_participant_not_attending():
    users = [
        _user_doc("attendee", web_push_enabled=True, push_prefs={"eventChat": True}),
        _user_doc("participant", web_push_enabled=True, push_prefs={"eventChat": True}),
    ]
    attendance = {"venue-A": {"canCome": ["attendee"]}}
    ep_attendee = _endpoint_doc("attendee")
    ep_participant = _endpoint_doc("participant")
    handler, _ = _make_db_handler(
        users,
        attendance_data=attendance,
        event_chat_participant_uids=["participant"],
        endpoint_docs={"attendee": [ep_attendee], "participant": [ep_participant]},
    )

    msg = _message_doc("msg-5b", uid="author", scope_type="event", scope_id="poll-42")
    recipient_uids = []

    def fake_send(endpoints, *, payload, **kwargs):
        recipient_uids.extend(ep.user_id for ep in endpoints)
        return list(dict.fromkeys(recipient_uids))

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        handler.chat_message_push_handler("msg-5b", msg)

    assert "attendee" in recipient_uids
    assert "participant" in recipient_uids


def test_chat_handler_event_excludes_user_muted_for_poll():
    users = [
        _user_doc("attendee", web_push_enabled=True, push_prefs={"eventChat": True}),
        _user_doc(
            "participant",
            web_push_enabled=True,
            push_prefs={"eventChat": True, "eventChatMutedPollIds": ["poll-42"]},
        ),
    ]
    attendance = {"venue-A": {"canCome": ["attendee"]}}
    ep_attendee = _endpoint_doc("attendee")
    ep_participant = _endpoint_doc("participant")
    handler, _ = _make_db_handler(
        users,
        attendance_data=attendance,
        event_chat_participant_uids=["participant"],
        endpoint_docs={"attendee": [ep_attendee], "participant": [ep_participant]},
    )

    msg = _message_doc("msg-5c", uid="author", scope_type="event", scope_id="poll-42")
    recipient_uids = []

    def fake_send(endpoints, *, payload, **kwargs):
        recipient_uids.extend(ep.user_id for ep in endpoints)
        return list(dict.fromkeys(recipient_uids))

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        handler.chat_message_push_handler("msg-5c", msg)

    assert "attendee" in recipient_uids
    assert "participant" not in recipient_uids


# ---------------------------------------------------------------------------
# DbHandler.chat_message_push_handler — idempotency
# ---------------------------------------------------------------------------


def test_chat_handler_skips_already_delivered_endpoint():
    """An endpoint whose hash is already in delivered_endpoints must not be re-sent."""
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": True}),
        _user_doc("u2", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    ep_u1 = _endpoint_doc("u1")  # default URL: https://push.example/a
    ep_u2 = _endpoint_doc("u2")
    u1_hash = _endpoint_hash(_valid_endpoint("u1"))
    handler, _ = _make_db_handler(
        users,
        chat_actions_exists=True,
        chat_actions_delivered_endpoints=[u1_hash],
        endpoint_docs={"u1": [ep_u1], "u2": [ep_u2]},
    )
    msg = _message_doc("msg-6", uid="author")
    recipients = []

    def fake_send(endpoints, *, payload, **kwargs):
        recipients.extend(ep.user_id for ep in endpoints)
        return list(dict.fromkeys(recipients))

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        handler.chat_message_push_handler("msg-6", msg)

    assert "u1" not in recipients
    assert "u2" in recipients


def test_chat_handler_passes_actions_doc_exists_true_on_retry():
    """When the actions doc already exists, actions_doc_exists=True is passed to send_chat_push."""
    users = [
        _user_doc("u2", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    ep_u2 = _endpoint_doc("u2")
    handler, actions_ref = _make_db_handler(
        users,
        chat_actions_exists=True,
        endpoint_docs={"u2": [ep_u2]},
    )
    msg = _message_doc("msg-7", uid="author")

    with patch("firebase_sub.database.handlers.send_chat_push") as mock_send:
        mock_send.return_value = ["u2"]
        handler.chat_message_push_handler("msg-7", msg)

    assert mock_send.call_args.kwargs["actions_doc_exists"] is True
    assert mock_send.call_args.kwargs["actions_ref"] is actions_ref


def test_chat_handler_passes_actions_doc_exists_false_on_first_send():
    """When actions doc does not exist, actions_doc_exists=False and scope args are passed."""
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    ep_u1 = _endpoint_doc("u1")
    handler, actions_ref = _make_db_handler(
        users,
        chat_actions_exists=False,
        endpoint_docs={"u1": [ep_u1]},
    )
    msg = _message_doc("msg-8", uid="author")

    with patch("firebase_sub.database.handlers.send_chat_push") as mock_send:
        mock_send.return_value = ["u1"]
        handler.chat_message_push_handler("msg-8", msg)

    call_kwargs = mock_send.call_args.kwargs
    assert call_kwargs["actions_doc_exists"] is False
    assert call_kwargs["scope_type"] == "global"
    assert call_kwargs["scope_id"] == "main"
    assert call_kwargs["actions_ref"] is actions_ref


def test_chat_handler_does_not_write_actions_on_retryable_failure():
    users = [
        _user_doc("u1", web_push_enabled=True, push_prefs={"globalChat": True}),
    ]
    ep_u1 = _endpoint_doc("u1")
    handler, actions_ref = _make_db_handler(
        users,
        chat_actions_exists=False,
        endpoint_docs={"u1": [ep_u1]},
    )
    msg = _message_doc("msg-9", uid="author")

    def fake_send(endpoints, *, payload, **kwargs):
        raise CallbackExceptionRetry("retryable")

    with patch("firebase_sub.database.handlers.send_chat_push", side_effect=fake_send):
        with pytest.raises(CallbackExceptionRetry):
            handler.chat_message_push_handler("msg-9", msg)

    actions_ref.set.assert_not_called()
    actions_ref.update.assert_not_called()
