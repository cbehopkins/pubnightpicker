from contextlib import AbstractContextManager
from typing import Protocol

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import EventPlugin


class AdminDeleteRequestHandlerProtocol(Protocol):
    enabled: bool

    def handle_request_document(self, document: DocumentSnapshot | None) -> None: ...


class AdminDeleteRequestListenerPlugin(EventPlugin):
    """Listener plugin for admin delete request documents."""

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

    def build_manager(self) -> AbstractContextManager[object]:
        """Events are now produced externally by event producers."""
        # No-op manager since Firestore watches are managed by event producers
        from contextlib import nullcontext

        return nullcontext()

    def filter(self, envelope: EventEnvelope) -> bool:
        return (
            envelope.type == EventType.ADMIN_DELETE_REQUEST
            and envelope.doc is not None
            and self._handler.enabled
        )

    def handle(self, envelope: EventEnvelope) -> None:
        if envelope.doc is None:
            return
        self._handler.handle_request_document(envelope.doc)

    def mark_done(self, envelope: EventEnvelope) -> None:
        # The admin delete handler persists status transitions internally.
        del envelope
        return
