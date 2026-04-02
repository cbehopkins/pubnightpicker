from unittest.mock import MagicMock

import pytest

from firebase_sub.database.repositories import (
    FirestorePollRepository,
    FirestoreUserRepository,
)


def _make_doc(doc_id: str, payload: dict | None):
    doc = MagicMock()
    doc.id = doc_id
    doc.to_dict.return_value = payload
    return doc


def test_poll_repository_get_poll_returns_none_for_missing_payload():
    db = MagicMock()
    db.collection.return_value.document.return_value.get.return_value = _make_doc(
        "poll-1", None
    )

    repo = FirestorePollRepository(db)

    assert repo.get_poll("poll-1") is None


def test_poll_repository_get_polls_by_status_builds_filter_query():
    db = MagicMock()
    query = MagicMock()
    polls_collection = MagicMock()
    polls_collection.where.return_value = query
    db.collection.return_value = polls_collection

    repo = FirestorePollRepository(db)

    assert repo.get_polls_by_status(completed=True) is query
    polls_collection.where.assert_called_once()


def test_user_repository_invalid_preference_raises():
    repo = FirestoreUserRepository(MagicMock())

    with pytest.raises(ValueError, match="Unknown email preference"):
        list(repo.query_users_by_email_preference("bad_flag"))


def test_user_repository_yields_only_complete_user_records():
    db = MagicMock()
    docs_query = MagicMock()
    docs_query.stream.return_value = [
        _make_doc("u1", {"notificationEmail": "one@example.com", "uid": "u1"}),
        _make_doc("u2", {"notificationEmail": "two@example.com"}),
        _make_doc("u3", None),
    ]
    db.collection.return_value.where.return_value = docs_query

    repo = FirestoreUserRepository(db)

    out = list(repo.query_users_by_email_preference("notificationEmailEnabled"))

    assert out == [("one@example.com", "u1")]
