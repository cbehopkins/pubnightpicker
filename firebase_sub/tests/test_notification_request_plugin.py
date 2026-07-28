from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.my_types import RetryableServiceError
from firebase_sub.plugins.notification_request import NotificationRequestListenerPlugin
from firebase_sub.runtime.event_registry import EventRegistry, EventWritebackError


class _InMemoryDocRef:
    def __init__(self, store: dict[str, dict[str, object]], key: str) -> None:
        self._store = store
        self._key = key

    def get(self):
        return SimpleNamespace(
            to_dict=lambda: dict(self._store.get(self._key, {})),
        )

    def set(self, payload: dict[str, object], merge: bool = False) -> None:
        existing = self._store.get(self._key, {}) if merge else {}
        merged = _deep_merge(dict(existing), payload)
        self._store[self._key] = merged


class _InMemoryCollection:
    def __init__(self, root_store: dict[str, dict[str, dict[str, object]]], name: str):
        self._root_store = root_store
        self._name = name

    def document(self, doc_id: str) -> _InMemoryDocRef:
        collection_store = self._root_store.setdefault(self._name, {})
        return _InMemoryDocRef(collection_store, doc_id)


class _InMemoryDb:
    def __init__(self) -> None:
        self._store: dict[str, dict[str, dict[str, object]]] = {}

    def collection(self, name: str) -> _InMemoryCollection:
        return _InMemoryCollection(self._store, name)


def _deep_merge(base: dict[str, object], patch: dict[str, object]) -> dict[str, object]:
    for key, value in patch.items():
        if key in base and isinstance(base[key], dict) and isinstance(value, dict):
            base[key] = _deep_merge(
                cast(dict[str, object], base[key]), cast(dict[str, object], value)
            )
            continue
        base[key] = value
    return base


class _FakeNotificationMirrorHandler:
    def __init__(self) -> None:
        self.handled: list[str] = []
        self.db = _InMemoryDb()
        self.ack_collection_name = "notification_ack"

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
    assert plugin.mark_done(envelope) is None

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
    assert plugin.mark_done(envelope) is None

    assert mirror_handler.handled == ["req-2"]
    assert push_test_handler.handled == []


def test_notification_request_dispatch_distinguishes_handler_failure() -> None:
    class _ExplodingNotificationMirrorHandler(_FakeNotificationMirrorHandler):
        def mirror_request_document(self, request_document) -> None:
            del request_document
            raise RuntimeError("handler failed")

    plugin = NotificationRequestListenerPlugin(
        notification_mirror=_ExplodingNotificationMirrorHandler(),
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)
    envelope = EventEnvelope(
        type=EventType.PUSH,
        doc=cast(DocumentSnapshot, SimpleNamespace(id="req-fail")),
    )

    try:
        registry.dispatch(envelope)
        raise AssertionError("Expected retryable handler failure")
    except EventWritebackError as exc:
        raise AssertionError(f"Did not expect writeback error: {exc}")
    except RetryableServiceError as exc:
        assert isinstance(exc.__cause__, RuntimeError)
        assert str(exc.__cause__) == "handler failed"


def test_notification_request_dispatch_wraps_writeback_failure() -> None:
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=_FakeNotificationMirrorHandler(),
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )
    plugin.filter = lambda envelope: True  # type: ignore[method-assign]
    plugin.handle = lambda envelope: None  # type: ignore[method-assign]
    plugin.mark_done = lambda envelope: (_ for _ in ()).throw(
        RuntimeError("writeback failed")
    )  # type: ignore[method-assign]

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)
    envelope = EventEnvelope(
        type=EventType.PUSH,
        doc=cast(DocumentSnapshot, SimpleNamespace(id="req-writeback")),
    )

    try:
        registry.dispatch(envelope)
        raise AssertionError("Expected EventWritebackError")
    except EventWritebackError as exc:
        assert isinstance(exc.__cause__, RuntimeError)
        assert str(exc.__cause__) == "writeback failed"


def test_notification_request_filter_rejects_unhandled_event_type() -> None:
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=_FakeNotificationMirrorHandler(),
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="tick-doc"))
    envelope = EventEnvelope(type=EventType.TICK, doc=document)

    assert plugin.filter(envelope) is False


def test_notification_request_commit_writes_idempotency_done_metadata() -> None:
    mirror_handler = _FakeNotificationMirrorHandler()
    push_test_handler = _FakeNotificationPushTestHandler(push_test_ids={"push_test"})
    plugin = NotificationRequestListenerPlugin(
        notification_mirror=mirror_handler,
        notification_push_test=push_test_handler,
    )

    document = cast(
        DocumentSnapshot,
        SimpleNamespace(id="req-3", to_dict=lambda: {"manual": 123}),
    )
    envelope = EventEnvelope(type=EventType.PUSH, doc=document)

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)
    executed = registry.dispatch(envelope)

    assert executed == 1
    ack_snapshot = (
        mirror_handler.db.collection("notification_ack").document("req-3").get()
    )
    ack_payload = ack_snapshot.to_dict()
    idempotency_root = cast(dict[str, object], ack_payload["_service_idempotency"])
    assert idempotency_root
    first_entry = next(iter(idempotency_root.values()))
    first_entry_map = cast(dict[str, object], first_entry)
    assert first_entry_map["state"] == "done"
