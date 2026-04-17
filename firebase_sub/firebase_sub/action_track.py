import logging
from collections import defaultdict
from typing import Callable

from firebase_sub.my_types import ActionDict, ActionType, DocumentId

_log = logging.getLogger("ActionTrack")
from typing import Any, Protocol


class ActionCallbackProtocol(Protocol):
    def __call__(
        self, *args: Any, previously_actioned: bool, dummy_run: bool, **kwargs: Any
    ) -> None: ...


class _CallbackException(Exception): ...


class CallbackExceptionIgnore(_CallbackException):
    """An exception in the callback to ignore - mark the action as happened"""


class CallbackExceptionRetry(_CallbackException):
    """An exception in the callback to retry later - leave the action unhappened"""


class ActionTrack(defaultdict[str, set[DocumentId]]):
    def __init__(self, obj=None):
        super().__init__(set)
        if obj is None:
            return
        for at, pub_id_list in obj.items():
            self[str(at)] = set(pub_id_list)

    def needs_action(self, at: ActionType, action_key: DocumentId) -> bool:
        """Return whether this specific action key has not been actioned yet."""
        return action_key not in self.get(str(at), set())

    def has_any_actioned(self, at: ActionType) -> bool:
        """Return whether this action type has ever been actioned."""
        return str(at) in self

    def mark_actioned(self, at: ActionType, action_key: DocumentId) -> None:
        self[str(at)].add(action_key)

    def to_action(self, at: ActionType, action_key: DocumentId) -> bool:
        return self.needs_action(at, action_key)

    def previously_actioned(self, at: ActionType):
        return self.has_any_actioned(at)

    def action(self, at: ActionType, action_key: DocumentId) -> None:
        self.mark_actioned(at, action_key)

    @property
    def as_dict(self) -> dict[str, set[DocumentId]]:
        return dict(self)


class ActionMan:

    def __init__(self, dummy_run: bool = False):
        self._callbacks: dict[ActionType, ActionCallbackProtocol] = {}
        self._dummy_run_overrides: dict[ActionType, bool] = {}
        self.dummy_run = dummy_run

    def bind(
        self,
        action: ActionType,
        callback: ActionCallbackProtocol,
        *,
        dummy_run: bool | None = None,
    ):
        """Bind an action type against callbacks"""
        self._callbacks[action] = callback
        if dummy_run is None:
            self._dummy_run_overrides.pop(action, None)
        else:
            self._dummy_run_overrides[action] = dummy_run

    def run(
        self, action_dict: ActionDict, action_key: DocumentId, *args, **kwargs
    ) -> tuple[ActionDict, bool]:
        """Run all pending action callbacks"""
        ad = ActionTrack(action_dict)
        anything_actioned = False
        for action_type, callback in self._callbacks.items():
            previously_actioned = ad.has_any_actioned(action_type)
            if ad.needs_action(action_type, action_key):
                anything_actioned = True
                callback_dummy_run = self._dummy_run_overrides.get(
                    action_type, self.dummy_run
                )
                try:
                    callback(
                        *args,
                        previously_actioned=previously_actioned,
                        dummy_run=callback_dummy_run,
                        **kwargs,
                    )
                    ad.mark_actioned(action_type, action_key=action_key)
                except CallbackExceptionIgnore as exc:
                    _log.exception(
                        f"got an ignorable exception running {action_type}:{exc}"
                    )
                    ad.mark_actioned(action_type, action_key=action_key)
                except CallbackExceptionRetry as exc:
                    _log.exception(
                        f"got an retry exception running {action_type}:{exc}"
                    )
        return ad, anything_actioned

    def action_event(self, *args, **kwargs):
        new_action_dict, actioned = self.run(*args, **kwargs)
        if actioned:
            return new_action_dict
        return None
