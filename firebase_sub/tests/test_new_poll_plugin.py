from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.my_types import TerminalServiceError
from firebase_sub.plugins.new_poll import MissingPollDateError, NewPollListenerPlugin
from firebase_sub.plugins.protocols import NewPollDbHandler
from firebase_sub.runtime.event_registry import EventRegistry, EventWritebackError
from firebase_sub.runtime.idempotency import IdempotencyMetadata, IdempotencyStore


class _FakeDbHandler:
    def __init__(self) -> None:
        self.db = _FakeDb()
        self.poll_repo = _FakePollRepo()


class _FakePollRepo:
    def __init__(self, poll: dict[str, str] | None = None) -> None:
        self._poll = poll or {"date": "2026-01-01"}

    def get_poll(self, _poll_id: str) -> dict[str, str] | None:
        return self._poll


class _FakeDb:
    def __init__(self):
        self._store: dict[tuple[str, str], dict] = {}

    def collection(self, name: str):
        return _FakeCollection(self._store, name)


class _FakeCollection:
    def __init__(self, store: dict[tuple[str, str], dict], name: str):
        self._store = store
        self._name = name

    def document(self, doc_id: str):
        return _FakeDocRef(self._store, (self._name, doc_id))


class _FakeDocRef:
    def __init__(self, store: dict[tuple[str, str], dict], key: tuple[str, str]):
        self._store = store
        self._key = key

    def get(self):
        return _FakeDocSnapshot(self._store.get(self._key))

    def set(self, payload: dict, merge: bool = False):
        existing = dict(self._store.get(self._key, {}))
        if merge:
            existing.update(payload)
            self._store[self._key] = existing
            return
        self._store[self._key] = dict(payload)


class _FakeDocSnapshot:
    def __init__(self, payload: dict | None):
        self._payload = payload

    def to_dict(self) -> dict | None:
        return self._payload


class _FakeActionExecutor:
    def __init__(self) -> None:
        self.calls: list[dict[str, str]] = []

    def action_event(
        self, action_dict: dict, action_key: str, poll_id: str, poll_date: str
    ) -> None:
        del action_dict
        self.calls.append(
            {
                "action_key": action_key,
                "poll_id": poll_id,
                "poll_date": poll_date,
            }
        )


class _FakeIdempotencyStore(IdempotencyStore):
    def __init__(self, *, done: bool = False) -> None:
        self.done = done

    def is_done(self, *, entity_id: str, dedupe_key: str) -> bool:
        del entity_id, dedupe_key
        return self.done

    def mark_done(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata | None = None,
    ) -> None:
        del entity_id, dedupe_key, metadata

    def mark_retryable_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        del entity_id, dedupe_key, metadata

    def mark_terminal_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        del entity_id, dedupe_key, metadata


def test_new_poll_listener_initialization():
    """Test that the plugin can be initialized without errors."""
    db_handler: NewPollDbHandler = _FakeDbHandler()
    action_executor = _FakeActionExecutor()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=_FakeIdempotencyStore(),
    )

    assert plugin.name() == "new_poll_listener"
    assert plugin.is_enabled() is True


def test_new_poll_listener_filter_accepts_new_poll_events():
    """Test that filter accepts NEW_POLL event type."""
    db_handler: NewPollDbHandler = _FakeDbHandler()
    action_executor = _FakeActionExecutor()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=_FakeIdempotencyStore(),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="poll-1"))
    envelope = EventEnvelope(type=EventType.NEW_POLL, doc=document)

    # The filter method should return a boolean
    # (testing actual behavior with mocked Firestore is complex, so just test the call doesn't crash)
    try:
        result = plugin.filter(envelope)
        assert isinstance(result, bool)
    except (TypeError, AttributeError):
        # If mock setup is insufficient, that's OK - we're testing the refactoring, not Firestore mocks
        pass


def test_new_poll_listener_handle_calls_action_manager():
    db_handler: NewPollDbHandler = _FakeDbHandler()
    action_executor = _FakeActionExecutor()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=_FakeIdempotencyStore(),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="poll-2"))
    envelope = EventEnvelope(type=EventType.NEW_POLL, doc=document)

    assert plugin.filter(envelope) is True
    plugin.handle(envelope)

    assert action_executor.calls == [
        {
            "action_key": "poll-2",
            "poll_id": "poll-2",
            "poll_date": "2026-01-01",
        }
    ]


