import time
from typing import Any


class PubsList(dict):
    def __init__(self, db, *args, **kwargs):
        self.db = db
        super().__init__(*args, **kwargs)

    def __enter__(self):
        self.unsubscribe = self.db.collection("pubs").on_snapshot(self._pub_updater)
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.unsubscribe()

    def __getitem__(self, key: Any) -> Any:
        if key not in self:
            # Just in case the database hasn't populated yet
            time.sleep(1)
        return super().__getitem__(key)

    def _pub_updater(self, doc_snapshot, changes, read_time):
        for change in changes:
            if change.type.name == "ADDED":
                self[change.document.id] = change.document.to_dict()
            elif change.type.name == "MODIFIED":
                self[change.document.id] = change.document.to_dict()
            elif change.type.name == "REMOVED":
                del self[change.document.id]
