import logging
from contextlib import AbstractContextManager, ExitStack
from collections.abc import Sequence

from firebase_sub.plugins.housekeeping import HousekeepingPluginRunner
from firebase_sub.plugins.protocols import HousekeepingPlugin, ListenerPlugin

_log = logging.getLogger(__name__)


class PluginRuntime(AbstractContextManager["PluginRuntime"]):
    """Context manager that owns listener + housekeeping plugin lifecycle."""

    def __init__(
        self,
        *,
        listener_plugins: Sequence[ListenerPlugin],
        housekeeping_plugins: Sequence[HousekeepingPlugin],
    ) -> None:
        self._listener_plugins = list(listener_plugins)
        self._housekeeping_plugins = list(housekeeping_plugins)
        self._housekeeping_runner: HousekeepingPluginRunner | None = None
        self._stack = ExitStack()

    def __enter__(self) -> "PluginRuntime":
        register_listener_plugins(
            plugins=self._listener_plugins, exit_stack=self._stack
        )
        self._housekeeping_runner = HousekeepingPluginRunner(self._housekeeping_plugins)
        self._stack.callback(self._housekeeping_runner.unregister)
        return self

    def run_housekeeping(self) -> None:
        if self._housekeeping_runner is None:
            raise RuntimeError(
                "PluginRuntime must be entered before running housekeeping"
            )
        self._housekeeping_runner.run_all()

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self._stack.close()


def register_listener_plugins(
    *,
    plugins: Sequence[ListenerPlugin],
    exit_stack: ExitStack,
) -> None:
    """Register listener plugins and attach their context-managed resources."""
    for plugin in plugins:
        if not plugin.is_enabled():
            _log.info("Skipping disabled listener plugin: %s", plugin.name())
            continue
        _log.info("Registering listener plugin: %s", plugin.name())
        plugin.on_registered()
        exit_stack.callback(plugin.on_unregistered)
        exit_stack.enter_context(plugin.build_manager())
