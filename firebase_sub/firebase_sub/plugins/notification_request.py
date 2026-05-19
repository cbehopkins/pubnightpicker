from contextlib import AbstractContextManager
from collections.abc import Callable

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.query import Query

from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.event import Event, EventType
from firebase_sub.plugins.protocols import ListenerPlugin
from firebase_sub.runtime.job_queue import JobQueue


class NotificationRequestListenerPlugin(ListenerPlugin):
    """Listener plugin for notification request documents."""

    def __init__(
        self,
        *,
        query_notification_requests: Query | Callable[[], Query],
        event_queue: JobQueue[Event],
        notification_mirror: NotificationAckMirrorHandler,
        notification_push_test: NotificationPushTestHandler,
    ) -> None:
        self._query_notification_requests = query_notification_requests
        self._event_queue = event_queue
        self._notification_mirror = notification_mirror
        self._notification_push_test = notification_push_test

    def name(self) -> str:
        return "notification_request_listener"

    def build_manager(self) -> AbstractContextManager[object]:
        query = (
            self._query_notification_requests()
            if callable(self._query_notification_requests)
            else self._query_notification_requests
        )
        return PollManager(
            query=query,
            add=self._notification_request_callback,
            modify=self._notification_request_callback,
        )

    def _notification_request_callback(self, document: DocumentSnapshot) -> None:
        if self._notification_push_test.is_push_test_request(document):
            self._event_queue.put(
                Event(
                    type=EventType.PUSH_TEST,
                    doc=document,
                    callback=self._notification_push_test.handle,
                )
            )
            return

        self._event_queue.put(
            Event(
                type=EventType.PUSH,
                doc=document,
                callback=self._notification_mirror.handle,
            )
        )
