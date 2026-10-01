from types import SimpleNamespace
from typing import Any, cast

import pytest
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client

from firebase_sub.common.rate_limit import TokenBucket, rate_limited
from firebase_sub.database.test_email_request import (
    TestEmailRequestHandler,
    resolve_test_email_address,
)
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.plugin_config import build_event_registry
from firebase_sub.plugins.test_email_request import TestEmailRequestListenerPlugin
from firebase_sub.send_email import _skip_test_mail_send, send_test_email


class _FakeUserRef:
    def __init__(self, payload: dict[str, Any] | None) -> None:
        self.payload = payload
        self.updates: list[dict[str, Any]] = []

    def get(self) -> Any:
        return SimpleNamespace(to_dict=lambda: self.payload)

    def update(self, data: dict[str, Any]) -> None:
        self.updates.append(data)


class _FakeDb:
    def __init__(self, user_ref: _FakeUserRef) -> None:
        self.user_ref = user_ref
        self.paths: list[tuple[str, str]] = []

    def collection(self, name: str) -> Any:
        def document(doc_id: str) -> _FakeUserRef:
            self.paths.append((name, doc_id))
            return self.user_ref

        return SimpleNamespace(document=document)


class _FakeSender:
    def __init__(self, result: bool = True, error: Exception | None = None) -> None:
        self.result = result
        self.error = error
        self.calls: list[tuple[str, bool]] = []

    def __call__(self, email: str, *, dummy_run: bool) -> bool:
        self.calls.append((email, dummy_run))
        if self.error is not None:
            raise self.error
        return self.result


def _doc(uid: str = "u1") -> DocumentSnapshot:
    return cast(DocumentSnapshot, SimpleNamespace(id=uid))


def _handler(
    payload: dict[str, Any] | None, sender: _FakeSender, *, dummy_run: bool = False
) -> tuple[TestEmailRequestHandler, _FakeUserRef, _FakeDb]:
    user_ref = _FakeUserRef(payload)
    db = _FakeDb(user_ref)
    handler = TestEmailRequestHandler(
        cast(Client, db), dummy_run=dummy_run, sender=sender
    )
    return handler, user_ref, db


def test_sends_and_acks_when_request_differs_from_ack() -> None:
    sender = _FakeSender()
    handler, user_ref, db = _handler(
        {
            "testEmailReq": "uuid-2",
            "testEmailAck": "uuid-1",
            "notificationEmail": "alerts@example.com",
            "email": "login@example.com",
        },
        sender,
        dummy_run=True,
    )

    handler.handle_request_document(_doc("u1"))

    assert db.paths == [("users", "u1")]
    assert sender.calls == [("alerts@example.com", True)]
    assert user_ref.updates == [{"testEmailAck": "uuid-2"}]


def test_sends_when_ack_missing() -> None:
    sender = _FakeSender()
    handler, user_ref, _ = _handler(
        {"testEmailReq": "uuid-1", "email": "login@example.com"}, sender
    )

    handler.handle_request_document(_doc())

    assert sender.calls == [("login@example.com", False)]
    assert user_ref.updates == [{"testEmailAck": "uuid-1"}]


@pytest.mark.parametrize(
    "payload",
    [
        None,
        {"email": "a@example.com"},
        {"testEmailReq": "", "email": "a@example.com"},
        {"testEmailReq": 123, "email": "a@example.com"},
        {"testEmailReq": "x", "testEmailAck": "x", "email": "a@example.com"},
    ],
)
def test_no_op_when_nothing_pending(payload: dict[str, Any] | None) -> None:
    sender = _FakeSender()
    handler, user_ref, _ = _handler(payload, sender)

    handler.handle_request_document(_doc())

    assert sender.calls == []
    assert user_ref.updates == []


