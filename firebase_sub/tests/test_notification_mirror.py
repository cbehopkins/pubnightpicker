from unittest.mock import MagicMock

from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler


def _snapshot(doc_id: str, payload: dict | None) -> MagicMock:
    snapshot = MagicMock()
    snapshot.id = doc_id
    snapshot.to_dict.return_value = payload
    return snapshot


def test_request_create_with_one_key_mirrors_to_ack():
    db = MagicMock()
    ack_document = MagicMock()
    ack_document.get.return_value = _snapshot("diagnostics", None)
    db.collection.return_value.document.return_value = ack_document

    mirror = NotificationAckMirrorHandler(db)

    mirror.mirror_request_document(_snapshot("diagnostics", {"manual": 123}))

    ack_document.set.assert_called_once_with({"manual": 123}, merge=True)


def test_request_update_adds_second_key_ack_keeps_first_and_adds_second():
    db = MagicMock()
    ack_document = MagicMock()
    ack_document.get.return_value = _snapshot("diagnostics", {"manual": 123})
    db.collection.return_value.document.return_value = ack_document

    mirror = NotificationAckMirrorHandler(db)

    mirror.mirror_request_document(
        _snapshot("diagnostics", {"manual": 123, "other": "ok"})
    )

    ack_document.set.assert_called_once_with({"other": "ok"}, merge=True)


def test_request_changes_existing_key_value_ack_updates_only_that_key():
    db = MagicMock()
    ack_document = MagicMock()
    ack_document.get.return_value = _snapshot("diagnostics", {"manual": 123})
    db.collection.return_value.document.return_value = ack_document

    mirror = NotificationAckMirrorHandler(db)

    mirror.mirror_request_document(_snapshot("diagnostics", {"manual": 456}))

    ack_document.set.assert_called_once_with({"manual": 456}, merge=True)


def test_noop_when_ack_already_matches_request():
    db = MagicMock()
    ack_document = MagicMock()
    ack_document.get.return_value = _snapshot("diagnostics", {"manual": 456})
    db.collection.return_value.document.return_value = ack_document

    mirror = NotificationAckMirrorHandler(db)

    mirror.mirror_request_document(_snapshot("diagnostics", {"manual": 456}))

    ack_document.set.assert_not_called()
