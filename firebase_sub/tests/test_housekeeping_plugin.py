"""Tests for HousekeepingTaskPlugin and HousekeepingPluginRunner."""

from datetime import UTC, datetime, timedelta
from zoneinfo import ZoneInfo

from firebase_sub.database.housekeeping import HousekeepingTask
from firebase_sub.plugins.housekeeping import (
    DailyUtcScheduledCallablePlugin,
    HousekeepingPluginRunner,
    HousekeepingTaskPlugin,
)
from firebase_sub.plugins.protocols import (
    PlannedPluginException,
    UnexpectedPluginException,
)
from firebase_sub.plugins.scheduled_housekeeping import ScheduledHousekeepingRunner

# ---------------------------------------------------------------------------
# HousekeepingTaskPlugin
# ---------------------------------------------------------------------------


def test_task_plugin_run_calls_callback():
    called: list[str] = []
    task = HousekeepingTask(name="my_task", callback=lambda: called.append("ran"))
    plugin = HousekeepingTaskPlugin(task)

    plugin.run()

    assert called == ["ran"]


def test_task_plugin_name_returns_task_name():
    task = HousekeepingTask(name="cleanup_old_docs", callback=lambda: None)
    plugin = HousekeepingTaskPlugin(task)

    assert plugin.name() == "cleanup_old_docs"


def test_task_plugin_registration_hooks_are_no_ops():
    task = HousekeepingTask(name="t", callback=lambda: None)
    plugin = HousekeepingTaskPlugin(task)

    # Should not raise
    plugin.on_registered()
    plugin.on_unregistered()


# ---------------------------------------------------------------------------
# HousekeepingPluginRunner
# ---------------------------------------------------------------------------


class _SuccessPlugin:
    def __init__(self, name: str) -> None:
        self._name = name
        self.run_count = 0
        self.registered = False
        self.unregistered = False

    def is_enabled(self) -> bool:
        return True

    def on_registered(self) -> None:
        self.registered = True

    def on_unregistered(self) -> None:
        self.unregistered = True

    def run(self) -> None:
        self.run_count += 1

    def name(self) -> str:
        return self._name


class _RaisingPlugin:
    def __init__(self, exc: Exception) -> None:
        self._exc = exc

    def is_enabled(self) -> bool:
        return True

    def on_registered(self) -> None:
        pass

    def on_unregistered(self) -> None:
        pass

    def run(self) -> None:
        raise self._exc

    def name(self) -> str:
        return "raiser"


def test_runner_calls_on_registered_on_init():
    plugin = _SuccessPlugin("p1")
    HousekeepingPluginRunner([plugin])

    assert plugin.registered


def test_runner_run_all_calls_each_plugin():
    p1 = _SuccessPlugin("p1")
    p2 = _SuccessPlugin("p2")
    runner = HousekeepingPluginRunner([p1, p2])

    runner.run_all()

    assert p1.run_count == 1
    assert p2.run_count == 1


def test_runner_run_all_continues_after_planned_exception(caplog):
    failing = _RaisingPlugin(PlannedPluginException("expected"))
    succeeding = _SuccessPlugin("after")
    runner = HousekeepingPluginRunner([failing, succeeding])

    runner.run_all()

    assert succeeding.run_count == 1


def test_runner_run_all_continues_after_unexpected_exception(caplog):
    failing = _RaisingPlugin(UnexpectedPluginException("unexpected"))
    succeeding = _SuccessPlugin("after")
    runner = HousekeepingPluginRunner([failing, succeeding])

    runner.run_all()

    assert succeeding.run_count == 1


def test_runner_run_all_continues_after_generic_exception(caplog):
    failing = _RaisingPlugin(RuntimeError("boom"))
    succeeding = _SuccessPlugin("after")
    runner = HousekeepingPluginRunner([failing, succeeding])

    runner.run_all()

    assert succeeding.run_count == 1


def test_runner_unregister_calls_on_unregistered():
    plugin = _SuccessPlugin("p")
    runner = HousekeepingPluginRunner([plugin])

    runner.unregister()

    assert plugin.unregistered


# ---------------------------------------------------------------------------
# ScheduledHousekeepingRunner
# ---------------------------------------------------------------------------