def test_acks_without_sending_when_no_address() -> None:
    sender = _FakeSender()
    handler, user_ref, _ = _handler(
        {"testEmailReq": "uuid-1", "notificationEmail": "  ", "email": ""}, sender
    )

    handler.handle_request_document(_doc())

    assert sender.calls == []
    assert user_ref.updates == [{"testEmailAck": "uuid-1"}]


def test_leaves_request_pending_when_rate_limited() -> None:
    sender = _FakeSender(result=False)
    handler, user_ref, _ = _handler(
        {"testEmailReq": "uuid-1", "email": "a@example.com"}, sender
    )

    handler.handle_request_document(_doc())

    assert len(sender.calls) == 1
    assert user_ref.updates == []


def test_leaves_request_pending_and_does_not_raise_when_send_fails() -> None:
    sender = _FakeSender(error=RuntimeError("boom"))
    handler, user_ref, _ = _handler(
        {"testEmailReq": "uuid-1", "email": "a@example.com"}, sender
    )

    handler.handle_request_document(_doc())

    assert user_ref.updates == []


def test_resolve_address_prefers_notification_email() -> None:
    assert (
        resolve_test_email_address({"notificationEmail": "n@x.com", "email": "e@x.com"})
        == "n@x.com"
    )
    assert resolve_test_email_address({"email": " e@x.com "}) == "e@x.com"
    assert resolve_test_email_address({}) is None


def test_plugin_filters_and_delegates_to_handler() -> None:
    handled: list[DocumentSnapshot | None] = []
    plugin = TestEmailRequestListenerPlugin(
        handler=SimpleNamespace(handle_request_document=handled.append)
    )
    doc = _doc()

    assert plugin.filter(EventEnvelope(type=EventType.TEST_EMAIL_REQUEST, doc=doc))
    assert not plugin.filter(EventEnvelope(type=EventType.TEST_EMAIL_REQUEST, doc=None))
    assert not plugin.filter(EventEnvelope(type=EventType.PUSH, doc=doc))

    envelope = EventEnvelope(type=EventType.TEST_EMAIL_REQUEST, doc=doc)
    plugin.handle(envelope)
    assert plugin.mark_done(envelope) is None
    assert handled == [doc]


def test_event_registry_routes_test_email_plugin() -> None:
    plugin = TestEmailRequestListenerPlugin(
        handler=SimpleNamespace(handle_request_document=lambda _doc: None)
    )

    registry = build_event_registry(event_plugins=[plugin])

    assert list(registry.get_plugins(EventType.TEST_EMAIL_REQUEST)) == [plugin]


class _FakeMail:
    def __init__(self, **kwargs: Any) -> None:
        self.kwargs = kwargs


class _FakeClient:
    def __init__(self) -> None:
        self.sent: list[_FakeMail] = []

    def send(self, mail: _FakeMail) -> dict[str, Any]:
        self.sent.append(mail)
        return {}


def test_send_test_email_sends_single_mail(monkeypatch: pytest.MonkeyPatch) -> None:
    fake_client = _FakeClient()
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Mail", _FakeMail)
    monkeypatch.setattr(
        "firebase_sub.send_email.mailtrap.Address", lambda **kwargs: kwargs
    )
    monkeypatch.setattr(
        "firebase_sub.send_email._mail_client", lambda dummy_run=True: fake_client
    )

    assert send_test_email("a@example.com", dummy_run=False) is True

    assert len(fake_client.sent) == 1
    sent = fake_client.sent[0].kwargs
    assert sent["to"] == [{"email": "a@example.com"}]
    assert sent["subject"] == "Pub Night Picker test email"


def test_exhausted_test_bucket_skips_with_false() -> None:
    bucket = TokenBucket(
        refill_amount=1,
        max_tokens=1,
        refill_interval_seconds=3600,
        initial_tokens=0,
        on_stall=_skip_test_mail_send,
    )
    try:
        calls: list[str] = []

        @rate_limited(bucket)
        def _send(email: str) -> bool:
            calls.append(email)
            return True

        assert _send("a@example.com") is False
        assert calls == []
    finally:
        bucket.close()
