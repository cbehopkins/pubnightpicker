from types import SimpleNamespace

import pytest

from firebase_sub.database.handlers import RetryablePollDataNotReadyError
from firebase_sub.event import Event, EventType


class _FakeDbHandler:
    def __init__(self, *, complete_failures_before_success: int = 0):
        self.new_calls = []
        self.complete_calls = []
        self._remaining_complete_failures = complete_failures_before_success

    def new_poll_event_handler(self, am, poll_id):
        self.new_calls.append((am, poll_id))

    def complete_poll_event_handler(self, pubs_list, am, poll_id):
        if self._remaining_complete_failures > 0:
            self._remaining_complete_failures -= 1
            raise RetryablePollDataNotReadyError("pubs list not ready")
        self.complete_calls.append((pubs_list, am, poll_id))


def test_handle_queue_item_new_poll_routes_to_new_handler():
    handler = _FakeDbHandler()
    open_am = object()
    complete_am = object()
    pubs_list = object()
    doc = SimpleNamespace(id="poll-1")

    event = Event(type=EventType.NEW_POLL, doc=doc)

    event.handle_queue_item(handler, pubs_list, open_am, complete_am)

    assert handler.new_calls == [(open_am, "poll-1")]
    assert handler.complete_calls == []


def test_handle_queue_item_complete_poll_routes_to_complete_handler():
    handler = _FakeDbHandler()
    open_am = object()
    complete_am = object()
    pubs_list = object()
    doc = SimpleNamespace(id="poll-2")

    event = Event(type=EventType.COMP_POLL, doc=doc)

    event.handle_queue_item(handler, pubs_list, open_am, complete_am)

    assert handler.new_calls == []
    assert handler.complete_calls == [(pubs_list, complete_am, "poll-2")]


def test_handle_queue_item_complete_poll_retries_then_succeeds(monkeypatch):
    monkeypatch.setattr("firebase_sub.event.time.sleep", lambda _seconds: None)
    handler = _FakeDbHandler(complete_failures_before_success=2)
    open_am = object()
    complete_am = object()
    pubs_list = object()
    doc = SimpleNamespace(id="poll-retry")

    event = Event(type=EventType.COMP_POLL, doc=doc)

    event.handle_queue_item(handler, pubs_list, open_am, complete_am)

    assert handler.complete_calls == [(pubs_list, complete_am, "poll-retry")]


def test_handle_queue_item_complete_poll_raises_after_max_retries(monkeypatch):
    monkeypatch.setattr("firebase_sub.event.time.sleep", lambda _seconds: None)
    handler = _FakeDbHandler(complete_failures_before_success=10)
    event = Event(type=EventType.COMP_POLL, doc=SimpleNamespace(id="poll-fail"))

    with pytest.raises(RetryablePollDataNotReadyError):
        event.handle_queue_item(handler, object(), object(), object())


def test_handle_queue_item_raises_on_missing_document():
    handler = _FakeDbHandler()

    event = Event(type=EventType.NEW_POLL, doc=None)

    with pytest.raises(ValueError, match="Event has no document"):
        event.handle_queue_item(handler, object(), object(), object())


def test_handle_queue_item_housekeeping_tick_routes_to_callback():
    handler = _FakeDbHandler()
    called = {"count": 0}

    def housekeeping_callback():
        called["count"] += 1

    event = Event(
        type=EventType.TICK,
        doc=None,
        callback=housekeeping_callback,
    )

    event.handle_queue_item(
        handler,
        object(),
        object(),
        object(),
    )

    assert called["count"] == 1


def test_handle_queue_item_housekeeping_tick_requires_callback():
    handler = _FakeDbHandler()
    event = Event(type=EventType.TICK, doc=None)

    with pytest.raises(ValueError, match="Tick event requires callback."):
        event.handle_queue_item(handler, object(), object(), object())
