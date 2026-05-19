from unittest.mock import MagicMock

from firebase_sub.plugins.plugin_config import (
    build_housekeeping_plugins,
    build_scheduled_housekeeping_plugins,
)


def test_build_housekeeping_plugins_uses_explicit_static_registration() -> None:
    plugins = build_housekeeping_plugins(db=MagicMock())

    plugin_names = [plugin.name() for plugin in plugins]
    assert plugin_names == [
        "delete_notification_diagnostics",
        "delete_notification_docs_for_past_polls",
        "delete_inactive_push_endpoints",
        "delete_stale_push_diagnostic_entries",
        "delete_stale_poll_action_audit_entries",
        "maintain_event_recurrence_polls",
    ]


def test_build_scheduled_housekeeping_plugins_defaults_to_empty() -> None:
    plugins = build_scheduled_housekeeping_plugins(db=MagicMock())

    assert plugins == []
