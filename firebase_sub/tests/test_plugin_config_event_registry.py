from contextlib import nullcontext
from types import SimpleNamespace
from typing import cast

import pytest
from google.cloud.firestore_v1.query import Query

from firebase_sub.action_track import ActionMan
from firebase_sub.event import EventType
from firebase_sub.plugins.complete_poll import CompletePollListenerPlugin
from firebase_sub.plugins.new_poll import NewPollListenerPlugin
from firebase_sub.plugins.plugin_config import build_event_registry
from firebase_sub.plugins.protocols import EventPlugin


class _FakePollDbHandler:
    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query:
        del completed, min_date
        return cast(Query, SimpleNamespace(on_snapshot=lambda _cb: None))

    def new_poll_event_handler(self, am: ActionMan, poll_id: str) -> None:
        del am, poll_id

    def complete_poll_event_handler(
        self, pubs_list, am: ActionMan, poll_id: str
    ) -> None:
        del pubs_list, am, poll_id


class _UnknownEventPlugin(EventPlugin):
    def name(self) -> str:
        return "unknown"

    def build_manager(self):
        return nullcontext()

    def filter(self, envelope):
        del envelope
        return False

    def handle(self, envelope) -> None:
        del envelope

    def mark_done(self, envelope) -> None:
        del envelope


def test_build_event_registry_routes_new_and_complete_poll_plugins() -> None:
    db_handler = _FakePollDbHandler()
    new_plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_manager=ActionMan(),
    )
    complete_plugin = CompletePollListenerPlugin(
        db_handler=db_handler,
        action_manager=ActionMan(),
        max_retries=1,
        retry_delay_seconds=0.0,
    )

    registry = build_event_registry(event_plugins=[new_plugin, complete_plugin])

    assert list(registry.get_plugins(EventType.NEW_POLL)) == [new_plugin]
    assert list(registry.get_plugins(EventType.COMP_POLL)) == [complete_plugin]


def test_build_event_registry_raises_for_unknown_event_plugin_type() -> None:
    with pytest.raises(ValueError, match="Unsupported EventPlugin registration"):
        build_event_registry(event_plugins=[_UnknownEventPlugin()])
