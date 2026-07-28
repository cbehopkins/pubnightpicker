"""Queue runner: event dispatch loop with healthcheck integration.

Event work items are treated as a state machine with explicit lifecycle states
(`enqueued`, `running`, `retry_scheduled`, `completed`, `terminal_failed`,
`dequeued`). Completion is not final until the plugin's writeback/mark_done
step succeeds.
"""

import heapq
import logging
import queue as _queue
from dataclasses import dataclass, field
from collections.abc import Callable, Sequence
from datetime import UTC, datetime, timedelta
from typing import Protocol

from firebase_sub.event import Event, EventEnvelope
from firebase_sub.my_types import RetryableServiceError, TerminalServiceError
from firebase_sub.plugins.protocols import EventWorkItemState
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
_DEFAULT_REQUEUE_BASE_DELAY_SECONDS = 0.1
_DEFAULT_REQUEUE_MAX_DELAY_SECONDS = 5.0
_DEFAULT_UNKNOWN_ERROR_MAX_RETRIES = 3


@dataclass(order=True)
class _RetryEntry:
    run_at: datetime
    sequence: int
    event: Event = field(compare=False)


class ScheduledRunnerProtocol(Protocol):
    def run_due(self, *, now: datetime) -> None: ...

    def seconds_until_next(self, *, now: datetime) -> float | None: ...


