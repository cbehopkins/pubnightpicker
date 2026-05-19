from contextlib import AbstractContextManager

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.common.retry import retry
from firebase_sub.database.handlers import RetryablePollDataNotReadyError
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType
from firebase_sub.action_track import ActionMan
from firebase_sub.plugins.protocols import (
    CompletePollDbHandler,
    ListenerPlugin,
)
from firebase_sub.runtime.job_queue import JobQueue


class CompletePollListenerPlugin(ListenerPlugin):
    """Listener plugin that enqueues COMP_POLL events for completed polls."""

    def __init__(
        self,
        *,
        db_handler: CompletePollDbHandler,
        event_queue: JobQueue[Event],
        action_manager: ActionMan,
        min_date: str | None,
        max_retries: int,
        retry_delay_seconds: float,
    ) -> None:
        self._db_handler = db_handler
        self._event_queue = event_queue
        self._action_manager = action_manager
        self._min_date = min_date

        @retry(
            retry_errors=(RetryablePollDataNotReadyError,),
            max_retries=max_retries,
            delay_seconds=retry_delay_seconds,
            operation_name="completed poll event after pubs not ready",
        )
        def _retrying_handler(
            document: DocumentSnapshot | None,
            pubs_list: PubsList,
        ) -> None:
            self._run_complete_poll_handler(document=document, pubs_list=pubs_list)

        self._retrying_handler = _retrying_handler

    def name(self) -> str:
        return "complete_poll_listener"

    def on_registered(self) -> None:
        return

    def on_unregistered(self) -> None:
        return

    def build_manager(self) -> AbstractContextManager[object]:
        return PollManager(
            self._db_handler.query_polls_by_status(
                completed=True,
                min_date=self._min_date,
            ),
            add=self._complete_poll_event_callback,
            modify=self._complete_poll_event_callback,
        )

    def _run_complete_poll_handler(
        self,
        *,
        document: DocumentSnapshot | None,
        pubs_list: PubsList,
    ) -> None:
        if document is None:
            raise ValueError(
                "Completed Event has no document. This indicates a coding error."
            )
        self._db_handler.complete_poll_event_handler(
            pubs_list,
            self._action_manager,
            poll_id=document.id,
        )

    def _complete_poll_handler(
        self,
        document: DocumentSnapshot | None,
        pubs_list: PubsList,
    ) -> None:
        self._retrying_handler(document, pubs_list)

    def _complete_poll_event_callback(self, document: DocumentSnapshot) -> None:
        self._event_queue.put(
            Event(
                type=EventType.COMP_POLL,
                doc=document,
                callback=self._complete_poll_handler,
            )
        )
