from datetime import UTC, date, datetime
from unittest.mock import MagicMock

from firebase_sub.database.housekeeping_tasks import (
    DIAGNOSTICS_DOC_ID,
    NOTIFICATION_ACK_COLLECTION,
    NOTIFICATION_REQ_COLLECTION,
    POLLS_COLLECTION,
    PUSH_TEST_DOC_ID,
    PUSH_ENDPOINTS_COLLECTION,
    delete_inactive_push_endpoints,
    delete_notification_diagnostics,
    delete_notification_docs_for_past_polls,
    delete_stale_push_diagnostic_entries,
)


def test_delete_notification_diagnostics_deletes_req_and_ack_docs():
    db = MagicMock()
    req_doc = MagicMock()
    ack_doc = MagicMock()

    def collection_side_effect(name):
        collection = MagicMock()
        if name == NOTIFICATION_REQ_COLLECTION:
            collection.document.return_value = req_doc
        elif name == NOTIFICATION_ACK_COLLECTION:
            collection.document.return_value = ack_doc
        return collection

    db.collection.side_effect = collection_side_effect

    delete_notification_diagnostics(db)

    req_doc.delete.assert_called_once_with()
    ack_doc.delete.assert_called_once_with()


def test_delete_notification_docs_for_past_polls_deletes_req_and_ack_for_each_poll():
    db = MagicMock()
    req_collection = MagicMock()
    ack_collection = MagicMock()
    polls_collection = MagicMock()

    req_docs: dict[str, MagicMock] = {}
    ack_docs: dict[str, MagicMock] = {}

    def req_document_side_effect(doc_id: str):
        req_docs.setdefault(doc_id, MagicMock())
        return req_docs[doc_id]

    def ack_document_side_effect(doc_id: str):
        ack_docs.setdefault(doc_id, MagicMock())
        return ack_docs[doc_id]

    req_collection.document.side_effect = req_document_side_effect
    ack_collection.document.side_effect = ack_document_side_effect

    poll_doc_1 = MagicMock()
    poll_doc_1.id = "poll-1"
    poll_doc_2 = MagicMock()
    poll_doc_2.id = "poll-2"

    where_query = MagicMock()
    where_query.stream.return_value = [poll_doc_1, poll_doc_2]
    polls_collection.where.return_value = where_query

    def collection_side_effect(name: str):
        if name == NOTIFICATION_REQ_COLLECTION:
            return req_collection
        if name == NOTIFICATION_ACK_COLLECTION:
            return ack_collection
        if name == POLLS_COLLECTION:
            return polls_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    delete_notification_docs_for_past_polls(db, today=date(2026, 4, 2))

    req_docs["poll-1"].delete.assert_called_once_with()
    req_docs["poll-2"].delete.assert_called_once_with()
    ack_docs["poll-1"].delete.assert_called_once_with()
    ack_docs["poll-2"].delete.assert_called_once_with()


def test_delete_notification_docs_for_past_polls_no_past_polls_no_deletes():
    db = MagicMock()
    req_collection = MagicMock()
    ack_collection = MagicMock()
    polls_collection = MagicMock()

    where_query = MagicMock()
    where_query.stream.return_value = []
    polls_collection.where.return_value = where_query

    def collection_side_effect(name: str):
        if name == NOTIFICATION_REQ_COLLECTION:
            return req_collection
        if name == NOTIFICATION_ACK_COLLECTION:
            return ack_collection
        if name == POLLS_COLLECTION:
            return polls_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    delete_notification_docs_for_past_polls(db, today=date(2026, 4, 2))

    req_collection.document.assert_not_called()
    ack_collection.document.assert_not_called()


def test_delete_inactive_push_endpoints_deletes_matching_docs():
    db = MagicMock()
    query_active = MagicMock()
    query_disabled = MagicMock()
    endpoint_doc_1 = MagicMock()
    endpoint_doc_2 = MagicMock()

    db.collection_group.return_value = query_active
    query_active.where.return_value = query_disabled
    query_disabled.where.return_value.stream.return_value = [
        endpoint_doc_1,
        endpoint_doc_2,
    ]

    now = datetime(2026, 4, 17, tzinfo=UTC)
    delete_inactive_push_endpoints(db, now=now, retention_days=30)

    endpoint_doc_1.reference.delete.assert_called_once_with()
    endpoint_doc_2.reference.delete.assert_called_once_with()


