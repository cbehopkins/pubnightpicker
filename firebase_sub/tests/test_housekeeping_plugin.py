"""Tests for HousekeepingTaskPlugin and HousekeepingPluginRunner."""

import pytest

from firebase_sub.database.housekeeping import HousekeepingTask
from firebase_sub.plugins.housekeeping import (
    HousekeepingPluginRunner,
    HousekeepingTaskPlugin,
)
from firebase_sub.plugins.protocols import (
    PlannedPluginException,
    UnexpectedPluginException,
)

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
