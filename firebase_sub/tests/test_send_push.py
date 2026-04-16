from types import SimpleNamespace
from unittest.mock import ANY

import pytest
from pywebpush import WebPushException

from firebase_sub.action_track import CallbackExceptionRetry
from firebase_sub.send_push import (
    _build_complete_payload,
    _build_open_payload,
    _deliver_pushes,
)


class _DocRef:
    def __init__(self):
        self.set_calls = []

    def set(self, payload, merge):
        self.set_calls.append((payload, merge))


class _EndpointDoc:
    def __init__(self, endpoint, uid="user-1", p256dh="p256dh", auth="auth"):
        self.id = "endpoint-1"
        self.reference = _DocRef()
        self._endpoint = endpoint
        self._uid = uid
        self._p256dh = p256dh
        self._auth = auth

    def to_dict(self):
        payload = {"endpoint": self._endpoint}
        if self._p256dh is not None:
            payload["p256dh"] = self._p256dh
        if self._auth is not None:
            payload["auth"] = self._auth
        return payload

    @property
    def reference_parent_id(self):
        return self._uid


def _link_parent(doc):
    doc.reference.parent = SimpleNamespace(parent=SimpleNamespace(id=doc._uid))
    return doc


def test_build_open_payload_contains_expected_fields():
    payload = _build_open_payload("poll-1")

    assert payload["eventType"] == "poll_opened"
    assert payload["pollId"] == "poll-1"
    assert payload["url"].endswith("/active_polls")
    assert payload["tag"] == "poll-open:poll-1"


def test_build_complete_payload_marks_reschedule_when_previously_actioned():
    payload = _build_complete_payload(
        poll_id="poll-9",
        poll_dict={"selected": "pub-1", "date": "2026-04-16"},
        pub_dict={"pub-1": {"name": "The Swan", "venueType": "pub"}},
        previously_actioned=True,
    )

    assert payload["eventType"] == "poll_rescheduled"
    assert payload["title"].startswith("Pub Night rescheduled")
    assert payload["url"].endswith("/current_events")


def test_deliver_pushes_supports_dummy_delivery_without_retry_failures():
    docs = [_link_parent(_EndpointDoc("https://push.example/a"))]

    result = _deliver_pushes(
        payload={"eventType": "poll_opened"},
        endpoints_src=lambda: docs,
        dummy_run=True,
    )

    assert result.delivered == 1
    assert result.retryable_failures == 0


@pytest.mark.parametrize(
    ("p256dh", "auth"),
    [
        (None, "auth"),
        ("p256dh", None),
        (None, None),
    ],
)
def test_deliver_pushes_deactivates_malformed_endpoint_missing_keys(p256dh, auth):
    doc = _link_parent(_EndpointDoc("https://push.example/a", p256dh=p256dh, auth=auth))

    result = _deliver_pushes(
        payload={"eventType": "poll_opened"},
        endpoints_src=lambda: [doc],
        dummy_run=False,
    )

    assert result.delivered == 0
    assert result.invalid == 1
    assert result.retryable_failures == 0
    assert doc.reference.set_calls == [
        (
            {
                "active": False,
                "disabledAt": ANY,
                "lastSeenAt": ANY,
            },
            True,
        )
    ]


def test_deliver_pushes_raises_retry_exception_on_retryable_failure(monkeypatch):
    docs = [_link_parent(_EndpointDoc("https://push.example/a"))]

    def _boom(*args, **kwargs):
        raise RuntimeError("network down")

    monkeypatch.setattr("firebase_sub.send_push._send_to_endpoint", _boom)

    with pytest.raises(CallbackExceptionRetry):
        _deliver_pushes(
            payload={"eventType": "poll_opened"},
            endpoints_src=lambda: docs,
            dummy_run=False,
        )


def test_deliver_pushes_deactivates_endpoint_on_non_retryable_403(monkeypatch):
    doc = _link_parent(_EndpointDoc("https://push.example/a"))
    docs = [doc]

    def _forbidden(*args, **kwargs):
        _ = args, kwargs
        raise WebPushException(
            "forbidden",
            response=SimpleNamespace(status_code=403, text="Forbidden"),
        )

    monkeypatch.setattr("firebase_sub.send_push._send_to_endpoint", _forbidden)

    result = _deliver_pushes(
        payload={"eventType": "poll_opened"},
        endpoints_src=lambda: docs,
        dummy_run=False,
    )

    assert result.retryable_failures == 0
    assert result.invalid == 1
    assert doc.reference.set_calls


def test_deliver_pushes_raises_retry_on_webpush_500(monkeypatch):
    docs = [_link_parent(_EndpointDoc("https://push.example/a"))]

    def _server_error(*args, **kwargs):
        _ = args, kwargs
        raise WebPushException(
            "server error",
            response=SimpleNamespace(status_code=500, text="Internal Server Error"),
        )

    monkeypatch.setattr("firebase_sub.send_push._send_to_endpoint", _server_error)

    with pytest.raises(CallbackExceptionRetry):
        _deliver_pushes(
            payload={"eventType": "poll_opened"},
            endpoints_src=lambda: docs,
            dummy_run=False,
        )
