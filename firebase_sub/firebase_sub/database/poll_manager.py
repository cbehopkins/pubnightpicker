import logging
import threading
from collections.abc import Sequence
from datetime import datetime as Datetime
from typing import Self

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.my_types import Callback, DocCallback

_log = logging.getLogger("PollManager")


class PollManager:
    def __init__(
        self,
        query: Query,
        add: DocCallback = None,
        modify: DocCallback = None,
        rm: DocCallback = None,
    ):
        self.add = add
        self.modify = modify
        self.rm = rm
        self.query = query
        self.unsubscribe: Callback = None
        self._lock = threading.Lock()

    def __enter__(self) -> Self:
        self._start_watch()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        assert self.unsubscribe
        self.unsubscribe()
        self.unsubscribe = None

    def _start_watch(self):
        with self._lock:
            if self.unsubscribe:
                self.unsubscribe()
            self.unsubscribe = self.query.on_snapshot(self._poll_updater).unsubscribe

    def _poll_updater(
        self,
        doc_snapshot: Sequence[DocumentSnapshot],
        changes: Sequence[DocumentChange],
        read_time: Datetime,
    ) -> None:
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

    def start_periodic_restart(self, minutes: int) -> Self:
        """Retained for compatibility; periodic watch restarts are disabled."""
        _log.info(
            "Ignoring periodic restart request (%s minutes); Firestore watches now stay attached until process exit",
            minutes,
        )
        return self
