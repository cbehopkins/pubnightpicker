from unittest.mock import MagicMock

import pytest

from firebase_sub.database import repositories
from firebase_sub.database.repositories import (
    FirestorePollRepository,
    FirestoreUserRepository,
    _parse_poll_document,
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


@pytest.mark.parametrize(
    ("selected", "stored_completed", "expected"),
    [("", True, False), ("venue-1", False, True), ("any", False, True)],
)
def test_poll_parser_derives_completion_from_selected(
    selected, stored_completed, expected, caplog
):
    poll = _parse_poll_document(
        {"selected": selected, "date": "2026-11-26", "completed": stored_completed}
    )

    assert poll is not None
    assert poll["completed"] is expected
    if selected == "any":
        assert 'unexpected selected Venue ID "any"' in caplog.text


@pytest.mark.parametrize(("completed", "operator"), [(True, ">"), (False, "==")])
def test_poll_repository_get_polls_by_status_builds_filter_query(
    monkeypatch, completed, operator
):
    db = MagicMock()
    query = MagicMock()
    polls_collection = MagicMock()
    polls_collection.where.return_value = query
    db.collection.return_value = polls_collection
    monkeypatch.setattr(repositories, "FieldFilter", lambda *parts: parts)

    repo = FirestorePollRepository(db)

    assert repo.get_polls_by_status(completed=completed) is query
    polls_collection.where.assert_called_once_with(filter=("selected", operator, ""))


def test_user_repository_invalid_preference_raises():
    repo = FirestoreUserRepository(MagicMock())

    with pytest.raises(ValueError, match="Unknown email preference"):
        list(repo.query_users_by_email_preference("bad_flag"))


def test_user_repository_yields_only_complete_user_records():
    db = MagicMock()
    docs_query = MagicMock()
    docs_query.stream.return_value = [
        _make_doc("u1", {"notificationEmail": "one@example.com"}),
        _make_doc("u2", {"notificationEmail": ""}),
        _make_doc("u3", None),
    ]
    db.collection.return_value.where.return_value = docs_query

    repo = FirestoreUserRepository(db)

    out = list(repo.query_users_by_email_preference("notificationEmailEnabled"))

    assert out == [("one@example.com", "u1")]
