from types import SimpleNamespace
from typing import cast
from unittest.mock import patch

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.chat_message import ChatMessageListenerPlugin


class _FakeDbHandler:
    pass


def test_chat_message_listener_enqueues_chat_event() -> None:
    """Test that the plugin filters and handles chat message events."""
    db_handler = _FakeDbHandler()
    plugin = ChatMessageListenerPlugin(
        db_handler=cast(object, db_handler),
        dummy_run=True,
    )

    # Create a fake document and event envelope
    document = cast(DocumentSnapshot, SimpleNamespace(id="msg-1"))
    envelope = EventEnvelope(type=EventType.CHAT_MESSAGE, doc=document)

    # Test filter accepts chat message events
    assert plugin.filter(envelope) is True

    with patch(
        "firebase_sub.plugins.chat_message.process_chat_message_push"
    ) as mock_push:
        plugin.handle(envelope)

    mock_push.assert_called_once_with(db_handler, "msg-1", document, dummy_run=True)


def test_chat_message_listener_is_enabled() -> None:
    plugin = ChatMessageListenerPlugin(
        db_handler=cast(object, _FakeDbHandler()),
        dummy_run=False,
    )

    assert plugin.is_enabled() is True


def test_chat_message_filter_and_handle() -> None:
    db_handler = _FakeDbHandler()
    plugin = ChatMessageListenerPlugin(
        db_handler=cast(object, db_handler),
        dummy_run=True,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="msg-2"))
    envelope = EventEnvelope(type=EventType.CHAT_MESSAGE, doc=document)

    assert plugin.filter(envelope) is True
    with patch(
        "firebase_sub.plugins.chat_message.process_chat_message_push"
    ) as mock_push:
        plugin.handle(envelope)
    mock_push.assert_called_once_with(db_handler, "msg-2", document, dummy_run=True)
    plugin.mark_done(envelope)


def test_chat_message_filter_rejects_other_event_types() -> None:
    plugin = ChatMessageListenerPlugin(
        db_handler=cast(object, _FakeDbHandler()),
        dummy_run=False,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="msg-3"))

    assert plugin.filter(EventEnvelope(type=EventType.PUSH, doc=document)) is False
