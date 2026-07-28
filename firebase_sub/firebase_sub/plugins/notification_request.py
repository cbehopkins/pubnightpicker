import hashlib
import json
from collections.abc import Mapping
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.my_types import RetryableServiceError, TerminalServiceError
from firebase_sub.runtime.idempotency import IdempotencyMetadata, IdempotencyStore
from firebase_sub.runtime.service_adapter import (
    AdapterBackedEventPlugin,
    ServiceAdapter,
    ServiceContext,
    ServiceResult,
)


class _NotificationAckIdempotencyStore(IdempotencyStore):
    """Idempotency metadata persistence for notification request processing.

    State is stored under a dedicated metadata key in the ack document so
    request/ack ownership remains unchanged while lifecycle metadata converges.
    """

    _ROOT_FIELD = "_service_idempotency"

    def __init__(self, *, db: Any, ack_collection_name: str) -> None:
        self._db = db
        self._ack_collection_name = ack_collection_name

    def is_done(self, *, entity_id: str, dedupe_key: str) -> bool:
        entry = self._entry(entity_id=entity_id, dedupe_key=dedupe_key)
        return entry.get("state") == "done"

    def mark_done(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata | None = None,
    ) -> None:
        existing = self._entry(entity_id=entity_id, dedupe_key=dedupe_key)
        attempt_count = self._attempt_count(existing)
        payload = metadata or IdempotencyMetadata.attempted_now(
            attempt_count=attempt_count,
        )
        self._write_entry(
            entity_id=entity_id,
            dedupe_key=dedupe_key,
            entry={
                "state": "done",
                "attempt_count": payload.attempt_count,
                "last_attempted_at": payload.last_attempted_at,
                "last_error_code": None,
                "last_error_message": None,
            },
        )

    def mark_retryable_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        self._write_entry(
            entity_id=entity_id,
            dedupe_key=dedupe_key,
            entry={
                "state": "retryable_failed",
                "attempt_count": metadata.attempt_count,
                "last_attempted_at": metadata.last_attempted_at,
                "last_error_code": metadata.last_error_code,
                "last_error_message": metadata.last_error_message,
            },
        )

    def mark_terminal_failure(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        metadata: IdempotencyMetadata,
    ) -> None:
        self._write_entry(
            entity_id=entity_id,
            dedupe_key=dedupe_key,
            entry={
                "state": "terminal_failed",
                "attempt_count": metadata.attempt_count,
                "last_attempted_at": metadata.last_attempted_at,
                "last_error_code": metadata.last_error_code,
                "last_error_message": metadata.last_error_message,
            },
        )

    def attempt_count(self, *, entity_id: str, dedupe_key: str) -> int:
        return self._attempt_count(
            self._entry(entity_id=entity_id, dedupe_key=dedupe_key)
        )

    def _entry(self, *, entity_id: str, dedupe_key: str) -> dict[str, Any]:
        doc = self._ack_document(entity_id=entity_id)
        snapshot = cast(DocumentSnapshot, cast(Any, doc).get())
        payload = cast(dict[str, Any] | None, snapshot.to_dict()) or {}
        root = payload.get(self._ROOT_FIELD)
        if not isinstance(root, Mapping):
            return {}
        entry = cast(Mapping[str, Any], root).get(dedupe_key)
        if not isinstance(entry, Mapping):
            return {}
        return dict(cast(Mapping[str, Any], entry))

    @staticmethod
    def _attempt_count(entry: Mapping[str, Any]) -> int:
        raw_value = entry.get("attempt_count", 0)
        if isinstance(raw_value, int) and raw_value >= 0:
            return raw_value + 1
        return 1

    def _write_entry(
        self,
        *,
        entity_id: str,
        dedupe_key: str,
        entry: dict[str, Any],
    ) -> None:
        doc = self._ack_document(entity_id=entity_id)
        cast(Any, doc).set(
            {
                self._ROOT_FIELD: {
                    dedupe_key: entry,
                }
            },
            merge=True,
        )

    def _ack_document(self, *, entity_id: str) -> Any:
        return self._db.collection(self._ack_collection_name).document(entity_id)


