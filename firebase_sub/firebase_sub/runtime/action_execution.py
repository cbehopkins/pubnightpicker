"""Action execution contracts and adapters.

This module keeps listener-facing execution APIs independent from ActionMan
while allowing incremental migration by adapting existing ActionMan instances.
"""

from collections.abc import Mapping
from typing import Any, Protocol

from firebase_sub.action_track import ActionMan


class PollActionExecutor(Protocol):
    """Execution contract for poll action side effects."""

    def action_event(self, *args: Any, **kwargs: Any) -> Mapping[str, set[str]] | None:
        """Execute bound actions and return updated action mapping when changed."""
        ...


class ActionManPollActionExecutor(PollActionExecutor):
    """PollActionExecutor adapter backed by an ActionMan instance."""

    def __init__(self, action_manager: ActionMan) -> None:
        self._action_manager = action_manager

    def action_event(self, *args: Any, **kwargs: Any) -> Mapping[str, set[str]] | None:
        return self._action_manager.action_event(*args, **kwargs)
