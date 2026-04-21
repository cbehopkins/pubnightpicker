from types import SimpleNamespace
from typing import Any, cast

import pytest
from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType


def test_handle_queue_item_calls_callback_with_doc_and_pubs_list():
    calls = []
    doc = cast(DocumentSnapshot, SimpleNamespace(id="poll-1"))
    pubs_list = cast(PubsList, object())

    def callback(document, supplied_pubs_list):
        calls.append((document, supplied_pubs_list))

    event = Event(type=EventType.NEW_POLL, doc=doc, callback=callback)

    event.handle_queue_item(pubs_list)

    assert calls == [(doc, pubs_list)]


def test_handle_queue_item_passes_none_doc_to_callback():
    calls = []
    pubs_list = cast(PubsList, object())

    def callback(document, supplied_pubs_list):
        calls.append((document, supplied_pubs_list))

    event = Event(type=EventType.TICK, doc=None, callback=callback)

    event.handle_queue_item(pubs_list)

    assert calls == [(None, pubs_list)]


def test_event_requires_callback_at_construction():
    kwargs: Any = {"type": EventType.TICK, "doc": None}
    with pytest.raises(TypeError, match="callback"):
        Event(**kwargs)
