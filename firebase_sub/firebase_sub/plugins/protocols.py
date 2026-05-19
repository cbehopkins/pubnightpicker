"""
Plugin protocols and base exceptions for the event subscription system.

This module defines the contracts that all plugins must implement. The system
uses a plugin-based architecture to maintain clear separation of concerns and
allow easy addition/removal of listeners and background tasks.

Design Principle: Database-Derived Stateful Plugins
=====================================================
Plugins may maintain local state for performance optimization (e.g., caching
database content, tracking conditions for triggers). However, all authoritative
state MUST live in the database. Plugins are reproducible: their state can
always be reconstructed from the database.

This means:
- If a plugin crashes or restarts, its state can be recovered from the database
- Database queries always reflect ground truth; plugin caches are optimizations only
- No data loss or corruption can result from plugin failures
- Plugins are loosely coupled to the system; they don't own critical data

Examples:
- A listener caching user preferences to reduce database queries: acceptable
  (cache is reconstructible from the "users" collection)
- A housekeeping task reading state to detect a trigger condition: acceptable
  (the detection logic is reproducible; only kept local to reduce queries)
- A plugin storing unique state that can't be recovered from the database: NOT acceptable
  (violates the principle; leads to hard-to-recover failures)
"""

from abc import ABC, abstractmethod
from contextlib import AbstractContextManager
from enum import StrEnum
from typing import TYPE_CHECKING, Protocol, runtime_checkable

from google.cloud.firestore_v1.query import Query

from firebase_sub.my_types import PollId

if TYPE_CHECKING:
    from firebase_sub.action_track import ActionMan
    from firebase_sub.database.pubs_list import PubsList

__all__ = [
    "BasePluginException",
    "PlannedPluginException",
    "UnexpectedPluginException",
    "PluginKind",
    "Plugin",
    "ListenerPlugin",
    "HousekeepingPlugin",
    "PollStatusQueryDbHandler",
    "NewPollDbHandler",
    "CompletePollDbHandler",
    "PollListenerDbHandler",
]


# ============================================================================
# Exception Hierarchy
# ============================================================================


class BasePluginException(Exception):
    """Base exception for all plugin errors.

    All exceptions raised by plugins must derive from this class. This allows
    the system to distinguish plugin errors from infrastructure errors.
    """

    pass


class PlannedPluginException(BasePluginException):
    """Exception for errors that are planned and handled in the plugin design.

    Use this when a plugin encounters an error condition that was anticipated
    and is part of the plugin's error handling strategy. The system will still
    terminate, but log this as an expected error.

    Example: A housekeeping task that fails because a required external service
    is temporarily unavailable, but the plugin has logic to handle this gracefully.
    """

    pass


class UnexpectedPluginException(BasePluginException):
    """Exception for errors that indicate a design fault or unexpected condition.

    Use this when a plugin encounters an error that should not occur under normal
    circumstances. This signals to the system that there is a gap in error handling
    that should be investigated.

    Example: A listener receives a malformed document that violates the schema,
    indicating a bug or external data corruption.
    """

    pass


# ============================================================================
# Plugin Protocols
# ============================================================================


class PluginKind(StrEnum):
    LISTENER = "listener"
    HOUSEKEEPING = "housekeeping"


class Plugin(ABC):
    """Shared base contract for all runtime plugins."""

    @property
    @abstractmethod
    def kind(self) -> PluginKind:
        """Return the runtime category for this plugin."""

    @abstractmethod
    def name(self) -> str:
        """Return a human-readable name for this plugin."""

    def is_enabled(self) -> bool:
        """Return whether this plugin should be active in the current runtime."""
        return True

    def on_registered(self) -> None:
        """Called when the plugin is registered with the runtime."""
        return

    def on_unregistered(self) -> None:
        """Called when the plugin is unregistered or the runtime is stopping."""
        return


class ListenerPlugin(Plugin):
    """Protocol for plugins that handle database listener callbacks.

    Listener plugins are responsible for reacting to database changes. They may
    register themselves with the event queue and process documents as they are
    created or modified.

    Design Notes:
    - Listeners should be idempotent: processing the same document multiple times
      should not cause duplicate side effects.
    - Listeners may cache database state for performance, but must follow the
      database-derived stateful plugin principle (see module docstring).
    - Listeners must raise only exceptions derived from BasePluginException.
    - Listeners should not maintain state critical to system correctness;
      all critical state must live in the database.
    """

    @property
    def kind(self) -> PluginKind:
        return PluginKind.LISTENER

    @abstractmethod
    def build_manager(self) -> AbstractContextManager[object]:
        """Build the context manager that attaches listener resources.

        The manager is entered while the runtime is active and exited on
        shutdown. Plugins should use this for Firestore watches and any
        related resources.
        """
        ...


class HousekeepingPlugin(Plugin):
    """Protocol for plugins that perform periodic maintenance tasks.

    Housekeeping plugins are called on a regular schedule (currently a central
    tick) and perform background work like cleanup, data migration, or metric
    aggregation.

    Design Notes:
    - Housekeeping plugins should be idempotent: running the same task multiple
      times (even concurrently, in the future) should be safe.
    - Plugins may cache database state for performance, but must follow the
      database-derived stateful plugin principle (see module docstring).
    - Plugins must raise only exceptions derived from BasePluginException.
    - Plugins should not maintain state critical to system correctness;
      all critical state must live in the database.

    Future Enhancement:
    - Individual housekeeping plugins may declare their own schedule (interval,
      cron expression, etc.) rather than all using the central tick.
    """

    @property
    def kind(self) -> PluginKind:
        return PluginKind.HOUSEKEEPING

    @abstractmethod
    def run(self) -> None:
        """Execute one cycle of the housekeeping task.

        This is called on each central tick (or at the plugin's own schedule,
        in the future). Should be idempotent and handle partial state gracefully.

        May raise BasePluginException if an error occurs. The system will
        terminate, but the exception type indicates whether this was an
        expected or unexpected error.
        """
        ...


class PollStatusQueryDbHandler(Protocol):
    """Shared protocol for poll status query support."""

    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query: ...


class NewPollDbHandler(PollStatusQueryDbHandler, Protocol):
    """Protocol for the db-handler capabilities needed by NewPollListenerPlugin."""

    def new_poll_event_handler(
        self,
        am: "ActionMan",
        poll_id: PollId,
    ) -> None: ...


class CompletePollDbHandler(PollStatusQueryDbHandler, Protocol):
    """Protocol for db-handler capabilities needed by CompletePollListenerPlugin."""

    def complete_poll_event_handler(
        self,
        pubs_list: "PubsList",
        am: "ActionMan",
        poll_id: PollId,
    ) -> None: ...


@runtime_checkable
class PollListenerDbHandler(NewPollDbHandler, CompletePollDbHandler, Protocol):
    """Combined db-handler contract for poll listener plugin configuration."""

    pass
