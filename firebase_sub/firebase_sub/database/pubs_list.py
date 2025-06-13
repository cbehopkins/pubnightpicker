import time
from typing import Any, Sequence
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange
from google.cloud.firestore_v1.collection import CollectionReference

from datetime import datetime as Datetime
from firebase_sub.my_types import Callback, DocumentId


class PubsList(dict[DocumentId, dict[str, Any]]):
    def __init__(self, pub_collection: CollectionReference, *args, **kwargs):
        self.pub_collection = pub_collection
        self.unsubscribe: Callback = None
        super().__init__(*args, **kwargs)

    def __enter__(self):
        self.unsubscribe = self.pub_collection.on_snapshot(
            self._pub_updater
        ).unsubscribe
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        assert self.unsubscribe
        self.unsubscribe()
        self.unsubscribe = None

    def __getitem__(self, key: Any) -> Any:
        if key not in self:
            # Just in case the database hasn't populated yet
            time.sleep(1)
        return super().__getitem__(key)

    def _pub_updater(
        self,
        doc_snapshot: DocumentSnapshot,
        changes: Sequence[DocumentChange],
        read_time: Datetime,
    ):
        for change in changes:
            if change.type.name == "ADDED":
                self[change.document.id] = change.document.to_dict()
            elif change.type.name == "MODIFIED":
                self[change.document.id] = change.document.to_dict()
            elif change.type.name == "REMOVED":
                del self[change.document.id]
