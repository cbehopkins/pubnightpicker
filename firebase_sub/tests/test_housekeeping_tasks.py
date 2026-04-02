from datetime import date
from unittest.mock import MagicMock

from firebase_sub.database.housekeeping_tasks import (
    DIAGNOSTICS_DOC_ID,
    NOTIFICATION_ACK_COLLECTION,
    NOTIFICATION_REQ_COLLECTION,
    POLLS_COLLECTION,
    delete_notification_diagnostics,
    delete_notification_docs_for_past_polls,
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
