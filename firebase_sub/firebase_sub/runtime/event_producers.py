"""Event producers handle database queries and create events for the queue.

Event producers decouple database queries from plugins. They are responsible for:
1. Setting up Firestore watches via PollManager
2. Querying the database for changes
3. Creating Event objects and placing them on the event queue

Plugins then consume these events via the event registry (filter -> handle -> mark_done).
This ensures the query -> event -> queue -> plugin flow is decoupled.
"""

from contextlib import AbstractContextManager, nullcontext

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.event import Event, EventType
from firebase_sub.plugins.protocols import CompletePollDbHandler, NewPollDbHandler
from firebase_sub.runtime.job_queue import JobQueue
from firebase_sub.database.handlers import DbHandler


class EventProducer:
    """Produces events from Firestore queries and places them on the event queue."""

    def __init__(
        self,
        db_handler: DbHandler,
        event_queue: JobQueue[Event],
        notification_push_test: NotificationPushTestHandler | None = None,
        new_poll_db_handler: NewPollDbHandler | None = None,
        complete_poll_db_handler: CompletePollDbHandler | None = None,
        min_date: str | None = None,
    ) -> None:
        self.db_handler = db_handler
        self.event_queue = event_queue
        self.notification_push_test = notification_push_test
        self.new_poll_db_handler = new_poll_db_handler
        self.complete_poll_db_handler = complete_poll_db_handler
        self.min_date = min_date

    def build_chat_message_manager(self) -> AbstractContextManager[object]:
        """Build manager for chat message watch."""
        return PollManager(
            query=self.db_handler.query_messages,
            add=self._chat_message_callback,
        )

    def build_notification_request_manager(self) -> AbstractContextManager[object]:
        """Build manager for notification request watch."""
        return PollManager(
            query=self.db_handler.query_notification_requests,
            add=self._notification_request_callback,
            modify=self._notification_request_callback,
        )

    def build_admin_delete_request_manager(self) -> AbstractContextManager[object]:
        """Build manager for admin delete request watch."""
        return PollManager(
            query=self.db_handler.query_admin_delete_requests,
            add=self._admin_delete_request_callback,
            modify=self._admin_delete_request_callback,
        )

    def _chat_message_callback(self, document: DocumentSnapshot) -> None:
        """Called when a chat message document is created or updated."""
        event = Event(
            type=EventType.CHAT_MESSAGE,
            doc=document,
        )
        self.event_queue.put(event)

    def _notification_request_callback(self, document: DocumentSnapshot) -> None:
        """Called when a notification request document is created or updated."""
        # Determine event type based on whether it's a push test request
        event_type = EventType.PUSH_TEST
        if (
            self.notification_push_test
            and not self.notification_push_test.is_push_test_request(document)
        ):
            event_type = EventType.PUSH

        event = Event(
            type=event_type,
            doc=document,
        )
        self.event_queue.put(event)

    def _admin_delete_request_callback(self, document: DocumentSnapshot) -> None:
        """Called when an admin delete request document is created or updated."""
        event = Event(
            type=EventType.ADMIN_DELETE_REQUEST,
            doc=document,
        )
        self.event_queue.put(event)

    def build_new_poll_manager(self) -> AbstractContextManager[object]:
        """Build manager for new poll watch."""
        if not self.new_poll_db_handler:
            return nullcontext()

        return PollManager(
            query=self.new_poll_db_handler.query_polls_by_status(
                completed=False,
                min_date=self.min_date,
            ),
            add=self._new_poll_callback,
        )

    def build_complete_poll_manager(self) -> AbstractContextManager[object]:
        """Build manager for complete poll watch."""
        if not self.complete_poll_db_handler:
            return nullcontext()

        return PollManager(
            query=self.complete_poll_db_handler.query_polls_by_status(
                completed=True,
                min_date=self.min_date,
            ),
            add=self._complete_poll_callback,
            modify=self._complete_poll_callback,
        )

    def _new_poll_callback(self, document: DocumentSnapshot) -> None:
        """Called when a new (open) poll document is created."""
        event = Event(
            type=EventType.NEW_POLL,
            doc=document,
        )
        self.event_queue.put(event)

    def _complete_poll_callback(self, document: DocumentSnapshot) -> None:
        """Called when a completed poll document is created or updated."""
        event = Event(
            type=EventType.COMP_POLL,
            doc=document,
        )
        self.event_queue.put(event)
