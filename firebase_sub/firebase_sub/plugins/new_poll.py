from contextlib import AbstractContextManager

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.query import Query

from firebase_sub.database.poll_manager import PollManager
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType
from firebase_sub.action_track import ActionMan
from firebase_sub.plugins.protocols import (
    ListenerPlugin,
    NewPollDbHandler,
)
from firebase_sub.runtime.job_queue import JobQueue


class NewPollListenerPlugin(ListenerPlugin):
    """Listener plugin that enqueues NEW_POLL events for open polls."""

    def __init__(
        self,
        *,
        db_handler: NewPollDbHandler,
        event_queue: JobQueue[Event],
        action_manager: ActionMan,
        min_date: str | None,
    ) -> None:
        self._db_handler = db_handler
        self._event_queue = event_queue
        self._action_manager = action_manager
        self._min_date = min_date

    def name(self) -> str:
        return "new_poll_listener"

    def on_registered(self) -> None:
        return

    def on_unregistered(self) -> None:
        return

    def build_manager(self) -> AbstractContextManager[object]:
        poll_query = self._db_handler.query_polls_by_status(
            completed=False,
            min_date=self._min_date,
        )
        return PollManager(
            query=poll_query,
            add=self._open_poll_event_callback,
        )

    def _new_poll_handler(
        self,
        document: DocumentSnapshot | None,
        pubs_list: PubsList,
    ) -> None:
        del pubs_list
        if document is None:
            raise ValueError(
                "New Event has no document. This indicates a coding error."
            )
        self._db_handler.new_poll_event_handler(
            self._action_manager, poll_id=document.id
        )

    def _open_poll_event_callback(self, document: DocumentSnapshot) -> None:
        self._event_queue.put(
            Event(
                type=EventType.NEW_POLL, doc=document, callback=self._new_poll_handler
            )
        )
