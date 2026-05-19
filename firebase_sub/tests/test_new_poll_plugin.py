from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.query import Query

from firebase_sub.action_track import ActionMan
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import EventType
from firebase_sub.plugins.new_poll import NewPollListenerPlugin
from firebase_sub.plugins.protocols import NewPollDbHandler
from firebase_sub.runtime.job_queue import JobQueue


class _FakeDbHandler:
    def __init__(self) -> None:
        self.calls: list[tuple[object, str]] = []

    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query:
        del completed, min_date
        return cast(Query, SimpleNamespace(on_snapshot=lambda _cb: None))

    def new_poll_event_handler(self, am: ActionMan, poll_id: str) -> None:
        self.calls.append((am, poll_id))


class _FakeActionManager(ActionMan):
    pass


def test_new_poll_listener_enqueues_new_poll_event():
    db_handler: NewPollDbHandler = _FakeDbHandler()
    event_queue: JobQueue = JobQueue()
    action_manager: ActionMan = _FakeActionManager()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        event_queue=event_queue,
        action_manager=action_manager,
        min_date=None,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="poll-1"))

    plugin._open_poll_event_callback(document)

    event = event_queue.get(timeout=0.1)
    assert event.type == EventType.NEW_POLL
    assert event.doc is document


def test_new_poll_listener_handler_calls_db_handler():
    db_handler: NewPollDbHandler = _FakeDbHandler()
    event_queue: JobQueue = JobQueue()
    action_manager: ActionMan = _FakeActionManager()
    plugin = NewPollListenerPlugin(
        db_handler=db_handler,
        event_queue=event_queue,
        action_manager=action_manager,
        min_date=None,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="poll-2"))

    plugin._new_poll_handler(document, cast(PubsList, object()))

    assert cast(_FakeDbHandler, db_handler).calls == [(action_manager, "poll-2")]
