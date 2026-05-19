from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import EventPlugin


class NotificationRequestListenerPlugin(EventPlugin):
    """Listener plugin for notification request documents."""

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

    def filter(self, envelope: EventEnvelope) -> bool:
        if envelope.doc is None:
            return False
        if envelope.type == EventType.PUSH_TEST:
            return self._notification_push_test.is_push_test_request(envelope.doc)
        if envelope.type == EventType.PUSH:
            return not self._notification_push_test.is_push_test_request(envelope.doc)
        return False

    def handle(self, envelope: EventEnvelope) -> None:
        if envelope.doc is None:
            return
        if envelope.type == EventType.PUSH_TEST:
            self._notification_push_test.handle_request_document(envelope.doc)
            return
        if envelope.type == EventType.PUSH:
            self._notification_mirror.mirror_request_document(envelope.doc)

    def mark_done(self, envelope: EventEnvelope) -> None:
        # Notification handlers persist their own completion/ack state.
        del envelope
        return
