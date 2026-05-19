from contextlib import AbstractContextManager
from collections.abc import Callable

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.query import Query

from firebase_sub.database.admin_delete_requests import AdminDeleteRequestHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.event import Event, EventType
from firebase_sub.plugins.protocols import ListenerPlugin
from firebase_sub.runtime.job_queue import JobQueue


class AdminDeleteRequestListenerPlugin(ListenerPlugin):
    """Listener plugin for admin delete request documents."""

    def __init__(
        self,
        *,
        query_admin_delete_requests: Query | Callable[[], Query],
        event_queue: JobQueue[Event],
        handler: AdminDeleteRequestHandler,
    ) -> None:
        self._query_admin_delete_requests = query_admin_delete_requests
        self._event_queue = event_queue
        self._handler = handler

    def name(self) -> str:
        return "admin_delete_request_listener"

    def is_enabled(self) -> bool:
        return self._handler.enabled

    def build_manager(self) -> AbstractContextManager[object]:
        query = (
            self._query_admin_delete_requests()
            if callable(self._query_admin_delete_requests)
            else self._query_admin_delete_requests
        )
        return PollManager(
            query=query,
            add=self._admin_delete_request_callback,
            modify=self._admin_delete_request_callback,
        )

    def _admin_delete_request_callback(self, document: DocumentSnapshot) -> None:
        self._event_queue.put(
            Event(
                type=EventType.ADMIN_DELETE_REQUEST,
                doc=document,
                callback=self._handler.handle,
            )
        )
