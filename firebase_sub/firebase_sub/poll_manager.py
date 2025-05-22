import logging
from typing import  Sequence
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange
from datetime import datetime as Datetime

from firebase_sub.my_types import Callback, DocCallback
_log = logging.getLogger("PollManager")



class PollManager:
    def __init__(self, query: Query, add: DocCallback=None, modify: DocCallback=None, rm: DocCallback=None):
        self.add = add
        self.modify = modify
        self.rm = rm
        self.query = query
        self.unsubscribe: Callback = None

    def __enter__(self):
        self.unsubscribe = self.query.on_snapshot(self._poll_updater).unsubscribe

    def __exit__(self, exc_type, exc_val, exc_tb):
        assert self.unsubscribe
        self.unsubscribe()
        self.unsubscribe = None

    def _poll_updater(self, doc_snapshot: DocumentSnapshot, changes: Sequence[DocumentChange], read_time: Datetime):
        for change in changes:
            if change.type.name == "ADDED":
                if self.add is None:
                    _log.info(f"New {change.document.id}")
                else:
                    self.add(change.document)
            elif change.type.name == "MODIFIED":
                if self.modify is None:
                    _log.info(f"Modified : {change.document.id}")
                else:
                    self.modify(change.document)
            elif change.type.name == "REMOVED":
                if self.rm is None:
                    _log.info(f"Removed : {change.document.id}")
                else:
                    self.rm(change.document)
