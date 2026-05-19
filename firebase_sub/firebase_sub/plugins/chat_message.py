from contextlib import AbstractContextManager
from collections.abc import Callable
from functools import partial

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.query import Query

from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.event import Event, EventType
from firebase_sub.plugins.protocols import ListenerPlugin
from firebase_sub.runtime.job_queue import JobQueue


class ChatMessageListenerPlugin(ListenerPlugin):
    """Listener plugin for chat message push processing."""

    def __init__(
        self,
        *,
        query_messages: Query | Callable[[], Query],
        db_handler: DbHandler,
        event_queue: JobQueue[Event],
        dummy_run: bool,
    ) -> None:
        self._query_messages = query_messages
        self._db_handler = db_handler
        self._event_queue = event_queue
        self._dummy_run = dummy_run

    def name(self) -> str:
        return "chat_message_listener"

    def is_enabled(self) -> bool:
        return True

    def build_manager(self) -> AbstractContextManager[object]:
        query = (
            self._query_messages()
            if callable(self._query_messages)
            else self._query_messages
        )
        return PollManager(
            query=query,
            add=self._chat_message_callback,
        )

    def _chat_message_callback(self, document: DocumentSnapshot) -> None:
        self._event_queue.put(
            Event(
                type=EventType.CHAT_MESSAGE,
                doc=document,
                callback=partial(
                    self._db_handler.handle_chat_message,
                    dummy_run=self._dummy_run,
                ),
            )
        )
