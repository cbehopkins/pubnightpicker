from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventType
from firebase_sub.plugins.notification_request import NotificationRequestListenerPlugin
from firebase_sub.runtime.job_queue import JobQueue


class _FakeNotificationMirrorHandler:
    def handle(self, request_document, pubs_list) -> None:
        del request_document, pubs_list


class _FakeNotificationPushTestHandler:
    def __init__(self, *, push_test_ids: set[str]) -> None:
        self._push_test_ids = push_test_ids

    def is_push_test_request(self, request_document) -> bool:
        return request_document.id in self._push_test_ids

    def handle(self, request_document, pubs_list) -> None:
        del request_document, pubs_list


def test_notification_request_listener_enqueues_push_test_event() -> None:
    event_queue: JobQueue = JobQueue()
    plugin = NotificationRequestListenerPlugin(
        query_notification_requests=lambda: cast(object, None),
        event_queue=event_queue,
        notification_mirror=_FakeNotificationMirrorHandler(),
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="push_test"))

    plugin._notification_request_callback(document)

    event = event_queue.get(timeout=0.1)
    assert event.type == EventType.PUSH_TEST
    assert event.doc is document


def test_notification_request_listener_enqueues_mirror_event_for_regular_docs() -> None:
    event_queue: JobQueue = JobQueue()
    mirror_handler = _FakeNotificationMirrorHandler()
    plugin = NotificationRequestListenerPlugin(
        query_notification_requests=lambda: cast(object, None),
        event_queue=event_queue,
        notification_mirror=mirror_handler,
        notification_push_test=_FakeNotificationPushTestHandler(
            push_test_ids={"push_test"}
        ),
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-1"))

    plugin._notification_request_callback(document)

    event = event_queue.get(timeout=0.1)
    assert event.type == EventType.PUSH
    assert event.doc is document
    assert event.callback == mirror_handler.handle
