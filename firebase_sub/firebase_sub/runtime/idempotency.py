"""Shared idempotency contracts for concern-specific stores.

This module defines a common interface that listeners can rely on while keeping
physical storage separated per concern (open actions, complete actions, chat
push actions, request/ack docs, and request status collections).
"""

import logging
from collections.abc import Callable, Iterable, Mapping
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Protocol, cast

from firebase_sub.action_track import ActionMan

_log = logging.getLogger(__name__)


@dataclass(slots=True)
class IdempotencyMetadata:
    """Common metadata fields for idempotency records.

    Stores can persist only the fields they support, but this shape defines the
    shared vocabulary for attempt/error tracking.
    """

    attempt_count: int = 0
    last_attempted_at: datetime | None = None
    last_error_code: str | None = None
    last_error_message: str | None = None

    @classmethod
    def attempted_now(
        cls,
        *,
        attempt_count: int,
        last_error_code: str | None = None,
        last_error_message: str | None = None,
    ) -> "IdempotencyMetadata":
        """Create metadata populated for a single processing attempt."""
        return cls(
            attempt_count=attempt_count,
            last_attempted_at=datetime.now(UTC),
            last_error_code=last_error_code,
            last_error_message=last_error_message,
        )


class IdempotencyStore(Protocol):
    """Common contract for concern-specific idempotency persistence.

    The store implementation decides how keys map to physical document layout,
    but callers interact with a consistent read/check/mark_done API.
    """

    def is_done(self, *, entity_id: str, dedupe_key: str) -> bool:
        """Return True if the dedupe key is already completed for this entity."""
        ...

    def mark_done(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata | None = None,
    ) -> None:
        """Persist completion for an entity/dedupe key pair."""
        ...

    def mark_retryable_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        """Persist retryable failure metadata for observability/attempt tracking."""
        ...

    def mark_terminal_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        """Persist terminal failure metadata for an unrecoverable outcome."""
        ...


class _SnapshotLike(Protocol):
    def to_dict(self) -> Mapping[str, object] | None: ...


class FirestoreActionManIdempotencyStore(IdempotencyStore):
    """IdempotencyStore backed by Firestore action docs and ActionMan semantics.

    This adapter preserves current ActionMan behavior while exposing the shared
    IdempotencyStore contract. It is intended as an incremental migration path.
    """

    def __init__(
        self,
        *,
        action_manager: ActionMan,
        document_for_entity: Callable[[str], object],
    ) -> None:
        self._action_manager = action_manager
        self._document_for_entity = document_for_entity

    def is_done(self, *, entity_id: str, dedupe_key: str) -> bool:
        action_dict = self._read_action_dict(entity_id)
        return not self._action_manager.filter(
            action_dict=action_dict,
            action_key=dedupe_key,
        )

    def mark_done(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata | None = None,
    ) -> None:
        del metadata
        action_dict = self._read_action_dict(entity_id)
        new_action_dict = self._action_manager.mark_done(
            action_dict=action_dict,
            action_key=dedupe_key,
        )
        self._write_action_dict(entity_id, new_action_dict)

    def mark_retryable_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        _log.debug(
            "Idempotency retryable failure entity_id=%s dedupe_key=%s attempt_count=%s error=%s",
            entity_id,
            dedupe_key,
            metadata.attempt_count,
            metadata.last_error_code,
        )

    def mark_terminal_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        _log.debug(
            "Idempotency terminal failure entity_id=%s dedupe_key=%s attempt_count=%s error=%s",
            entity_id,
            dedupe_key,
            metadata.attempt_count,
            metadata.last_error_code,
        )

    def _read_action_dict(self, entity_id: str) -> dict[str, set[str]]:
        document = self._document_for_entity(entity_id)
        snapshot = _snapshot_get(document)
        payload = snapshot.to_dict() or {}
        normalized: dict[str, set[str]] = {}
        for key, value in payload.items():
            if isinstance(value, Iterable) and not isinstance(value, str):
                iter_values = cast(Iterable[object], value)
                normalized[str(key)] = {str(item) for item in iter_values}
        return normalized

    def _write_action_dict(self, entity_id: str, payload: dict[str, set[str]]) -> None:
        document = self._document_for_entity(entity_id)
        serialized = {key: sorted(values) for key, values in payload.items()}
        _document_set(document, serialized, merge=True)


def _snapshot_get(document_ref: object) -> _SnapshotLike:
    raw_snapshot = cast(Any, document_ref).get()
    if raw_snapshot is not None and hasattr(raw_snapshot, "to_dict"):
        return cast(_SnapshotLike, raw_snapshot)
    raise TypeError("Expected synchronous snapshot-like object from get()")


def _document_set(
    document_ref: object,
    payload: dict[str, list[str]],
    *,
    merge: bool,
) -> None:
    cast(Any, document_ref).set(payload, merge=merge)