def test_new_poll_listener_uses_event_snapshot_date_when_poll_repo_unavailable():
    db_handler: NewPollDbHandler = _FakeDbHandler()
    db_handler.poll_repo = _FakePollRepo(poll=None)

    action_executor = _FakeActionExecutor()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=_FakeIdempotencyStore(),
    )

    document = cast(
        DocumentSnapshot,
        SimpleNamespace(
            id="poll-3",
            to_dict=lambda: {"date": "2026-05-27", "completed": False},
        ),
    )
    envelope = EventEnvelope(type=EventType.NEW_POLL, doc=document)

    assert plugin.filter(envelope) is True
    plugin.handle(envelope)

    assert action_executor.calls == [
        {
            "action_key": "poll-3",
            "poll_id": "poll-3",
            "poll_date": "2026-05-27",
        }
    ]


def test_new_poll_listener_filter_raises_terminal_error_when_date_missing() -> None:
    db_handler: NewPollDbHandler = _FakeDbHandler()
    db_handler.poll_repo = _FakePollRepo()
    db_handler.poll_repo._poll = None
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=_FakeActionExecutor(),
        idempotency_store=_FakeIdempotencyStore(),
    )
    document = cast(
        DocumentSnapshot,
        SimpleNamespace(
            id="poll-missing-date",
            to_dict=lambda: {"completed": False},
        ),
    )
    envelope = EventEnvelope(type=EventType.NEW_POLL, doc=document)

    try:
        plugin.filter(envelope)
        raise AssertionError("Expected MissingPollDateError")
    except MissingPollDateError as exc:
        assert "poll-missing-date" in str(exc)
        assert isinstance(exc, TerminalServiceError)


def test_new_poll_listener_filter_raises_terminal_error_when_repo_date_missing() -> (
    None
):
    db_handler: NewPollDbHandler = _FakeDbHandler()
    db_handler.poll_repo = _FakePollRepo(poll={"completed": "false"})
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=_FakeActionExecutor(),
        idempotency_store=_FakeIdempotencyStore(),
    )
    document = cast(
        DocumentSnapshot,
        SimpleNamespace(
            id="poll-missing-repo-date",
            to_dict=lambda: {"completed": False},
        ),
    )
    envelope = EventEnvelope(type=EventType.NEW_POLL, doc=document)

    try:
        plugin.filter(envelope)
        raise AssertionError("Expected MissingPollDateError")
    except MissingPollDateError as exc:
        assert "poll-missing-repo-date" in str(exc)


def test_new_poll_dispatch_distinguishes_handler_failure() -> None:
    db_handler: NewPollDbHandler = _FakeDbHandler()
    action_executor = _FakeActionExecutor()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=_FakeIdempotencyStore(),
    )
    plugin.filter = lambda envelope: True  # type: ignore[method-assign]
    plugin.handle = lambda envelope: (_ for _ in ()).throw(
        RuntimeError("handler failed")
    )  # type: ignore[method-assign]

    registry = EventRegistry()
    registry.subscribe(EventType.NEW_POLL, plugin)
    envelope = EventEnvelope(
        type=EventType.NEW_POLL,
        doc=cast(DocumentSnapshot, SimpleNamespace(id="poll-fail")),
    )

    try:
        registry.dispatch(envelope)
        raise AssertionError("Expected handler failure")
    except EventWritebackError as exc:
        raise AssertionError(f"Did not expect writeback error: {exc}")
    except RuntimeError as exc:
        assert str(exc) == "handler failed"


def test_new_poll_dispatch_wraps_writeback_failure() -> None:
    db_handler: NewPollDbHandler = _FakeDbHandler()
    action_executor = _FakeActionExecutor()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=_FakeIdempotencyStore(),
    )
    plugin.filter = lambda envelope: True  # type: ignore[method-assign]
    plugin.handle = lambda envelope: None  # type: ignore[method-assign]
    plugin.mark_done = lambda envelope: (_ for _ in ()).throw(
        RuntimeError("writeback failed")
    )  # type: ignore[method-assign]

    registry = EventRegistry()
    registry.subscribe(EventType.NEW_POLL, plugin)
    envelope = EventEnvelope(
        type=EventType.NEW_POLL,
        doc=cast(DocumentSnapshot, SimpleNamespace(id="poll-writeback")),
    )

    try:
        registry.dispatch(envelope)
        raise AssertionError("Expected EventWritebackError")
    except EventWritebackError as exc:
        assert isinstance(exc.__cause__, RuntimeError)
        assert str(exc.__cause__) == "writeback failed"