class _ScheduledPlugin(_SuccessPlugin):
    def __init__(
        self,
        name: str,
        *,
        run_times: list[datetime | None],
        run_log: list[str] | None = None,
        exc: Exception | None = None,
    ) -> None:
        super().__init__(name)
        self._run_times = run_times
        self._run_time_index = 0
        self._run_log = run_log
        self._exc = exc

    def run_at(self, now: datetime) -> datetime | None:
        del now
        if not self._run_times:
            return None
        index = min(self._run_time_index, len(self._run_times) - 1)
        value = self._run_times[index]
        self._run_time_index += 1
        return value

    def run(self) -> None:
        if self._exc is not None:
            raise self._exc
        super().run()
        if self._run_log is not None:
            self._run_log.append(self.name())


def test_scheduled_runner_runs_due_plugin_and_reschedules():
    now = datetime(2026, 5, 19, 12, 0, tzinfo=UTC)
    next_run = now + timedelta(minutes=10)
    plugin = _ScheduledPlugin("sched", run_times=[now, next_run])
    runner = ScheduledHousekeepingRunner([plugin])

    runner.run_due(now=now)

    assert plugin.run_count == 1
    assert runner.seconds_until_next(now=now) == 600.0


def test_scheduled_runner_ignores_none_schedule():
    now = datetime(2026, 5, 19, 12, 0, tzinfo=UTC)
    plugin = _ScheduledPlugin("none", run_times=[None])
    runner = ScheduledHousekeepingRunner([plugin])

    assert runner.seconds_until_next(now=now) is None


def test_scheduled_runner_uses_registration_order_for_equal_timestamps():
    now = datetime(2026, 5, 19, 12, 0, tzinfo=UTC)
    run_log: list[str] = []
    p1 = _ScheduledPlugin("first", run_times=[now], run_log=run_log)
    p2 = _ScheduledPlugin("second", run_times=[now], run_log=run_log)
    runner = ScheduledHousekeepingRunner([p1, p2])

    runner.run_due(now=now)

    assert run_log == ["first", "second"]


def test_scheduled_runner_skips_naive_datetime_from_run_at():
    aware_now = datetime(2026, 5, 19, 12, 0, tzinfo=UTC)
    naive_time = datetime(2026, 5, 19, 12, 0)
    plugin = _ScheduledPlugin("naive", run_times=[naive_time])
    runner = ScheduledHousekeepingRunner([plugin])

    assert runner.seconds_until_next(now=aware_now) is None


def test_scheduled_runner_continues_after_plugin_exception():
    now = datetime(2026, 5, 19, 12, 0, tzinfo=UTC)
    failing = _ScheduledPlugin(
        "fail",
        run_times=[now, None],
        exc=RuntimeError("boom"),
    )
    succeeding = _ScheduledPlugin("ok", run_times=[now, None])
    runner = ScheduledHousekeepingRunner([failing, succeeding])

    runner.run_due(now=now)

    assert succeeding.run_count == 1


def test_daily_utc_scheduled_callable_returns_today_target_when_before():
    plugin = DailyUtcScheduledCallablePlugin(
        name="daily16",
        callback=lambda: None,
        hour=16,
        minute=0,
    )

    now = datetime(2026, 5, 19, 15, 59, tzinfo=UTC)
    assert plugin.run_at(now) == datetime(2026, 5, 19, 16, 0, tzinfo=UTC)


def test_daily_utc_scheduled_callable_returns_tomorrow_target_when_after():
    plugin = DailyUtcScheduledCallablePlugin(
        name="daily16",
        callback=lambda: None,
        hour=16,
        minute=0,
    )

    now = datetime(2026, 5, 19, 16, 1, tzinfo=UTC)
    assert plugin.run_at(now) == datetime(2026, 5, 20, 16, 0, tzinfo=UTC)


def test_daily_utc_scheduled_callable_rejects_naive_now():
    plugin = DailyUtcScheduledCallablePlugin(
        name="daily16",
        callback=lambda: None,
        hour=16,
        minute=0,
    )

    try:
        plugin.run_at(datetime(2026, 5, 19, 16, 0))
        raise AssertionError("Expected ValueError")
    except ValueError:
        pass


def test_daily_utc_scheduled_callable_supports_local_wall_clock_timezone():
    plugin = DailyUtcScheduledCallablePlugin(
        name="daily16-london",
        callback=lambda: None,
        hour=16,
        minute=0,
        schedule_timezone=ZoneInfo("Europe/London"),
    )

    now = datetime(2026, 5, 19, 14, 59, tzinfo=UTC)
    assert plugin.run_at(now) == datetime(2026, 5, 19, 15, 0, tzinfo=UTC)
