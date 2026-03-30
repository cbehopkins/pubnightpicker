# type: ignore
from datetime import datetime
from unittest.mock import MagicMock

import pytest

from firebase_sub.database.pubs_list import PubsList


class DummyDoc:
    def __init__(self, id, data):
        self.id = id
        self._data = data

    def to_dict(self):
        return self._data


class DummyChange:
    def __init__(self, type_name, doc):
        self.type = MagicMock()
        self.type.name = type_name
        self.document = doc


def test_pub_updater_add_modify_remove():
    # Mock pub_collection and on_snapshot
    pub_collection = MagicMock()
    unsubscribe_mock = MagicMock()
    pub_collection.on_snapshot.return_value.unsubscribe = unsubscribe_mock

    pubs = PubsList(pub_collection)

    # Simulate ADDED
    doc1 = DummyDoc("pub1", {"name": "Pub One"})
    change1 = DummyChange("ADDED", doc1)
    pubs._poll_updater([doc1], [change1], datetime.now())
    assert pubs["pub1"] == {"name": "Pub One"}

    # Simulate MODIFIED
    doc1_mod = DummyDoc("pub1", {"name": "Pub One Modified"})
    change1_mod = DummyChange("MODIFIED", doc1_mod)
    pubs._poll_updater([doc1_mod], [change1_mod], datetime.now())
    assert pubs["pub1"] == {"name": "Pub One Modified"}

    # Simulate REMOVED
    change1_rem = DummyChange("REMOVED", doc1_mod)
    pubs._poll_updater([doc1_mod], [change1_rem], datetime.now())
    assert "pub1" not in pubs


def test_context_manager_calls_unsubscribe():
    pub_collection = MagicMock()
    unsubscribe_mock = MagicMock()
    pub_collection.on_snapshot.return_value.unsubscribe = unsubscribe_mock
    with PubsList(pub_collection) as pubs:
        pass
    unsubscribe_mock.assert_called_once()
