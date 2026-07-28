from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.admin_delete_request import AdminDeleteRequestListenerPlugin
from firebase_sub.runtime.event_registry import EventRegistry, EventWritebackError


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
    assert plugin.mark_done(envelope) is None

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


def test_admin_delete_dispatch_distinguishes_handler_failure() -> None:
    plugin = AdminDeleteRequestListenerPlugin(
        handler=_FakeAdminDeleteHandler(enabled=True),
    )
    plugin.filter = lambda envelope: True  # type: ignore[method-assign]
    plugin.handle = lambda envelope: (_ for _ in ()).throw(
        RuntimeError("handler failed")
    )  # type: ignore[method-assign]

    registry = EventRegistry()
    registry.subscribe(EventType.ADMIN_DELETE_REQUEST, plugin)
    envelope = EventEnvelope(
        type=EventType.ADMIN_DELETE_REQUEST,
        doc=cast(DocumentSnapshot, SimpleNamespace(id="req-fail")),
    )

    try:
        registry.dispatch(envelope)
        raise AssertionError("Expected handler failure")
    except EventWritebackError as exc:
        raise AssertionError(f"Did not expect writeback error: {exc}")
    except RuntimeError as exc:
        assert str(exc) == "handler failed"


def test_admin_delete_dispatch_wraps_writeback_failure() -> None:
    plugin = AdminDeleteRequestListenerPlugin(
        handler=_FakeAdminDeleteHandler(enabled=True),
    )
    plugin.filter = lambda envelope: True  # type: ignore[method-assign]
    plugin.handle = lambda envelope: None  # type: ignore[method-assign]
    plugin.mark_done = lambda envelope: (_ for _ in ()).throw(
        RuntimeError("writeback failed")
    )  # type: ignore[method-assign]

    registry = EventRegistry()
    registry.subscribe(EventType.ADMIN_DELETE_REQUEST, plugin)
    envelope = EventEnvelope(
        type=EventType.ADMIN_DELETE_REQUEST,
        doc=cast(DocumentSnapshot, SimpleNamespace(id="req-writeback")),
    )

    try:
        registry.dispatch(envelope)
        raise AssertionError("Expected EventWritebackError")
    except EventWritebackError as exc:
        assert isinstance(exc.__cause__, RuntimeError)
        assert str(exc.__cause__) == "writeback failed"