class _NotificationRequestServiceAdapter(ServiceAdapter):
    """Adapter implementation for notification request processing."""

    def __init__(
        self,
        *,
        notification_mirror: NotificationAckMirrorHandler,
        notification_push_test: NotificationPushTestHandler,
    ) -> None:
        self._notification_mirror = notification_mirror
        self._notification_push_test = notification_push_test
        self._idempotency_store = _NotificationAckIdempotencyStore(
            db=notification_mirror.db,
            ack_collection_name=notification_mirror.ack_collection_name,
        )

    def name(self) -> str:
        return "notification_request_listener"

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        if envelope.doc is None:
            return None
        if envelope.type == EventType.PUSH_TEST:
            if not self._notification_push_test.is_push_test_request(envelope.doc):
                return None
        elif envelope.type == EventType.PUSH:
            if self._notification_push_test.is_push_test_request(envelope.doc):
                return None
        else:
            return None

        return ServiceContext(
            envelope=envelope,
            entity_id=envelope.document_id() or "",
            dedupe_key=_notification_dedupe_key(envelope),
            idempotency_store=self._idempotency_store,
        )

    def execute(self, context: ServiceContext) -> ServiceResult:
        envelope = context.envelope
        if envelope.doc is None:
            return ServiceResult(success=True)
        if context.idempotency_store is None:
            raise RuntimeError("notification adapter requires idempotency store")

        if context.idempotency_store.is_done(
            entity_id=context.entity_id,
            dedupe_key=context.dedupe_key,
        ):
            return ServiceResult(success=True)

        attempt_count = self._idempotency_store.attempt_count(
            entity_id=context.entity_id,
            dedupe_key=context.dedupe_key,
        )
        try:
            if envelope.type == EventType.PUSH_TEST:
                # The push-test handler persists both ack state and request cleanup inline.
                self._notification_push_test.handle_request_document(envelope.doc)
                return ServiceResult(success=True)
            if envelope.type == EventType.PUSH:
                # The mirror handler persists the ack document inline.
                self._notification_mirror.mirror_request_document(envelope.doc)
                return ServiceResult(success=True)
            raise TerminalServiceError(
                f"notification_request adapter got unsupported event type {envelope.type}"
            )
        except RetryableServiceError as exc:
            context.idempotency_store.mark_retryable_failure(
                entity_id=context.entity_id,
                dedupe_key=context.dedupe_key,
                metadata=IdempotencyMetadata.attempted_now(
                    attempt_count=attempt_count,
                    last_error_code=type(exc).__name__,
                    last_error_message=str(exc),
                ),
            )
            raise
        except TerminalServiceError as exc:
            context.idempotency_store.mark_terminal_failure(
                entity_id=context.entity_id,
                dedupe_key=context.dedupe_key,
                metadata=IdempotencyMetadata.attempted_now(
                    attempt_count=attempt_count,
                    last_error_code=type(exc).__name__,
                    last_error_message=str(exc),
                ),
            )
            raise
        except Exception as exc:
            context.idempotency_store.mark_retryable_failure(
                entity_id=context.entity_id,
                dedupe_key=context.dedupe_key,
                metadata=IdempotencyMetadata.attempted_now(
                    attempt_count=attempt_count,
                    last_error_code=type(exc).__name__,
                    last_error_message=str(exc),
                ),
            )
            raise RetryableServiceError(
                "notification_request adapter failed to process request document"
            ) from exc

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        # Notification handlers persist request/ack state inline, and commit records
        # shared idempotency completion metadata for this dedupe key.
        if context.idempotency_store is not None:
            attempt_count = self._idempotency_store.attempt_count(
                entity_id=context.entity_id,
                dedupe_key=context.dedupe_key,
            )
            context.idempotency_store.mark_done(
                entity_id=context.entity_id,
                dedupe_key=context.dedupe_key,
                metadata=IdempotencyMetadata.attempted_now(
                    attempt_count=attempt_count,
                ),
            )
        del result
        return


def _notification_dedupe_key(envelope: EventEnvelope) -> str:
    doc_id = envelope.document_id() or ""
    payload = _snapshot_payload(envelope.doc)
    payload_json = json.dumps(payload, sort_keys=True, default=str)
    payload_hash = hashlib.sha256(payload_json.encode("utf-8")).hexdigest()[:16]
    return f"notification:{envelope.type}:{doc_id}:{payload_hash}"


def _snapshot_payload(document: DocumentSnapshot | None) -> dict[str, Any]:
    if document is None:
        return {}
    to_dict = getattr(document, "to_dict", None)
    if not callable(to_dict):
        return {}
    payload = cast(dict[str, Any] | None, to_dict())
    return payload or {}


class NotificationRequestListenerPlugin(AdapterBackedEventPlugin):
    """Listener plugin for notification request documents.

    Completion state is written inline by the notification handlers themselves.
    ``mark_done()`` remains as the lifecycle acknowledgment hook and is a no-op.
    """

    def __init__(
        self,
        *,
        notification_mirror: NotificationAckMirrorHandler,
        notification_push_test: NotificationPushTestHandler,
    ) -> None:
        self._notification_adapter = _NotificationRequestServiceAdapter(
            notification_mirror=notification_mirror,
            notification_push_test=notification_push_test,
        )
        super().__init__(self._notification_adapter)
        self._notification_mirror = notification_mirror
        self._notification_push_test = notification_push_test
