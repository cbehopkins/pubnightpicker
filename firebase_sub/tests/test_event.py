from types import SimpleNamespace
from typing import cast
from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import Event, EventEnvelope, EventType


def test_event_stores_type_and_document() -> None:
    doc = cast(DocumentSnapshot, SimpleNamespace(id="poll-1"))
    event = Event(type=EventType.NEW_POLL, doc=doc)

    assert event.type == EventType.NEW_POLL
    assert event.doc is doc


def test_event_allows_none_document() -> None:
    event = Event(type=EventType.TICK, doc=None)

    assert event.type == EventType.TICK
    assert event.doc is None


def test_event_envelope_document_id_returns_doc_id() -> None:
    doc = cast(DocumentSnapshot, SimpleNamespace(id="poll-2"))
    envelope = EventEnvelope(type=EventType.NEW_POLL, doc=doc)

    assert envelope.document_id() == "poll-2"


def test_event_envelope_document_id_returns_none_without_doc() -> None:
    envelope = EventEnvelope(type=EventType.TICK, doc=None)

    assert envelope.document_id() is None
