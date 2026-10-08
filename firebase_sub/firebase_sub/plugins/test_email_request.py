from typing import Protocol

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import EventPlugin


class TestEmailRequestHandlerProtocol(Protocol):
    def handle_request_document(self, document: DocumentSnapshot | None) -> None: ...


class TestEmailRequestListenerPlugin(EventPlugin):
    """Listener plugin for users/{uid}.testEmailReq; the handler writes the ack inline."""

    __test__ = False

    def __init__(self, *, handler: TestEmailRequestHandlerProtocol) -> None:
        self._handler = handler

    def name(self) -> str:
        return "test_email_request_listener"

    def filter(self, envelope: EventEnvelope) -> bool:
        return (
            envelope.type == EventType.TEST_EMAIL_REQUEST and envelope.doc is not None
        )

    def handle(self, envelope: EventEnvelope) -> None:
        self._handler.handle_request_document(envelope.doc)

    def mark_done(self, envelope: EventEnvelope) -> None:
        del envelope
