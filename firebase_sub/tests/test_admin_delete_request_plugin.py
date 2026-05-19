from types import SimpleNamespace
from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventType
from firebase_sub.plugins.admin_delete_request import AdminDeleteRequestListenerPlugin
from firebase_sub.runtime.job_queue import JobQueue


class _FakeAdminDeleteHandler:
    def __init__(self, *, enabled: bool) -> None:
        self.enabled = enabled

    def handle(self, request_document, pubs_list) -> None:
        del request_document, pubs_list


def test_admin_delete_listener_enqueues_admin_delete_event() -> None:
    event_queue: JobQueue = JobQueue()
    handler = _FakeAdminDeleteHandler(enabled=True)
    plugin = AdminDeleteRequestListenerPlugin(
        query_admin_delete_requests=lambda: cast(object, None),
        event_queue=event_queue,
        handler=handler,
    )
    document = cast(DocumentSnapshot, SimpleNamespace(id="req-1"))

    plugin._admin_delete_request_callback(document)

    event = event_queue.get(timeout=0.1)
    assert event.type == EventType.ADMIN_DELETE_REQUEST
    assert event.doc is document
    assert event.callback == handler.handle


def test_admin_delete_listener_is_disabled_when_handler_disabled() -> None:
    plugin = AdminDeleteRequestListenerPlugin(
        query_admin_delete_requests=lambda: cast(object, None),
        event_queue=JobQueue(),
        handler=_FakeAdminDeleteHandler(enabled=False),
    )

    assert plugin.is_enabled() is False
