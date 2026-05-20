from types import SimpleNamespace
from typing import cast

import pytest
from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.common.retry import retry
from firebase_sub.database.handlers import RetryablePollDataNotReadyError
from firebase_sub.database.pubs_list import PubsList


def test_retry_calls_callback_once_when_no_error():
    calls = []

    @retry(
        retry_errors=(RetryablePollDataNotReadyError,),
        max_retries=3,
        delay_seconds=0.1,
        operation_name="complete poll",
    )
    def callback(document, pubs_list):
        calls.append((document.id, pubs_list))

    document = cast(DocumentSnapshot, SimpleNamespace(id="poll-1"))
    pubs_list = cast(PubsList, object())
    callback(document, pubs_list)

    assert calls == [("poll-1", pubs_list)]


def test_retry_retries_then_succeeds(monkeypatch):
    monkeypatch.setattr("firebase_sub.common.retry.time.sleep", lambda _seconds: None)
    call_count = {"value": 0}

    @retry(
        retry_errors=(RetryablePollDataNotReadyError,),
        max_retries=5,
        delay_seconds=0.1,
        operation_name="complete poll",
    )
    def callback(document, pubs_list):
        call_count["value"] += 1
        if call_count["value"] < 3:
            raise RetryablePollDataNotReadyError("pubs list not ready")

    callback(
        cast(DocumentSnapshot, SimpleNamespace(id="poll-2")), cast(PubsList, object())
    )

    assert call_count["value"] == 3


def test_retry_raises_after_max_retries(monkeypatch):
    monkeypatch.setattr("firebase_sub.common.retry.time.sleep", lambda _seconds: None)
    call_count = {"value": 0}

    @retry(
        retry_errors=(RetryablePollDataNotReadyError,),
        max_retries=4,
        delay_seconds=0.1,
        operation_name="complete poll",
    )
    def callback(document, pubs_list):
        call_count["value"] += 1
        raise RetryablePollDataNotReadyError("still not ready")

    with pytest.raises(RetryablePollDataNotReadyError):
        callback(
            cast(DocumentSnapshot, SimpleNamespace(id="poll-3")),
            cast(PubsList, object()),
        )

    assert call_count["value"] == 4
