"""Event plugin registry for dispatcher-based routing.

Maps EventType to ordered lists of EventPlugins that should handle each event.
Enables multiple plugins per event type with deterministic execution order.
"""

import logging
from collections import defaultdict
from collections.abc import Sequence

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import EventPlugin

_log = logging.getLogger(__name__)


class EventWritebackError(RuntimeError):
    """Raised when a plugin handler succeeds but mark_done writeback fails."""


class EventRegistry:
    """Registry mapping EventType to ordered EventPlugin handlers.

    Each dispatched event follows the explicit lifecycle:
    filter -> running(handle) -> completion(writeback via mark_done).
    """

    def __init__(self) -> None:
        """Initialize an empty registry."""
        self._subscriptions: dict[EventType, list[EventPlugin]] = defaultdict(list)

    def subscribe(self, event_type: EventType, plugin: EventPlugin) -> None:
        """Register a plugin to handle a specific event type.

        Plugins are appended in subscription order; execution follows
        registration order (deterministic for reproducibility).

        Args:
            event_type: The EventType this plugin subscribes to.
            plugin: The EventPlugin to invoke.
        """
        self._subscriptions[event_type].append(plugin)
        _log.debug(
            "EventRegistry: subscribed %s to %s (total: %d)",
            plugin.name(),
            event_type,
            len(self._subscriptions[event_type]),
        )

    def get_plugins(self, event_type: EventType) -> Sequence[EventPlugin]:
        """Retrieve ordered plugins for an event type.

        Args:
            event_type: The EventType to look up.

        Returns:
            Sequence of EventPlugins (possibly empty if no subscriptions).
        """
        return self._subscriptions.get(event_type, [])

    def dispatch(self, envelope: EventEnvelope) -> int:
        """Execute all plugins for an event envelope, following filter-handle-mark_done lifecycle.

        For each subscribed plugin (in order):
        1. Call filter(envelope) to check if plugin should run.
        2. If True, call handle(envelope) to execute side effect.
          3. If handle succeeds (no exception), call mark_done(envelope) to persist state.
              The event is not considered completed until this writeback succeeds.

        Args:
            envelope: The event to dispatch.

        Returns:
            Count of plugins whose filter returned True and were executed.

        Raises:
            BasePluginException: if any plugin's filter, handle, or mark_done raises.
                The first exception is propagated; subsequent plugins are not called.
        """
        plugins = self.get_plugins(envelope.type)
        executed_count = 0

        for plugin in plugins:
            if not plugin.filter(envelope):
                _log.debug(
                    "Plugin %s filter returned False for event %s doc_id=%s",
                    plugin.name(),
                    envelope.type,
                    envelope.document_id(),
                )
                continue

            _log.debug(
                "Plugin %s filter returned True; calling handle for event %s doc_id=%s",
                plugin.name(),
                envelope.type,
                envelope.document_id(),
            )
            try:
                plugin.handle(envelope)
            except Exception as exc:
                _log.exception(
                    "Plugin %s handle failed for event %s doc_id=%s: %s",
                    plugin.name(),
                    envelope.type,
                    envelope.document_id(),
                    exc,
                )
                raise

            executed_count += 1
            _log.debug(
                "Plugin %s handle succeeded; calling mark_done for event %s doc_id=%s",
                plugin.name(),
                envelope.type,
                envelope.document_id(),
            )
            try:
                plugin.mark_done(envelope)
            except Exception as exc:
                _log.exception(
                    "Plugin %s mark_done writeback failed for event %s doc_id=%s: %s",
                    plugin.name(),
                    envelope.type,
                    envelope.document_id(),
                    exc,
                )
                raise EventWritebackError(
                    f"Plugin {plugin.name()} mark_done writeback failed for event "
                    f"{envelope.type} doc_id={envelope.document_id()}"
                ) from exc

            _log.info(
                "Plugin %s successfully processed event %s doc_id=%s",
                plugin.name(),
                envelope.type,
                envelope.document_id(),
            )

        return executed_count
