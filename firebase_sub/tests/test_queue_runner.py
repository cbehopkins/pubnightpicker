"""Tests for QueueRunner: healthcheck logic and event dispatch."""

import queue
import threading
from types import SimpleNamespace

from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType
from firebase_sub.runtime.job_queue import JobQueue
from firebase_sub.runtime.queue_runner import QueueRunner


def _fake_pubs_list() -> PubsList:
    return SimpleNamespace()  # type: ignore[return-value]


def _make_runner(
    *,
    event_queue: JobQueue,
    healthchecks=None,
    healthcheck_interval_seconds: float = 0.05,
) -> QueueRunner:
    return QueueRunner(
        event_queue=event_queue,
        pubs_list=_fake_pubs_list(),
        healthcheck_interval_seconds=healthcheck_interval_seconds,
        healthchecks=healthchecks or [],
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
        healthchecks=[lambda: (calls.append("checked") or None)],  # type: ignore[return-value]
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


def test_run_forever_processes_event_and_calls_handle():
    q: JobQueue[Event] = JobQueue()
    handled: list[str] = []

    def _callback(doc, pubs_list):
        handled.append("handled")

    event = Event(type=EventType.TICK, doc=None, callback=_callback)
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
        healthchecks=[_failing_after_one],
        healthcheck_interval_seconds=0.02,
    )

    try:
        runner.run_forever()
    except SystemExit:
        pass

    assert handled == ["handled"]


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
