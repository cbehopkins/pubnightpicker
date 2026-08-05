from types import SimpleNamespace
from typing import cast

import pytest
from google.cloud.firestore_v1.query import Query

from firebase_sub.event import EventType
from firebase_sub.plugins.complete_poll import CompletePollListenerPlugin
from firebase_sub.plugins.new_poll import NewPollListenerPlugin
from firebase_sub.plugins.plugin_config import build_event_registry
from firebase_sub.plugins.protocols import EventPlugin
from firebase_sub.runtime.idempotency import IdempotencyMetadata, IdempotencyStore


class _FakePollDbHandler:
    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query:
        del completed, min_date
        return cast(Query, SimpleNamespace(on_snapshot=lambda _cb: None))


class _UnknownEventPlugin(EventPlugin):
    def name(self) -> str:
        return "unknown"

    def filter(self, envelope):
        del envelope
        return False

    def handle(self, envelope) -> None:
        del envelope

    def mark_done(self, envelope) -> None:
        del envelope


class _FakeActionExecutor:
    def action_event(self, *args, **kwargs):
        del args, kwargs
        return {}


class _FakeIdempotencyStore(IdempotencyStore):
    def is_done(self, *, entity_id: str, dedupe_key: str) -> bool:
        del entity_id, dedupe_key
        return False

    def mark_done(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata | None = None,
    ) -> None:
        del entity_id, dedupe_key, metadata

    def mark_retryable_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        del entity_id, dedupe_key, metadata

    def mark_terminal_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        del entity_id, dedupe_key, metadata


def test_build_event_registry_routes_new_and_complete_poll_plugins() -> None:
    db_handler = _FakePollDbHandler()
    action_executor = _FakeActionExecutor()
    store = _FakeIdempotencyStore()
    new_plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        idempotency_store=store,
    )
    complete_plugin = CompletePollListenerPlugin(
        db_handler=db_handler,
        action_executor=action_executor,
        max_retries=1,
        retry_delay_seconds=0.0,
        idempotency_store=store,
    )

    registry = build_event_registry(event_plugins=[new_plugin, complete_plugin])

    assert list(registry.get_plugins(EventType.NEW_POLL)) == [new_plugin]
    assert list(registry.get_plugins(EventType.COMP_POLL)) == [complete_plugin]


def test_build_event_registry_raises_for_unknown_event_plugin_type() -> None:
    with pytest.raises(ValueError, match="Unsupported EventPlugin registration"):
        build_event_registry(event_plugins=[_UnknownEventPlugin()])
