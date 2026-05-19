from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope
from firebase_sub.event import EventType
from firebase_sub.plugins.admin_delete_request import AdminDeleteRequestListenerPlugin


class _FakeAdminDeleteHandler:
    def __init__(self, *, enabled: bool) -> None:
        self.enabled = enabled
        self.handled: list[str] = []

    def handle_request_document(
        self, request_document: DocumentSnapshot | None
    ) -> None:
        if request_document is None:
            return
        self.handled.append(request_document.id)


def test_admin_delete_listener_enqueues_admin_delete_event() -> None:
    """Test that the plugin filters and handles admin delete events."""
    handler = _FakeAdminDeleteHandler(enabled=True)
    plugin = AdminDeleteRequestListenerPlugin(
        handler=handler,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-1"))
    envelope = EventEnvelope(type=EventType.ADMIN_DELETE_REQUEST, doc=document)

    # Test filter accepts admin delete events
    assert plugin.filter(envelope) is True

    # Test handle processes the event
    plugin.handle(envelope)

    # Verify the handler was called
    assert handler.handled == ["req-1"]


def test_admin_delete_listener_is_disabled_when_handler_disabled() -> None:
    plugin = AdminDeleteRequestListenerPlugin(
        handler=_FakeAdminDeleteHandler(enabled=False),
    )

    assert plugin.is_enabled() is False


def test_admin_delete_listener_lifecycle_filter_and_handle() -> None:
    handler = _FakeAdminDeleteHandler(enabled=True)
    plugin = AdminDeleteRequestListenerPlugin(
        handler=handler,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-2"))
    envelope = EventEnvelope(type=EventType.ADMIN_DELETE_REQUEST, doc=document)

    assert plugin.filter(envelope) is True
    plugin.handle(envelope)
    plugin.mark_done(envelope)

    assert handler.handled == ["req-2"]


def test_admin_delete_listener_filter_rejects_when_disabled() -> None:
    plugin = AdminDeleteRequestListenerPlugin(
        handler=_FakeAdminDeleteHandler(enabled=False),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-3"))

    assert (
        plugin.filter(EventEnvelope(type=EventType.ADMIN_DELETE_REQUEST, doc=document))
        is False
    )
