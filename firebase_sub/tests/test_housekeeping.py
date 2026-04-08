from datetime import datetime, timedelta
from unittest.mock import MagicMock

from firebase_sub.database.housekeeping import (
    HousekeepingRunner,
    HousekeepingTask,
    IntervalSchedule,
)


def test_interval_schedule_due_without_last_run():
    schedule = IntervalSchedule(interval_seconds=60)

    assert schedule.is_due(datetime(2026, 4, 2, 12, 0, 0), None) is True


def test_interval_schedule_not_due_until_interval_elapsed():
    schedule = IntervalSchedule(interval_seconds=60)
    start = datetime(2026, 4, 2, 12, 0, 0)

    assert schedule.is_due(start + timedelta(seconds=59), start) is False
    assert schedule.is_due(start + timedelta(seconds=60), start) is True


def test_housekeeping_runner_executes_all_tasks_and_updates_last_run():
    task_a = MagicMock()
    task_b = MagicMock()
    runner = HousekeepingRunner(
        tasks=[
            HousekeepingTask(name="a", callback=task_a),
            HousekeepingTask(name="b", callback=task_b),
        ],
        schedule=IntervalSchedule(interval_seconds=60),
    )
    now = datetime(2026, 4, 2, 12, 0, 0)

    runner.maybe_run(now)

    task_a.assert_called_once_with()
    task_b.assert_called_once_with()
    assert runner.last_run == now


def test_housekeeping_runner_continues_after_task_failure():
    failing = MagicMock(side_effect=RuntimeError("boom"))
    succeeding = MagicMock()
    runner = HousekeepingRunner(
        tasks=[
            HousekeepingTask(name="failing", callback=failing),
            HousekeepingTask(name="succeeding", callback=succeeding),
        ],
        schedule=IntervalSchedule(interval_seconds=60),
    )

    runner.maybe_run(datetime(2026, 4, 2, 12, 0, 0))

    failing.assert_called_once_with()
    succeeding.assert_called_once_with()
