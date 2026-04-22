"""Canary watcher for detecting stale Firestore listeners.

Periodically writes a nonce to a dedicated Firestore document and verifies
that the long-lived on_snapshot watch delivers it back within a timeout.
This distinguishes a live watch from one that silently stopped receiving
updates (e.g. due to a proxy dropping idle TCP connections in Docker).
"""

import logging
import threading
import uuid
from collections.abc import Sequence
from datetime import UTC, datetime

from google.cloud.firestore import SERVER_TIMESTAMP
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client

_log = logging.getLogger(__name__)

_CANARY_COLLECTION = "listener_health"
_CANARY_DOC_ID = "sub_events"


class CanaryWatcher:
    """Attach a watch to a dedicated health document and verify round-trip delivery.

    Usage::

        canary = CanaryWatcher(db, timeout_seconds=120)
        canary_trigger = PeriodicTrigger(interval_seconds=300, callback=canary.send_canary)

        with canary, canary_trigger:
            # main loop
            if canary.is_stale():
                raise SystemExit("stale Firestore listener")
    """

    def __init__(self, db: Client, *, timeout_seconds: int = 120):
        self._db = db
        self._timeout_seconds = timeout_seconds
        self._doc_ref = db.collection(_CANARY_COLLECTION).document(_CANARY_DOC_ID)
        self._lock = threading.Lock()
        self._sent_nonce: str | None = None
        self._sent_at: datetime | None = None
        self._seen_nonce: str | None = None
        self._unsubscribe = None

    def __enter__(self) -> "CanaryWatcher":
        self._unsubscribe = self._doc_ref.on_snapshot(self._on_snapshot).unsubscribe
        _log.info("Canary watcher attached to %s/%s", _CANARY_COLLECTION, _CANARY_DOC_ID)
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        if self._unsubscribe is not None:
            self._unsubscribe()
            self._unsubscribe = None

    def _on_snapshot(
        self,
        doc_snapshot: Sequence[DocumentSnapshot],
        changes,
        read_time,
    ) -> None:
        for doc in doc_snapshot:
            payload = doc.to_dict() or {}
            nonce = payload.get("nonce")
            if nonce:
                with self._lock:
                    self._seen_nonce = str(nonce)
                _log.debug("Canary: observed nonce %s", nonce)

    def send_canary(self) -> None:
        """Write a fresh nonce to Firestore. Call this on a periodic timer."""
        nonce = str(uuid.uuid4())
        try:
            self._doc_ref.set({"nonce": nonce, "sentAt": SERVER_TIMESTAMP}, merge=True)
        except Exception:
            _log.exception("Canary: failed to write nonce to Firestore")
            return
        with self._lock:
            self._sent_nonce = nonce
            self._sent_at = datetime.now(UTC)
        _log.debug("Canary: sent nonce %s", nonce)

    def is_stale(self, *, now: datetime | None = None) -> bool:
        """Return True if a sent nonce was not observed by the watch within the timeout.

        Returns False when no canary has been sent yet (e.g. early startup),
        or when the most recently sent nonce has already been observed.
        """
        with self._lock:
            if self._sent_nonce is None:
                return False
            if self._sent_nonce == self._seen_nonce:
                return False
            if self._sent_at is None:
                return False
            elapsed = ((now or datetime.now(UTC)) - self._sent_at).total_seconds()
            if elapsed > self._timeout_seconds:
                _log.error(
                    "Canary: stale listener detected — sent nonce %s at %s (%.0fs ago), "
                    "last seen nonce is %r (timeout=%ss)",
                    self._sent_nonce,
                    self._sent_at.isoformat(),
                    elapsed,
                    self._seen_nonce,
                    self._timeout_seconds,
                )
                return True
            return False
