"""Housekeeping plugin implementations.

Adapts existing HousekeepingTask callbacks to the HousekeepingPlugin protocol
and provides a runner that enforces the plugin exception contract.
"""

import logging
from collections.abc import Callable, Sequence

from firebase_sub.database.housekeeping import HousekeepingTask
from firebase_sub.plugins.protocols import (
    HousekeepingPlugin,
    PlannedPluginException,
    UnexpectedPluginException,
)

_log = logging.getLogger(__name__)


class HousekeepingTaskPlugin(HousekeepingPlugin):
    """Adapts a HousekeepingTask callback to the HousekeepingPlugin protocol."""

    def __init__(self, task: HousekeepingTask) -> None:
        self._task = task

    def run(self) -> None:
        self._task.callback()

    def name(self) -> str:
        return self._task.name


class HousekeepingCallablePlugin(HousekeepingPlugin):
    """Housekeeping plugin backed by an explicit callback."""

    def __init__(
        self,
        *,
        name: str,
        callback: Callable[[], None],
        enabled: bool = True,
    ) -> None:
        self._name = name
        self._callback = callback
        self._enabled = enabled

    def is_enabled(self) -> bool:
        return self._enabled

    def run(self) -> None:
        self._callback()

    def name(self) -> str:
        return self._name


class HousekeepingPluginRunner:
    """Runs all registered housekeeping plugins in sequence.

    Exception contract:
    - ``PlannedPluginException``: logged as a warning; execution continues.
    - ``UnexpectedPluginException``: logged as an error; execution continues.
    - Any other ``Exception``: logged as an error; execution continues.

    This matches the original HousekeepingRunner behaviour (catch-all + log)
    while adding explicit protocol awareness.
    """

    def __init__(self, plugins: Sequence[HousekeepingPlugin]) -> None:
        self._plugins = [plugin for plugin in plugins if plugin.is_enabled()]
        for plugin in self._plugins:
            plugin.on_registered()

    def unregister(self) -> None:
        for plugin in self._plugins:
            plugin.on_unregistered()

    def run_all(self) -> None:
        _log.info("Housekeeping run started (%s tasks)", len(self._plugins))
        for plugin in self._plugins:
            try:
                plugin.run()
                _log.info("Housekeeping plugin completed: %s", plugin.name())
            except PlannedPluginException:
                _log.warning(
                    "Housekeeping plugin planned failure: %s",
                    plugin.name(),
                    exc_info=True,
                )
            except UnexpectedPluginException:
                _log.error(
                    "Housekeeping plugin unexpected failure: %s",
                    plugin.name(),
                    exc_info=True,
                )
            except Exception:
                _log.exception(
                    "Housekeeping plugin uncaught failure: %s", plugin.name()
                )
        _log.info("Housekeeping run finished")
