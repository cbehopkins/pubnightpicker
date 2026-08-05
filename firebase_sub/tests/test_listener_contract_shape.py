import inspect

import firebase_sub.plugins.complete_poll as complete_poll_module
import firebase_sub.plugins.new_poll as new_poll_module
from firebase_sub.plugins.complete_poll import CompletePollListenerPlugin
from firebase_sub.plugins.new_poll import NewPollListenerPlugin


def test_new_poll_listener_requires_executor_and_idempotency_store() -> None:
    signature = inspect.signature(NewPollListenerPlugin.__init__)

    assert "action_executor" in signature.parameters
    assert "idempotency_store" in signature.parameters
    assert signature.parameters["action_executor"].default is inspect._empty
    assert signature.parameters["idempotency_store"].default is inspect._empty


def test_complete_poll_listener_requires_executor_and_idempotency_store() -> None:
    signature = inspect.signature(CompletePollListenerPlugin.__init__)

    assert "action_executor" in signature.parameters
    assert "idempotency_store" in signature.parameters
    assert signature.parameters["action_executor"].default is inspect._empty
    assert signature.parameters["idempotency_store"].default is inspect._empty


def test_poll_listener_modules_do_not_directly_import_actionman() -> None:
    new_poll_source = inspect.getsource(new_poll_module)
    complete_poll_source = inspect.getsource(complete_poll_module)

    assert "from firebase_sub.action_track import ActionMan" not in new_poll_source
    assert "from firebase_sub.action_track import ActionMan" not in complete_poll_source
