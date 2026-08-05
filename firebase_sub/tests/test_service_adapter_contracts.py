from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.runtime.event_registry import EventRegistry, EventWritebackError
from firebase_sub.runtime.service_adapter import (
    AdapterBackedEventPlugin,
    ServiceContext,
    ServiceResult,
)


class _FakeAdapter:
    def __init__(self, *, skip_ids: set[str] | None = None) -> None:
        self._skip_ids = skip_ids or set()
        self.prepared: list[str] = []
        self.executed: list[str] = []
        self.committed: list[tuple[str, bool]] = []

    def name(self) -> str:
        return "fake_adapter"

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        doc_id = envelope.document_id()
        if doc_id is None or doc_id in self._skip_ids:
            return None
        self.prepared.append(doc_id)
        return ServiceContext(
            envelope=envelope,
            entity_id=doc_id,
            dedupe_key=f"dedupe:{doc_id}",
        )

    def execute(self, context: ServiceContext) -> ServiceResult:
        self.executed.append(context.entity_id)
        return ServiceResult(success=True)

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        self.committed.append((context.entity_id, result.success))


class _ExplodingExecuteAdapter(_FakeAdapter):
    def execute(self, context: ServiceContext) -> ServiceResult:
        del context
        raise RuntimeError("execute failed")


class _ExplodingCommitAdapter(_FakeAdapter):
    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        del context, result
        raise RuntimeError("commit failed")


def test_adapter_backed_plugin_dispatch_success_path() -> None:
    adapter = _FakeAdapter()
    plugin = AdapterBackedEventPlugin(adapter)

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)

    document = cast(DocumentSnapshot, SimpleNamespace(id="req-1"))
    executed = registry.dispatch(EventEnvelope(type=EventType.PUSH, doc=document))

    assert executed == 1
    assert adapter.prepared == ["req-1"]
    assert adapter.executed == ["req-1"]
    assert adapter.committed == [("req-1", True)]


def test_adapter_backed_plugin_dispatch_skip_when_prepare_returns_none() -> None:
    adapter = _FakeAdapter(skip_ids={"skip-me"})
    plugin = AdapterBackedEventPlugin(adapter)

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)

    document = cast(DocumentSnapshot, SimpleNamespace(id="skip-me"))
    executed = registry.dispatch(EventEnvelope(type=EventType.PUSH, doc=document))

    assert executed == 0
    assert adapter.prepared == []
    assert adapter.executed == []
    assert adapter.committed == []


def test_adapter_backed_plugin_propagates_execute_failure() -> None:
    adapter = _ExplodingExecuteAdapter()
    plugin = AdapterBackedEventPlugin(adapter)

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)

    document = cast(DocumentSnapshot, SimpleNamespace(id="req-err"))

    try:
        registry.dispatch(EventEnvelope(type=EventType.PUSH, doc=document))
        raise AssertionError("Expected execute failure")
    except RuntimeError as exc:
        assert str(exc) == "execute failed"

    assert adapter.prepared == ["req-err"]
    assert adapter.executed == []
    assert adapter.committed == []
    assert plugin._prepared == {}  # noqa: SLF001
    assert plugin._results == {}  # noqa: SLF001


def test_adapter_backed_plugin_cleans_state_when_commit_fails() -> None:
    adapter = _ExplodingCommitAdapter()
    plugin = AdapterBackedEventPlugin(adapter)

    registry = EventRegistry()
    registry.subscribe(EventType.PUSH, plugin)

    document = cast(DocumentSnapshot, SimpleNamespace(id="req-commit-err"))

    try:
        registry.dispatch(EventEnvelope(type=EventType.PUSH, doc=document))
        raise AssertionError("Expected commit failure")
    except EventWritebackError as exc:
        assert isinstance(exc.__cause__, RuntimeError)
        assert str(exc.__cause__) == "commit failed"

    assert adapter.prepared == ["req-commit-err"]
    assert adapter.executed == ["req-commit-err"]
    assert adapter.committed == []
    assert plugin._prepared == {}  # noqa: SLF001
    assert plugin._results == {}  # noqa: SLF001
