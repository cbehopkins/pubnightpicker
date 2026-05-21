import contextlib

from firebase_sub.plugins.runtime import PluginRuntime


class _FakeListenerPlugin:
    def __init__(self, *, name: str, enabled: bool = True) -> None:
        self._name = name
        self._enabled = enabled
        self.registered = False
        self.unregistered = False

    def name(self) -> str:
        return self._name

    def is_enabled(self) -> bool:
        return self._enabled

    def on_registered(self) -> None:
        self.registered = True

    def on_unregistered(self) -> None:
        self.unregistered = True

    def build_manager(self):
        return contextlib.nullcontext()


def test_listener_plugin_runtime_registers_and_unregisters_enabled_plugins() -> None:
    enabled_plugin = _FakeListenerPlugin(name="enabled", enabled=True)
    disabled_plugin = _FakeListenerPlugin(name="disabled", enabled=False)
    housekeeping_calls: list[str] = []

    class _FakeHousekeepingPlugin:
        def __init__(self) -> None:
            self.registered = False
            self.unregistered = False

        def name(self) -> str:
            return "housekeeping"

        def is_enabled(self) -> bool:
            return True

        def on_registered(self) -> None:
            self.registered = True

        def on_unregistered(self) -> None:
            self.unregistered = True

        def run(self) -> None:
            housekeeping_calls.append("ran")

    housekeeping_plugin = _FakeHousekeepingPlugin()

    with PluginRuntime(
        listener_plugins=[enabled_plugin, disabled_plugin],
        housekeeping_plugins=[housekeeping_plugin],
    ) as runtime:
        assert enabled_plugin.registered is True
        assert enabled_plugin.unregistered is False
        assert disabled_plugin.registered is False
        assert housekeeping_plugin.registered is True
        runtime.run_housekeeping()
        assert housekeeping_calls == ["ran"]

    assert enabled_plugin.unregistered is True
    assert disabled_plugin.unregistered is False
    assert housekeeping_plugin.unregistered is True
