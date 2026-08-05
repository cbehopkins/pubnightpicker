from collections.abc import Mapping
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.my_types import TerminalServiceError
from firebase_sub.plugins.protocols import NewPollDbHandler
from firebase_sub.runtime.action_execution import PollActionExecutor
from firebase_sub.runtime.idempotency import IdempotencyStore
from firebase_sub.runtime.service_adapter import (
    AdapterBackedEventPlugin,
    ServiceAdapter,
    ServiceContext,
    ServiceResult,
)


class _NewPollServiceAdapter(ServiceAdapter):
    """Adapter implementation for new-poll notification processing."""

    def __init__(
        self,
        *,
        db_handler: NewPollDbHandler,
        action_executor: PollActionExecutor,
        idempotency_store: IdempotencyStore,
    ) -> None:
        self._db_handler = db_handler
        self._action_executor = action_executor
        self._idempotency_store = idempotency_store

    def name(self) -> str:
        return "new_poll_listener"

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        if envelope.doc is None or envelope.type != EventType.NEW_POLL:
            return None

        poll_id = envelope.document_id()
        if poll_id is None:
            return None

        action_document = _action_document(self._db_handler, poll_id)
        action_snapshot = _snapshot_get(action_document)
        action_dict = action_snapshot.to_dict() or {}
        open_action_key = poll_id

        if self._idempotency_store.is_done(
            entity_id=poll_id,
            dedupe_key=open_action_key,
        ):
            return None

        return ServiceContext(
            envelope=envelope,
            entity_id=poll_id,
            dedupe_key=f"open_poll:{poll_id}",
            extras={
                "poll_date": _poll_date(envelope=envelope, db_handler=self._db_handler),
                "action_dict": action_dict,
            },
        )

    def execute(self, context: ServiceContext) -> ServiceResult:
        poll_id = context.entity_id
        open_action_key = poll_id
        action_dict = cast(dict[str, object], context.extras.get("action_dict", {}))
        poll_date = cast(str, context.extras.get("poll_date", ""))

        self._action_executor.action_event(
            action_dict=action_dict,
            action_key=open_action_key,
            poll_id=poll_id,
            poll_date=poll_date,
        )
        return ServiceResult(success=True)

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        del result
        poll_id = context.entity_id
        self._idempotency_store.mark_done(
            entity_id=poll_id,
            dedupe_key=poll_id,
        )


class NewPollListenerPlugin(AdapterBackedEventPlugin):
    """Listener plugin that processes NEW_POLL events for open polls.

    This adapter-backed listener maps runtime lifecycle phases onto service
    contracts:
    - `prepare`: checks if poll notifications are still pending via IdempotencyStore
    - `execute`: dispatches notification side effects via PollActionExecutor
    - `commit`: marks completion in IdempotencyStore
    """

    def __init__(
        self,
        *,
        db_handler: NewPollDbHandler,
        action_executor: PollActionExecutor,
        idempotency_store: IdempotencyStore,
    ) -> None:
        self._new_poll_adapter = _NewPollServiceAdapter(
            db_handler=db_handler,
            action_executor=action_executor,
            idempotency_store=idempotency_store,
        )
        super().__init__(self._new_poll_adapter)

    def name(self) -> str:
        return "new_poll_listener"

    def on_registered(self) -> None:
        return

    def on_unregistered(self) -> None:
        return


class MissingPollDateError(TerminalServiceError):
    """Raised when a NEW_POLL event has no resolvable poll date."""


def _poll_date(*, envelope: EventEnvelope, db_handler: NewPollDbHandler) -> str:
    poll_id = envelope.document_id()
    if poll_id is None:
        raise MissingPollDateError("NEW_POLL event missing document id")

    if envelope.doc is not None:
        to_dict = getattr(envelope.doc, "to_dict", None)
        if callable(to_dict):
            doc_payload = to_dict()
            if isinstance(doc_payload, Mapping):
                payload_map = cast(Mapping[str, object], doc_payload)
                raw_date_obj = payload_map.get("date", None)
                if isinstance(raw_date_obj, str):
                    return raw_date_obj

    # Fallback for snapshot-like docs where event payload is unavailable.
    poll_dict = db_handler.poll_repo.get_poll(poll_id)
    if poll_dict is not None:
        poll_date = poll_dict.get("date")
        if isinstance(poll_date, str):
            return poll_date

    raise MissingPollDateError(
        f"NEW_POLL poll '{poll_id}' is missing required date"
    )


def _action_document(db_handler: NewPollDbHandler, poll_id: str) -> object:
    return db_handler.db.collection("open_actions").document(poll_id)


def _snapshot_get(document_ref: object) -> DocumentSnapshot:
    raw_snapshot = cast(Any, document_ref).get()
    if isinstance(raw_snapshot, DocumentSnapshot):
        return raw_snapshot
    if raw_snapshot is not None and hasattr(raw_snapshot, "to_dict"):
        return cast(DocumentSnapshot, raw_snapshot)
    raise TypeError("Expected synchronous DocumentSnapshot from Firestore get()")
