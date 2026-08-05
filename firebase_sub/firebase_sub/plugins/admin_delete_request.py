from typing import Protocol

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.runtime.service_adapter import (
    AdapterBackedEventPlugin,
    ServiceAdapter,
    ServiceContext,
    ServiceResult,
)


class AdminDeleteRequestHandlerProtocol(Protocol):
    enabled: bool

    def handle_request_document(self, document: DocumentSnapshot | None) -> None: ...


class _AdminDeleteRequestServiceAdapter(ServiceAdapter):
    """Adapter implementation for admin delete request processing."""

    def __init__(self, *, handler: AdminDeleteRequestHandlerProtocol) -> None:
        self._handler = handler

    def name(self) -> str:
        return "admin_delete_request_listener"

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        if (
            envelope.type != EventType.ADMIN_DELETE_REQUEST
            or envelope.doc is None
            or not self._handler.enabled
        ):
            return None
        request_id = envelope.document_id()
        if request_id is None:
            return None
        return ServiceContext(
            envelope=envelope,
            entity_id=request_id,
            dedupe_key=f"admin_delete_request:{request_id}",
        )

    def execute(self, context: ServiceContext) -> ServiceResult:
        envelope = context.envelope
        if envelope.doc is None:
            return ServiceResult(success=True)
        # The handler persists the request status transitions inline.
        self._handler.handle_request_document(envelope.doc)
        return ServiceResult(success=True)

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        # The admin delete handler persists status transitions inline.
        del context, result
        return


class AdminDeleteRequestListenerPlugin(AdapterBackedEventPlugin):
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
        self._admin_delete_adapter = _AdminDeleteRequestServiceAdapter(handler=handler)
        super().__init__(self._admin_delete_adapter)
        self._handler = handler

    def name(self) -> str:
        return "admin_delete_request_listener"

    def is_enabled(self) -> bool:
        return self._handler.enabled