class QueueRunner:
    """Processes events from a JobQueue, running healthchecks on idle timeout.

    Healthchecks are callables that return ``None`` when healthy or an error
    message string when a problem is detected.  The first non-None message
    triggers ``SystemExit``.

    Dispatches all events through EventRegistry and manages the event-work-item
    lifecycle from enqueue through retry scheduling and completion.
    """

    def __init__(
        self,
        *,
        event_queue: JobQueue[Event],
        healthcheck_interval_seconds: float,
        healthchecks: Sequence[Callable[[], str | None]],
        registry: EventRegistry,
        scheduled_runner: ScheduledRunnerProtocol | None = None,
        requeue_base_delay_seconds: float = _DEFAULT_REQUEUE_BASE_DELAY_SECONDS,
        requeue_max_delay_seconds: float = _DEFAULT_REQUEUE_MAX_DELAY_SECONDS,
        unknown_error_max_retries: int = _DEFAULT_UNKNOWN_ERROR_MAX_RETRIES,
    ) -> None:
        self._queue = event_queue
        self._healthcheck_interval_seconds = healthcheck_interval_seconds
        self._healthchecks = list(healthchecks)
        self._registry = registry
        self._scheduled_runner = scheduled_runner
        self._requeue_base_delay_seconds = max(0.0, requeue_base_delay_seconds)
        self._requeue_max_delay_seconds = max(
            self._requeue_base_delay_seconds,
            requeue_max_delay_seconds,
        )
        self._unknown_error_max_retries = max(0, unknown_error_max_retries)
        self._retry_attempts: dict[tuple[str, str | None], int] = {}
        self._unknown_retry_attempts: dict[tuple[str, str | None], int] = {}
        self._retry_queue: list[_RetryEntry] = []
        self._retry_sequence = 0

    def run_forever(self) -> None:
        """Process events until a healthcheck fails or an unhandled error occurs."""
        while True:
            timeout_seconds = self._healthcheck_interval_seconds
            now = datetime.now(UTC)
            self._enqueue_due_retries(now=now)
            if self._scheduled_runner is not None:
                self._scheduled_runner.run_due(now=now)
                next_due_seconds = self._scheduled_runner.seconds_until_next(now=now)
                if next_due_seconds is not None:
                    timeout_seconds = min(timeout_seconds, next_due_seconds)
            retry_due_seconds = self._seconds_until_next_retry(now=now)
            if retry_due_seconds is not None:
                timeout_seconds = min(timeout_seconds, retry_due_seconds)

            try:
                event = self._queue.get(timeout=timeout_seconds)
            except _queue.Empty:
                now = datetime.now(UTC)
                self._enqueue_due_retries(now=now)
                if self._scheduled_runner is not None:
                    self._scheduled_runner.run_due(now=now)
                for check in self._healthchecks:
                    if msg := check():
                        raise SystemExit(msg)
                continue

            envelope = EventEnvelope(type=event.type, doc=event.doc)
            event_key = _event_retry_key(event)
            try:
                _log.debug(
                    "Event work item state=%s event=%s doc_id=%s",
                    EventWorkItemState.RUNNING,
                    event.type,
                    envelope.document_id(),
                )
                self._registry.dispatch(envelope)
            except RetryableServiceError as exc:
                next_attempt = self._retry_attempts.get(event_key, 0) + 1
                self._retry_attempts[event_key] = next_attempt
                self._unknown_retry_attempts.pop(event_key, None)
                backoff_seconds = min(
                    self._requeue_max_delay_seconds,
                    _retry_backoff_seconds(
                        attempt=next_attempt,
                        base_delay_seconds=self._requeue_base_delay_seconds,
                    ),
                )
                _log.warning(
                    "Event work item state=%s event=%s doc_id=%s; "
                    "scheduling retry attempt=%s in %.3fs: %s",
                    EventWorkItemState.RETRY_SCHEDULED,
                    event.type,
                    envelope.document_id(),
                    next_attempt,
                    backoff_seconds,
                    exc,
                    exc_info=True,
                )
                self._schedule_retry(event=event, delay_seconds=backoff_seconds)
                continue
            except TerminalServiceError as exc:
                _log.error(
                    "Event work item state=%s event=%s doc_id=%s; terminal error: %s",
                    EventWorkItemState.TERMINAL_FAILED,
                    event.type,
                    envelope.document_id(),
                    exc,
                    exc_info=True,
                )
                raise
            except Exception as exc:
                # Backward-compatible path for wrapped and legacy transient errors
                # that are not yet raised as RetryableServiceError directly.
                # FIXME - add an assert False here to catch them for our attention
                if _is_transient_runtime_error(exc):
                    next_attempt = self._retry_attempts.get(event_key, 0) + 1
                    self._retry_attempts[event_key] = next_attempt
                    self._unknown_retry_attempts.pop(event_key, None)
                    backoff_seconds = min(
                        self._requeue_max_delay_seconds,
                        _retry_backoff_seconds(
                            attempt=next_attempt,
                            base_delay_seconds=self._requeue_base_delay_seconds,
                        ),
                    )
                    _log.warning(
                        "Event work item state=%s event=%s doc_id=%s; "
                        "scheduling retry attempt=%s in %.3fs: %s",
                        EventWorkItemState.RETRY_SCHEDULED,
                        event.type,
                        envelope.document_id(),
                        next_attempt,
                        backoff_seconds,
                        exc,
                        exc_info=True,
                    )
                    self._schedule_retry(event=event, delay_seconds=backoff_seconds)
                    continue
                if _is_terminal_runtime_error(exc):
                    # FIXME - add an assert False here to catch them for our attention
                    _log.error(
                        "Event work item state=%s event=%s doc_id=%s; terminal error: %s",
                        EventWorkItemState.TERMINAL_FAILED,
                        event.type,
                        envelope.document_id(),
                        exc,
                        exc_info=True,
                    )
                    raise

                next_attempt = self._unknown_retry_attempts.get(event_key, 0) + 1
                if next_attempt <= self._unknown_error_max_retries:
                    self._unknown_retry_attempts[event_key] = next_attempt
                    self._retry_attempts.pop(event_key, None)
                    backoff_seconds = min(
                        self._requeue_max_delay_seconds,
                        _retry_backoff_seconds(
                            attempt=next_attempt,
                            base_delay_seconds=self._requeue_base_delay_seconds,
                        ),
                    )
                    _log.warning(
                        "Event work item state=%s event=%s doc_id=%s; "
                        "unknown error retry attempt=%s/%s in %.3fs: %s",
                        EventWorkItemState.RETRY_SCHEDULED,
                        event.type,
                        envelope.document_id(),
                        next_attempt,
                        self._unknown_error_max_retries,
                        backoff_seconds,
                        exc,
                        exc_info=True,
                    )
                    self._schedule_retry(event=event, delay_seconds=backoff_seconds)
                    continue

                _log.error(
                    "Event work item state=%s event=%s doc_id=%s; "
                    "unknown error retry budget exhausted attempts=%s: %s",
                    EventWorkItemState.TERMINAL_FAILED,
                    event.type,
                    envelope.document_id(),
                    next_attempt - 1,
                    exc,
                    exc_info=True,
                )
                self._unknown_retry_attempts.pop(event_key, None)
                raise
            self._retry_attempts.pop(event_key, None)
            self._unknown_retry_attempts.pop(event_key, None)
            _log.debug(
                "Event work item state=%s event=%s doc_id=%s",
                EventWorkItemState.COMPLETED,
                event.type,
                envelope.document_id(),
            )
            if self._scheduled_runner is not None:
                self._scheduled_runner.run_due(now=datetime.now(UTC))
            _log.debug(
                "Event work item state=%s event=%s doc_id=%s",
                EventWorkItemState.DEQUEUED,
                event.type,
                envelope.document_id(),
            )
            _log_event(event)

    def _schedule_retry(self, *, event: Event, delay_seconds: float) -> None:
        run_at = datetime.now(UTC) + timedelta(seconds=max(delay_seconds, 0.0))
        heapq.heappush(
            self._retry_queue,
            _RetryEntry(run_at=run_at, sequence=self._retry_sequence, event=event),
        )
        self._retry_sequence += 1

    def _enqueue_due_retries(self, *, now: datetime) -> None:
        while self._retry_queue and self._retry_queue[0].run_at <= now:
            entry = heapq.heappop(self._retry_queue)
            self._queue.put(entry.event)

    def _seconds_until_next_retry(self, *, now: datetime) -> float | None:
        if not self._retry_queue:
            return None
        return max((self._retry_queue[0].run_at - now).total_seconds(), 0.0)


