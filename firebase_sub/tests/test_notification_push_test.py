from unittest.mock import MagicMock

from firebase_sub.database.notification_push_diag import NotificationPushTestHandler


class _Snapshot:
    def __init__(self, doc_id, payload):
        self.id = doc_id
        self._payload = payload

    def to_dict(self):
        return self._payload


def _build_db(ack_payload=None):
    db = MagicMock()
    req_doc = MagicMock()
    ack_doc = MagicMock()
    ack_doc.get.return_value = _Snapshot("push_test", ack_payload)

    def _document(doc_id):
        if doc_id == "push_test":
            # First collection call is resolved by outer collection name.
            return (
                req_doc
                if _document.current_collection == "notification_req"
                else ack_doc
            )
        raise AssertionError(f"Unexpected doc id: {doc_id}")

    _document.current_collection = None

    def _collection(name):
        collection = MagicMock()
        _document.current_collection = name
        collection.document.side_effect = _document
        return collection

    db.collection.side_effect = _collection
    return db, req_doc, ack_doc


def test_ignores_non_push_test_document():
    db, req_doc, ack_doc = _build_db()
    handler = NotificationPushTestHandler(db, lambda _uid: [], dummy_push=True)

    handled = handler.handle_request_document(_Snapshot("diagnostics", {"manual": 1}))

    assert handled is False
    req_doc.set.assert_not_called()
    ack_doc.set.assert_not_called()


def test_processes_uid_request_and_acks_then_clears(monkeypatch):
    db, req_doc, ack_doc = _build_db(ack_payload={})
    query_mock = MagicMock(return_value=[])

    send_mock = MagicMock()
    send_mock.return_value = MagicMock(delivered=1, invalid=0, retryable_failures=0)
    monkeypatch.setattr(
        "firebase_sub.database.notification_push_diag.send_diagnostic_push",
        send_mock,
    )

    handler = NotificationPushTestHandler(db, query_mock, dummy_push=True)

    handled = handler.handle_request_document(_Snapshot("push_test", {"uid-1": 123}))

    assert handled is True
    send_mock.assert_called_once()
    ack_doc.set.assert_called_once_with({"uid-1": 123}, merge=True)
    req_doc.set.assert_called_once()


def test_skips_when_ack_already_matches_request(monkeypatch):
    db, req_doc, ack_doc = _build_db(ack_payload={"uid-1": 123})

    send_mock = MagicMock()
    monkeypatch.setattr(
        "firebase_sub.database.notification_push_diag.send_diagnostic_push",
        send_mock,
    )

    handler = NotificationPushTestHandler(db, lambda _uid: [], dummy_push=True)
    handler.handle_request_document(_Snapshot("push_test", {"uid-1": 123}))

    send_mock.assert_not_called()
    ack_doc.set.assert_not_called()
    req_doc.set.assert_called_once()


def test_does_not_ack_when_delivery_count_zero(monkeypatch):
    db, req_doc, ack_doc = _build_db(ack_payload={})

    send_mock = MagicMock()
    send_mock.return_value = MagicMock(delivered=0, invalid=0, retryable_failures=0)
    monkeypatch.setattr(
        "firebase_sub.database.notification_push_diag.send_diagnostic_push",
        send_mock,
    )

    handler = NotificationPushTestHandler(db, lambda _uid: [], dummy_push=True)
    handler.handle_request_document(_Snapshot("push_test", {"uid-1": 123}))

    ack_doc.set.assert_not_called()
    req_doc.set.assert_called_once()
