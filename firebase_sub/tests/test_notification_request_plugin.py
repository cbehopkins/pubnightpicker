from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.notification_request import NotificationRequestListenerPlugin


class _FakeNotificationMirrorHandler:
    def __init__(self) -> None:
        self.handled: list[str] = []

    def mirror_request_document(self, request_document) -> None:
        self.handled.append(request_document.id)


class _FakeNotificationPushTestHandler:
    def __init__(self, *, push_test_ids: set[str]) -> None:
        self._push_test_ids = push_test_ids
        self.handled: list[str] = []

    def is_push_test_request(self, request_document) -> bool:
        return request_document.id in self._push_test_ids

    def handle_request_document(self, request_document) -> None:
        self.handled.append(request_document.id)


def test_notification_request_listener_enqueues_push_test_event() -> None:
    """Test that the plugin filters and handles push test events."""
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=_FakeNotificationMirrorHandler(),
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="push_test"))
    envelope = EventEnvelope(type=EventType.PUSH_TEST, doc=document)

    # Test filter accepts push test events
    assert plugin.filter(envelope) is True

    # Test handle processes the event
    test_handler = plugin._notification_push_test
    assert test_handler.is_push_test_request(document) is True


def test_notification_request_listener_enqueues_mirror_event_for_regular_docs() -> None:
    """Test that the plugin filters and handles regular push events."""
    mirror_handler = _FakeNotificationMirrorHandler()
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=mirror_handler,
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-1"))
    envelope = EventEnvelope(type=EventType.PUSH, doc=document)

    # Test filter accepts regular push events
    assert plugin.filter(envelope) is True

    # Test handle processes the event
    plugin.handle(envelope)

    # Verify the handler was called
    assert mirror_handler.handled == ["req-1"]


def test_notification_request_filter_and_handle_for_push_test() -> None:
    mirror_handler = _FakeNotificationMirrorHandler()
    push_test_handler = _FakeNotificationPushTestHandler(push_test_ids={"push_test"})
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=mirror_handler,
        notification_push_test=push_test_handler,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="push_test"))
    envelope = EventEnvelope(type=EventType.PUSH_TEST, doc=document)

    assert plugin.filter(envelope) is True
    plugin.handle(envelope)
    plugin.mark_done(envelope)

    assert push_test_handler.handled == ["push_test"]
    assert mirror_handler.handled == []


def test_notification_request_filter_and_handle_for_mirror_push() -> None:
    mirror_handler = _FakeNotificationMirrorHandler()
    push_test_handler = _FakeNotificationPushTestHandler(push_test_ids={"push_test"})
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=mirror_handler,
        notification_push_test=push_test_handler,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-2"))
    envelope = EventEnvelope(type=EventType.PUSH, doc=document)

    assert plugin.filter(envelope) is True
    plugin.handle(envelope)
    plugin.mark_done(envelope)

    assert mirror_handler.handled == ["req-2"]
    assert push_test_handler.handled == []
