"""Shared idempotency contracts for concern-specific stores.

This module defines a common interface that listeners can rely on while keeping
physical storage separated per concern (open actions, complete actions, chat
push actions, request/ack docs, and request status collections).
"""

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Protocol


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

    def mark_done(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata | None = None,
    ) -> None:
        """Persist completion for an entity/dedupe key pair."""

    def mark_retryable_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        """Persist retryable failure metadata for observability/attempt tracking."""

    def mark_terminal_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        """Persist terminal failure metadata for an unrecoverable outcome."""
