from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.chat_message import ChatMessageListenerPlugin


class _FakeDbHandler:
    def __init__(self) -> None:
        self.handled: list[str] = []

    def chat_message_push_handler(
        self,
        message_id: str,
        message_doc,
        *,
        dummy_run: bool = False,
    ) -> None:
        del message_doc, dummy_run
        self.handled.append(message_id)

    def handle_chat_message(
        self, message_doc, pubs_list, *, dummy_run: bool = False
    ) -> None:
        del message_doc, pubs_list, dummy_run


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

    # Test handle processes the event
    plugin.handle(envelope)

    # Verify the handler was called
    assert "msg-1" in db_handler.handled


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
    plugin.handle(envelope)
    plugin.mark_done(envelope)

    assert db_handler.handled == ["msg-2"]


def test_chat_message_filter_rejects_other_event_types() -> None:
    plugin = ChatMessageListenerPlugin(
        db_handler=cast(object, _FakeDbHandler()),
        dummy_run=False,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="msg-3"))

    assert plugin.filter(EventEnvelope(type=EventType.PUSH, doc=document)) is False
