import logging
import threading
from datetime import datetime as Datetime
from collections.abc import Sequence
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
        self._restart_timer: threading.Timer | None = None
        self._restart_interval: int | None = None
        self._lock = threading.Lock()
        self._last_document: DocumentSnapshot | None = None

    def __enter__(self) -> Self:
        self._restart()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        assert self.unsubscribe
        self.unsubscribe()
        self.unsubscribe = None
        self._cancel_restart_timer()
        self._last_document = None

    def _restart(self):
        with self._lock:
            if self.unsubscribe:
                self.unsubscribe()
            query_to_watch = self.query
            if self._last_document is not None:
                query_to_watch = query_to_watch.start_after(self._last_document)
            self.unsubscribe = query_to_watch.on_snapshot(
                self._poll_updater
            ).unsubscribe
            if self._restart_interval is not None:
                self._start_restart_timer(self._restart_interval)

    def _poll_updater(
        self,
        doc_snapshot: DocumentSnapshot,
        changes: Sequence[DocumentChange],
        read_time: Datetime,
    ):
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
        # Track the last document seen for cursor
        if isinstance(doc_snapshot, (list, tuple)) and len(doc_snapshot) > 0:
            with self._lock:
                self._last_document = doc_snapshot[-1]

    def start_periodic_restart(self, minutes: int) -> Self:
        """Start periodic restart every N minutes."""
        self._restart_interval = minutes
        self._start_restart_timer(minutes)
        return self

    def _start_restart_timer(self, minutes: int):
        self._cancel_restart_timer()
        self._restart_timer = threading.Timer(minutes * 60, self._restart)
        self._restart_timer.daemon = True
        self._restart_timer.start()

    def _cancel_restart_timer(self):
        if self._restart_timer is not None:
            self._restart_timer.cancel()
            self._restart_timer = None