def _is_transient_runtime_error(exc: BaseException) -> bool:
    """Return True for retryable runtime failures.

    We first respect explicit service taxonomy:
    - RetryableServiceError -> retry
    - TerminalServiceError -> do not retry

    Then we apply FQCN transient matching for infrastructure/network errors.
    We walk ``__cause__``/``__context__`` because wrappers are common.
    """
    found_retryable = False
    for current in _walk_exception_chain(exc):
        if isinstance(current, TerminalServiceError):
            return False
        if isinstance(current, RetryableServiceError):
            found_retryable = True
            continue
        for exception_type in type(current).__mro__:
            fqcn = f"{exception_type.__module__}.{exception_type.__name__}"
            if fqcn in _TRANSIENT_EXCEPTION_FQCNS:
                found_retryable = True
                break
    return found_retryable


def _is_terminal_runtime_error(exc: BaseException) -> bool:
    return any(
        isinstance(current, TerminalServiceError)
        for current in _walk_exception_chain(exc)
    )


def _walk_exception_chain(exc: BaseException) -> list[BaseException]:
    visited: set[int] = set()
    pending: list[BaseException] = [exc]
    chain: list[BaseException] = []
    while pending:
        current = pending.pop()
        current_id = id(current)
        if current_id in visited:
            continue
        visited.add(current_id)
        chain.append(current)
        cause = getattr(current, "__cause__", None)
        if isinstance(cause, BaseException):
            pending.append(cause)
        context = getattr(current, "__context__", None)
        if isinstance(context, BaseException):
            pending.append(context)
    return chain


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


def _event_retry_key(event: Event) -> tuple[str, str | None]:
    return (str(event.type), event.doc.id if event.doc is not None else None)


def _retry_backoff_seconds(*, attempt: int, base_delay_seconds: float) -> float:
    return base_delay_seconds * (2 ** (attempt - 1))
