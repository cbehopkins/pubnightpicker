from typing import Protocol

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import EventPlugin


class AdminDeleteRequestHandlerProtocol(Protocol):
    enabled: bool

    def handle_request_document(self, document: DocumentSnapshot | None) -> None: ...


class AdminDeleteRequestListenerPlugin(EventPlugin):
    """Listener plugin for admin delete request documents.

    The admin-delete handler owns the request state machine and persists status
    transitions inline. ``mark_done()`` remains as the lifecycle acknowledgment
    hook and is a no-op.
    """

    def __init__(
        self,
        *,
        handler: AdminDeleteRequestHandlerProtocol,
    ) -> None:
        self._handler = handler

    def name(self) -> str:
        return "admin_delete_request_listener"

    def is_enabled(self) -> bool:
        return self._handler.enabled

    def filter(self, envelope: EventEnvelope) -> bool:
        return (
            envelope.type == EventType.ADMIN_DELETE_REQUEST
            and envelope.doc is not None
            and self._handler.enabled
        )

    def handle(self, envelope: EventEnvelope) -> None:
        if envelope.doc is None:
            return
        # The handler persists the request status transitions inline.
        self._handler.handle_request_document(envelope.doc)

    def mark_done(self, envelope: EventEnvelope) -> None:
        # The admin delete handler persists status transitions inline.
        del envelope
        return