def test_delete_inactive_push_endpoints_no_matches_no_deletes():
    db = MagicMock()
    query_active = MagicMock()
    query_disabled = MagicMock()

    db.collection_group.return_value = query_active
    query_active.where.return_value = query_disabled
    query_disabled.where.return_value.stream.return_value = []

    now = datetime(2026, 4, 17, tzinfo=UTC)
    delete_inactive_push_endpoints(db, now=now, retention_days=30)

    db.collection_group.assert_called_once_with(PUSH_ENDPOINTS_COLLECTION)


def test_delete_inactive_push_endpoints_rejects_negative_retention():
    db = MagicMock()

    try:
        delete_inactive_push_endpoints(db, retention_days=-1)
        raise AssertionError("Expected ValueError")
    except ValueError as exc:
        assert "retention_days" in str(exc)


def test_delete_stale_push_diagnostic_entries_deletes_only_stale_fields():
    db = MagicMock()
    now = datetime(2026, 4, 17, 12, 0, tzinfo=UTC)
    stale_value = int(datetime(2026, 4, 16, 10, 0, tzinfo=UTC).timestamp() * 1000)
    fresh_value = int(datetime(2026, 4, 17, 11, 0, tzinfo=UTC).timestamp() * 1000)

    req_doc = MagicMock()
    req_doc.get.return_value.to_dict.return_value = {
        "stale-user": stale_value,
        "fresh-user": fresh_value,
    }
    ack_doc = MagicMock()
    ack_doc.get.return_value.to_dict.return_value = {
        "stale-user": stale_value,
        "fresh-user": fresh_value,
    }

    def collection_side_effect(name: str):
        collection = MagicMock()
        if name == NOTIFICATION_REQ_COLLECTION:
            collection.document.return_value = req_doc
        elif name == NOTIFICATION_ACK_COLLECTION:
            collection.document.return_value = ack_doc
        return collection

    db.collection.side_effect = collection_side_effect

    delete_stale_push_diagnostic_entries(db, now=now)

    req_doc.set.assert_called_once()
    ack_doc.set.assert_called_once()
    assert req_doc.set.call_args.args[0].keys() == {"stale-user"}
    assert ack_doc.set.call_args.args[0].keys() == {"stale-user"}
    assert req_doc.set.call_args.kwargs == {"merge": True}
    assert ack_doc.set.call_args.kwargs == {"merge": True}


def test_delete_stale_push_diagnostic_entries_no_stale_fields_no_write():
    db = MagicMock()
    now = datetime(2026, 4, 17, 12, 0, tzinfo=UTC)
    fresh_value = int(datetime(2026, 4, 17, 11, 0, tzinfo=UTC).timestamp() * 1000)

    req_doc = MagicMock()
    req_doc.get.return_value.to_dict.return_value = {"fresh-user": fresh_value}
    ack_doc = MagicMock()
    ack_doc.get.return_value.to_dict.return_value = {"fresh-user": fresh_value}

    def collection_side_effect(name: str):
        collection = MagicMock()
        if name == NOTIFICATION_REQ_COLLECTION:
            collection.document.return_value = req_doc
        elif name == NOTIFICATION_ACK_COLLECTION:
            collection.document.return_value = ack_doc
        return collection

    db.collection.side_effect = collection_side_effect

    delete_stale_push_diagnostic_entries(db, now=now)

    req_doc.set.assert_not_called()
    ack_doc.set.assert_not_called()


def test_delete_stale_push_diagnostic_entries_rejects_negative_retention():
    db = MagicMock()

    try:
        delete_stale_push_diagnostic_entries(db, retention_days=-1)
        raise AssertionError("Expected ValueError")
    except ValueError as exc:
        assert "retention_days" in str(exc)
