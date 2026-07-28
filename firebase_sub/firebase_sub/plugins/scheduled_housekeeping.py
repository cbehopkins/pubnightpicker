"""Time-triggered work runner for per-plugin next-run timestamps.

Time-triggered work items are treated as a state machine with explicit states
(`registered`, `scheduled`, `due`, `running`, `rescheduled`, `completed`,
`terminal_failed`). These tasks share the same runtime machinery as periodic
maintenance, but their lifecycle is deadline-driven rather than cleanup-driven.
"""

import heapq
import logging
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import cast

from firebase_sub.plugins.protocols import (
    HousekeepingPlugin,
    ScheduledWorkItemState,
    PlannedPluginException,
    ScheduledHousekeepingPlugin,
    UnexpectedPluginException,
)

_log = logging.getLogger(__name__)


@dataclass(order=True)
class _ScheduledEntry:
    run_at: datetime
    sequence: int
    plugin: HousekeepingPlugin = field(compare=False)


class ScheduledHousekeepingRunner:
    """Run time-triggered work plugins inside the main runtime loop.

    A scheduled plugin is registered, becomes due at its next run time, runs,
    and is then rescheduled or marked complete depending on its next_run state.
    """

    def __init__(self, plugins: list[HousekeepingPlugin] | None = None) -> None:
        self._queue: list[_ScheduledEntry] = []
        self._sequence = 0
        if plugins:
            self.register_plugins(plugins, now=datetime.now(UTC))

    def register_plugins(
        self,
        plugins: list[HousekeepingPlugin],
        *,
        now: datetime,
    ) -> None:
        current_time = _to_utc(now)
        for plugin in plugins:
            if not plugin.is_enabled():
                continue
            next_run = self._plugin_next_run(plugin, current_time)
            if next_run is None:
                continue
            self._enqueue(plugin, next_run)
            _log.info(
                "Time-triggered plugin %s scheduled at %s",
                plugin.name(),
                next_run.isoformat(),
            )

    def seconds_until_next(self, *, now: datetime) -> float | None:
        if not self._queue:
            return None
        current_time = _to_utc(now)
        delta = (self._queue[0].run_at - current_time).total_seconds()
        return max(delta, 0.0)

    def run_due(self, *, now: datetime) -> None:
        current_time = _to_utc(now)

        while self._queue and self._queue[0].run_at <= current_time:
            entry = heapq.heappop(self._queue)
            plugin = entry.plugin
            _log.info(
                "Time-triggered plugin due: %s at %s",
                plugin.name(),
                entry.run_at.isoformat(),
            )
            _log.debug(
                "Scheduled work item state=%s plugin=%s",
                ScheduledWorkItemState.DUE,
                plugin.name(),
            )

            try:
                _log.debug(
                    "Scheduled work item state=%s plugin=%s",
                    ScheduledWorkItemState.RUNNING,
                    plugin.name(),
                )
                plugin.run()
                _log.info("Time-triggered plugin completed: %s", plugin.name())
            except PlannedPluginException:
                _log.debug(
                    "Scheduled work item state=%s plugin=%s",
                    ScheduledWorkItemState.TERMINAL_FAILED,
                    plugin.name(),
                )
                _log.warning(
                    "Time-triggered plugin planned failure: %s",
                    plugin.name(),
                    exc_info=True,
                )
            except UnexpectedPluginException:
                _log.debug(
                    "Scheduled work item state=%s plugin=%s",
                    ScheduledWorkItemState.TERMINAL_FAILED,
                    plugin.name(),
                )
                _log.error(
                    "Time-triggered plugin unexpected failure: %s",
                    plugin.name(),
                    exc_info=True,
                )
            except Exception:
                _log.debug(
                    "Scheduled work item state=%s plugin=%s",
                    ScheduledWorkItemState.TERMINAL_FAILED,
                    plugin.name(),
                )
                _log.exception(
                    "Time-triggered plugin uncaught failure: %s",
                    plugin.name(),
                )

            after_run = current_time
            next_run = self._plugin_next_run(plugin, after_run)
            if next_run is None:
                _log.debug(
                    "Scheduled work item state=%s plugin=%s",
                    ScheduledWorkItemState.COMPLETED,
                    plugin.name(),
                )
                continue
            if next_run <= after_run:
                # Prevent tight loops if a plugin returns a non-future schedule.
                next_run = after_run + timedelta(seconds=1)
            _log.debug(
                "Scheduled work item state=%s plugin=%s next_run=%s",
                ScheduledWorkItemState.RESCHEDULED,
                plugin.name(),
                next_run.isoformat(),
            )
            self._enqueue(plugin, next_run)
            _log.info(
                "Time-triggered plugin %s next run at %s",
                plugin.name(),
                next_run.isoformat(),
            )

    def _enqueue(self, plugin: HousekeepingPlugin, run_at: datetime) -> None:
        entry = _ScheduledEntry(
            run_at=run_at,
            sequence=self._sequence,
            plugin=plugin,
        )
        self._sequence += 1
        heapq.heappush(self._queue, entry)

    def _plugin_next_run(
        self,
        plugin: HousekeepingPlugin,
        now: datetime,
    ) -> datetime | None:
        if not isinstance(plugin, ScheduledHousekeepingPlugin):
            return None

        next_run_raw = cast(ScheduledHousekeepingPlugin, plugin).run_at(now)
        if next_run_raw is None:
            return None
        if next_run_raw.tzinfo is None:
            _log.error(
                "Time-triggered plugin %s returned naive datetime; skipping",
                plugin.name(),
            )
            return None
        return _to_utc(next_run_raw)


def _to_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        raise ValueError("Expected timezone-aware datetime")
    return value.astimezone(UTC)
