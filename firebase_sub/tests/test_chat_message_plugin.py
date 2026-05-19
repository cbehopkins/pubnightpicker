from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventType
from firebase_sub.plugins.chat_message import ChatMessageListenerPlugin
from firebase_sub.runtime.job_queue import JobQueue


class _FakeDbHandler:
    def handle_chat_message(
        self, message_doc, pubs_list, *, dummy_run: bool = False
    ) -> None:
        del message_doc, pubs_list, dummy_run


def test_chat_message_listener_enqueues_chat_event() -> None:
    event_queue: JobQueue = JobQueue()
    db_handler = _FakeDbHandler()
    plugin = ChatMessageListenerPlugin(
        query_messages=lambda: cast(object, None),
        db_handler=cast(object, db_handler),
        event_queue=event_queue,
        dummy_run=True,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="msg-1"))

    plugin._chat_message_callback(document)

    event = event_queue.get(timeout=0.1)
    assert event.type == EventType.CHAT_MESSAGE
    assert event.doc is document


def test_chat_message_listener_is_enabled() -> None:
    plugin = ChatMessageListenerPlugin(
        query_messages=lambda: cast(object, None),
        db_handler=cast(object, _FakeDbHandler()),
        event_queue=JobQueue(),
        dummy_run=False,
    )

    assert plugin.is_enabled() is True
