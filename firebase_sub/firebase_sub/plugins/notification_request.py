from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.runtime.service_adapter import (
    AdapterBackedEventPlugin,
    ServiceAdapter,
    ServiceContext,
    ServiceResult,
)


class _NotificationRequestServiceAdapter(ServiceAdapter):
    """Adapter implementation for notification request processing."""

    def __init__(
        self,
        *,
        notification_mirror: NotificationAckMirrorHandler,
        notification_push_test: NotificationPushTestHandler,
    ) -> None:
        self._notification_mirror = notification_mirror
        self._notification_push_test = notification_push_test

    def name(self) -> str:
        return "notification_request_listener"

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        if envelope.doc is None:
            return None
        if envelope.type == EventType.PUSH_TEST:
            if not self._notification_push_test.is_push_test_request(envelope.doc):
                return None
        elif envelope.type == EventType.PUSH:
            if self._notification_push_test.is_push_test_request(envelope.doc):
                return None
        else:
            return None

        return ServiceContext(
            envelope=envelope,
            entity_id=envelope.document_id() or "",
            dedupe_key=f"notification:{envelope.document_id()}",
        )

    def execute(self, context: ServiceContext) -> ServiceResult:
        envelope = context.envelope
        if envelope.doc is None:
            return ServiceResult(success=True)
        if envelope.type == EventType.PUSH_TEST:
            # The push-test handler persists both ack state and request cleanup inline.
            self._notification_push_test.handle_request_document(envelope.doc)
            return ServiceResult(success=True)
        if envelope.type == EventType.PUSH:
            # The mirror handler persists the ack document inline.
            self._notification_mirror.mirror_request_document(envelope.doc)
            return ServiceResult(success=True)
        return ServiceResult(success=True)

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        # Notification handlers persist their own completion/ack state inline.
        del context, result
        return


class NotificationRequestListenerPlugin(AdapterBackedEventPlugin):
    """Listener plugin for notification request documents.

    Completion state is written inline by the notification handlers themselves.
    ``mark_done()`` remains as the lifecycle acknowledgment hook and is a no-op.
    """

    def __init__(
        self,
        *,
        notification_mirror: NotificationAckMirrorHandler,
        notification_push_test: NotificationPushTestHandler,
    ) -> None:
        self._notification_adapter = _NotificationRequestServiceAdapter(
            notification_mirror=notification_mirror,
            notification_push_test=notification_push_test,
        )
        super().__init__(self._notification_adapter)
        self._notification_mirror = notification_mirror
        self._notification_push_test = notification_push_test
