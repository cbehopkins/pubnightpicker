"""
Plugin system for event listeners and housekeeping tasks.

This package provides the plugin-based architecture for registering and managing
database listeners and background tasks. See protocols.py for detailed contracts.
"""

from firebase_sub.plugins.protocols import (
    BasePluginException,
    HousekeepingPlugin,
    ListenerPlugin,
    ManagedListenerPlugin,
    PlannedPluginException,
    UnexpectedPluginException,
)

__all__ = [
    "BasePluginException",
    "PlannedPluginException",
    "UnexpectedPluginException",
    "ListenerPlugin",
    "ManagedListenerPlugin",
    "HousekeepingPlugin",
]
