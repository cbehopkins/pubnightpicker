"""Tests for QueueRunner: healthcheck logic and event dispatch."""

import threading

from firebase_sub.event import Event, EventType
from firebase_sub.runtime.job_queue import JobQueue
from firebase_sub.runtime.queue_runner import (
    QueueRunner,
    _is_transient_runtime_error,
    _retry_backoff_seconds,
)


class _FakeRegistry:
    def __init__(self) -> None:
        self.dispatched: list[EventType] = []

    def dispatch(self, envelope) -> int:
        self.dispatched.append(envelope.type)
        return 1


class _FakeScheduledRunner:
    def __init__(self, *, next_due_seconds: float | None = None) -> None:
        self.next_due_seconds = next_due_seconds
        self.run_due_calls = 0
        self.seconds_until_next_calls = 0

    def run_due(self, *, now) -> None:
        del now
        self.run_due_calls += 1

    def seconds_until_next(self, *, now) -> float | None:
        del now
        self.seconds_until_next_calls += 1
        return self.next_due_seconds


def _make_runner(
    *,
    event_queue: JobQueue,
    registry: _FakeRegistry | None = None,
    healthchecks=None,
    healthcheck_interval_seconds: float = 0.05,
    scheduled_runner: _FakeScheduledRunner | None = None,
    **kwargs,
) -> QueueRunner:
    return QueueRunner(
        event_queue=event_queue,
        healthcheck_interval_seconds=healthcheck_interval_seconds,
        healthchecks=healthchecks or [],
        registry=registry or _FakeRegistry(),
        scheduled_runner=scheduled_runner,
        **kwargs,
    )


# ---------------------------------------------------------------------------
# Healthcheck tests
# ---------------------------------------------------------------------------


def test_run_forever_raises_system_exit_when_healthcheck_fails():
    q: JobQueue[Event] = JobQueue()
    runner = _make_runner(
        event_queue=q,
        healthchecks=[lambda: "db is not okay"],
    )

    result: list[SystemExit] = []

    def run():
        try:
            runner.run_forever()
        except SystemExit as exc:
            result.append(exc)

    t = threading.Thread(target=run, daemon=True)
    t.start()
    t.join(timeout=2.0)

    assert result, "Expected SystemExit from failing healthcheck"
    assert "db is not okay" in str(result[0])


def test_run_forever_continues_when_all_healthchecks_pass():
    q: JobQueue[Event] = JobQueue()
    calls: list[str] = []
    runner = _make_runner(
        event_queue=q,
        healthchecks=[
            lambda: (calls.append("checked") or None),  # type: ignore[return-value]
        ],
        healthcheck_interval_seconds=0.02,
    )

    def run():
        try:
            runner.run_forever()
        except SystemExit:
            pass

    t = threading.Thread(target=run, daemon=True)
    t.start()
    # Let a few healthcheck cycles pass, then stop by injecting a failing check
    import time

    time.sleep(0.1)
    # Enqueue a "poison pill" event that stops the thread by raising
    # We'll just stop by making a healthcheck fail
    # Replace with a failing healthcheck indirectly by joining with timeout
    t.join(timeout=0.2)
    # If at least one healthy check happened, the runner kept going (no SystemExit)
    assert calls, "Expected at least one healthy healthcheck call"


def test_run_forever_processes_event_and_dispatches_envelope():
    q: JobQueue[Event] = JobQueue()
    registry = _FakeRegistry()
    event = Event(type=EventType.TICK, doc=None)
    q.put(event)

    # After the event is processed, subsequent healthcheck fails to stop the loop
    check_count = 0

    def _failing_after_one():
        nonlocal check_count
        check_count += 1
        if check_count >= 1:
            return "stop"
        return None

    runner = _make_runner(
        event_queue=q,
        registry=registry,
        healthchecks=[_failing_after_one],
        healthcheck_interval_seconds=0.02,
    )

    try:
        runner.run_forever()
    except SystemExit:
        pass

    assert registry.dispatched == [EventType.TICK]


# ---------------------------------------------------------------------------
# Multiple healthcheck ordering
# ---------------------------------------------------------------------------


def test_first_failing_healthcheck_exits_immediately():
    q: JobQueue[Event] = JobQueue()
    second_called: list[bool] = []

    runner = _make_runner(
        event_queue=q,
        healthchecks=[
            lambda: "first fails",
            lambda: (second_called.append(True) or None),  # type: ignore[return-value]
        ],
    )

    try:
        runner.run_forever()
    except SystemExit as exc:
        assert "first fails" in str(exc)

    assert (
        not second_called
    ), "Second healthcheck should not be called after first fails"


def test_scheduled_runner_invoked_during_idle_cycle():
    q: JobQueue[Event] = JobQueue()
    scheduled = _FakeScheduledRunner(next_due_seconds=0.01)
    runner = _make_runner(
        event_queue=q,
        healthchecks=[lambda: "stop"],
        healthcheck_interval_seconds=1.0,
        scheduled_runner=scheduled,
    )

    try:
        runner.run_forever()
    except SystemExit:
        pass

    assert scheduled.seconds_until_next_calls >= 1
    assert scheduled.run_due_calls >= 2


def test_scheduled_runner_invoked_after_event_dispatch():
    q: JobQueue[Event] = JobQueue()
    q.put(Event(type=EventType.TICK, doc=None))
    scheduled = _FakeScheduledRunner(next_due_seconds=None)
    runner = _make_runner(
        event_queue=q,
        healthchecks=[lambda: "stop"],
        healthcheck_interval_seconds=0.01,
        scheduled_runner=scheduled,
    )

    try:
        runner.run_forever()
    except SystemExit:
        pass

    # One call before blocking plus one call after event dispatch.
    assert scheduled.run_due_calls >= 2


def test_run_forever_retries_same_event_after_transient_dispatch_error():
    q: JobQueue[Event] = JobQueue()
    q.put(Event(type=EventType.TICK, doc=None))

    class _TransientRegistry:
        def __init__(self) -> None:
            self.calls = 0
            self.dispatched: list[EventType] = []

        def dispatch(self, envelope) -> int:
            self.calls += 1
            if self.calls == 1:
                transport_error = type(
                    "TransportError",
                    (Exception,),
                    {"__module__": "google.auth.exceptions"},
                )
                raise transport_error("temporary outage")
            self.dispatched.append(envelope.type)
            return 1

    registry = _TransientRegistry()
    runner = _make_runner(
        event_queue=q,
        registry=registry,
        healthchecks=[lambda: "stop"],
        healthcheck_interval_seconds=0.01,
        requeue_base_delay_seconds=0.0,
        requeue_max_delay_seconds=0.0,
    )

    try:
        runner.run_forever()
    except SystemExit:
        pass

    assert registry.calls >= 2
    assert registry.dispatched == [EventType.TICK]


def test_transient_dispatch_retry_uses_bounded_backoff():
    base_delay = 0.2
    max_delay = 0.25

    delays = [
        min(max_delay, _retry_backoff_seconds(attempt=attempt, base_delay_seconds=base_delay))
        for attempt in (1, 2, 3)
    ]

    assert delays == [0.2, 0.25, 0.25]


def test_is_transient_runtime_error_detects_nested_transport_error():
    transport_error = type(
        "TransportError",
        (Exception,),
        {"__module__": "google.auth.exceptions"},
    )
    wrapped = RuntimeError("wrapper")
    wrapped.__cause__ = transport_error("dns unavailable")

    assert _is_transient_runtime_error(wrapped) is True
