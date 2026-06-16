"""Queue runner: event dispatch loop with healthcheck integration."""

import logging
import queue as _queue
from collections.abc import Callable, Sequence
from datetime import UTC, datetime
from typing import Protocol

from firebase_sub.event import Event, EventEnvelope
from firebase_sub.runtime.event_registry import EventRegistry
from firebase_sub.runtime.job_queue import JobQueue

_log = logging.getLogger(__name__)

_TRANSIENT_EXCEPTION_FQCNS = {
    "google.auth.exceptions.TransportError",
    "requests.exceptions.ConnectionError",
    "urllib3.exceptions.MaxRetryError",
    "urllib3.exceptions.NameResolutionError",
    "socket.gaierror",
    "grpc.RpcError",
    "grpc._channel._InactiveRpcError",
}


class ScheduledRunnerProtocol(Protocol):
    def run_due(self, *, now: datetime) -> None: ...

    def seconds_until_next(self, *, now: datetime) -> float | None: ...


class QueueRunner:
    """Processes events from a JobQueue, running healthchecks on idle timeout.

    Healthchecks are callables that return ``None`` when healthy or an error
    message string when a problem is detected.  The first non-None message
    triggers ``SystemExit``.

    Dispatches all events through EventRegistry.
    """

    def __init__(
        self,
        *,
        event_queue: JobQueue[Event],
        healthcheck_interval_seconds: float,
        healthchecks: Sequence[Callable[[], str | None]],
        registry: EventRegistry,
        scheduled_runner: ScheduledRunnerProtocol | None = None,
    ) -> None:
        self._queue = event_queue
        self._healthcheck_interval_seconds = healthcheck_interval_seconds
        self._healthchecks = list(healthchecks)
        self._registry = registry
        self._scheduled_runner = scheduled_runner

    def run_forever(self) -> None:
        """Process events until a healthcheck fails or an unhandled error occurs."""
        while True:
            timeout_seconds = self._healthcheck_interval_seconds
            if self._scheduled_runner is not None:
                now = datetime.now(UTC)
                self._scheduled_runner.run_due(now=now)
                next_due_seconds = self._scheduled_runner.seconds_until_next(now=now)
                if next_due_seconds is not None:
                    timeout_seconds = min(timeout_seconds, next_due_seconds)

            try:
                event = self._queue.get(timeout=timeout_seconds)
            except _queue.Empty:
                if self._scheduled_runner is not None:
                    self._scheduled_runner.run_due(now=datetime.now(UTC))
                for check in self._healthchecks:
                    if msg := check():
                        raise SystemExit(msg)
                continue

            envelope = EventEnvelope(type=event.type, doc=event.doc)
            try:
                self._registry.dispatch(envelope)
            except Exception as exc:
                if _is_transient_runtime_error(exc):
                    _log.warning(
                        "Transient dispatch failure for event %s doc_id=%s; "
                        "deferring to next retry cycle: %s",
                        event.type,
                        envelope.document_id(),
                        exc,
                        exc_info=True,
                    )
                    continue
                raise
            if self._scheduled_runner is not None:
                self._scheduled_runner.run_due(now=datetime.now(UTC))
            _log_event(event)


def _is_transient_runtime_error(exc: BaseException) -> bool:
    """Return True for known transient network/auth failures.

    We walk ``__cause__``/``__context__`` because Google auth and requests often
    wrap lower-level network failures.
    """
    visited: set[int] = set()
    pending: list[BaseException] = [exc]

    while pending:
        current = pending.pop()
        current_id = id(current)
        if current_id in visited:
            continue
        visited.add(current_id)

        for exception_type in type(current).__mro__:
            fqcn = f"{exception_type.__module__}.{exception_type.__name__}"
            if fqcn in _TRANSIENT_EXCEPTION_FQCNS:
                return True

        cause = getattr(current, "__cause__", None)
        if isinstance(cause, BaseException):
            pending.append(cause)
        context = getattr(current, "__context__", None)
        if isinstance(context, BaseException):
            pending.append(context)

    return False


def _log_event(event: Event) -> None:
    if not event.doc:
        _log.debug("Completed Event: Type:%s", event.type)
        return
    doc = event.doc.to_dict()
    if doc is None:
        _log.warning(
            "Received event %s for doc %s with no payload",
            event.type,
            event.doc.id,
        )
        return
    event_date = doc.get("date")
    completed = doc.get("completed", False)
    _log.info(
        "Completed Event: Type:%s, Date:%s, Completed:%s",
        event.type,
        event_date,
        completed,
    )
