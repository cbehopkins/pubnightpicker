import logging
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


class ActionTrack(dict[str, set[DocumentId]]):
    def __init__(self, obj=None):
        super().__init__()
        if obj is None:
            return
        for at, pub_id_list in obj.items():
            self[at] = set(pub_id_list)

    def to_action(self, at: ActionType, action_key: DocumentId) -> bool:
        # FIXME this must be possible with a defaultdict
        already_actioned = self.get(str(at), set())
        return action_key not in already_actioned

    def previously_actioned(self, at: ActionType):
        return str(at) in self

    def action(self, at: ActionType, action_key: DocumentId) -> None:
        current = self.get(str(at), set())
        current.add(action_key)
        self[str(at)] = current

    @property
    def as_dict(self) -> dict:
        return dict(self)


class ActionMan:

    def __init__(self, dummy_run: bool = False):
        self._callbacks: dict[ActionType, ActionCallbackProtocol] = {}
        self.dummy_run = dummy_run

    def bind(self, action: ActionType, callback: ActionCallbackProtocol):
        """Bind an action type against callbacks"""
        self._callbacks[action] = callback

    def run(
        self, action_dict: ActionDict, action_key: DocumentId, *args, **kwargs
    ) -> tuple[ActionDict, bool]:
        """Run all pending action callbacks"""
        ad = ActionTrack(action_dict)
        anything_actioned = False
        for action_type, callback in self._callbacks.items():
            previously_actioned = ad.previously_actioned(action_type)
            if ad.to_action(action_type, action_key):
                anything_actioned = True
                try:
                    callback(
                        *args,
                        previously_actioned=previously_actioned,
                        dummy_run=self.dummy_run,
                        **kwargs,
                    )
                    ad.action(action_type, action_key=action_key)
                except CallbackExceptionIgnore as exc:
                    _log.exception(
                        f"got an ignorable exception running {action_type}:{exc}"
                    )
                    ad.action(action_type, action_key=action_key)
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
