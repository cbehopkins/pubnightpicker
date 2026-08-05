"""Service adapter contracts and an EventPlugin bridge.

This module defines prepare/execute/commit contracts so listener services can
migrate incrementally without breaking the existing EventPlugin dispatcher.
"""

from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Protocol

from firebase_sub.event import EventEnvelope
from firebase_sub.plugins.protocols import EventPlugin
from firebase_sub.runtime.idempotency import IdempotencyStore


def _new_extras_dict() -> dict[str, Any]:
    return {}


@dataclass(slots=True)
class ServiceContext:
    """Prepared execution context for one event work item."""

    envelope: EventEnvelope
    entity_id: str
    dedupe_key: str
    idempotency_store: IdempotencyStore | None = None
    attempt_count: int = 0
    next_attempt_at: datetime | None = None
    last_error: Exception | None = None
    extras: dict[str, Any] = field(default_factory=_new_extras_dict)


@dataclass(slots=True)
class ServiceResult:
    """Execution result produced by a ServiceAdapter."""

    success: bool = True
    retryable: bool = False
    terminal: bool = False
    error: Exception | None = None
    extras: dict[str, Any] = field(default_factory=_new_extras_dict)


class ServiceAdapter(Protocol):
    """Contract for prepare/execute/commit service orchestration."""

    def name(self) -> str:
        """Return a human-readable service name."""
        ...

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        """Build and return context for execution, or None to skip."""
        ...

    def execute(self, context: ServiceContext) -> ServiceResult:
        """Run side effects for a prepared work item and return result metadata."""
        ...

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        """Persist completion/writeback state after successful execute."""
        ...


class AdapterBackedEventPlugin(EventPlugin):
    """EventPlugin bridge that delegates lifecycle phases to a ServiceAdapter.

    The bridge stores prepared context/result between lifecycle phases so the
    existing EventRegistry flow (filter -> handle -> mark_done) remains intact.
    """

    def __init__(self, adapter: ServiceAdapter) -> None:
        self._adapter = adapter
        self._prepared: dict[tuple[str, str | None], ServiceContext] = {}
        self._results: dict[tuple[str, str | None], ServiceResult] = {}

    def name(self) -> str:
        return self._adapter.name()

    def filter(self, envelope: EventEnvelope) -> bool:
        key = _envelope_key(envelope)
        self._clear_key(key)
        context = self._adapter.prepare(envelope)
        if context is None:
            return False
        self._prepared[key] = context
        return True

    def handle(self, envelope: EventEnvelope) -> None:
        key = _envelope_key(envelope)
        context = self._prepared.get(key)
        if context is None:
            raise RuntimeError(
                "AdapterBackedEventPlugin.handle called before successful filter"
            )
        try:
            self._results[key] = self._adapter.execute(context)
        except Exception:
            self._clear_key(key)
            raise

    def mark_done(self, envelope: EventEnvelope) -> None:
        key = _envelope_key(envelope)
        context = self._prepared.pop(key, None)
        result = self._results.pop(key, None)
        if context is None:
            raise RuntimeError(
                "AdapterBackedEventPlugin.mark_done called without prepared context"
            )
        if result is None:
            raise RuntimeError(
                "AdapterBackedEventPlugin.mark_done called before handle result"
            )
        self._adapter.commit(context, result)

    def _clear_key(self, key: tuple[str, str | None]) -> None:
        self._prepared.pop(key, None)
        self._results.pop(key, None)


def _envelope_key(envelope: EventEnvelope) -> tuple[str, str | None]:
    return (str(envelope.type), envelope.document_id())
