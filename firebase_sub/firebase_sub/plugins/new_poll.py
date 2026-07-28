from collections.abc import Mapping
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionMan
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import NewPollDbHandler
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
        action_manager: ActionMan,
    ) -> None:
        self._db_handler = db_handler
        self._action_manager = action_manager

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

        if not self._action_manager.filter(
            action_dict=action_dict,
            action_key=open_action_key,
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

        self._action_manager.action_event(
            action_dict=action_dict,
            action_key=open_action_key,
            poll_id=poll_id,
            poll_date=poll_date,
        )
        return ServiceResult(success=True)

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        del result
        poll_id = context.entity_id
        action_document = _action_document(self._db_handler, poll_id)
        action_snapshot = _snapshot_get(action_document)
        action_dict = action_snapshot.to_dict() or {}
        new_action_dict = self._action_manager.mark_done(
            action_dict=action_dict,
            action_key=poll_id,
        )
        _document_set(action_document, new_action_dict, merge=True)


class NewPollListenerPlugin(AdapterBackedEventPlugin):
    """Listener plugin that processes NEW_POLL events for open polls.

    Implements EventPlugin contract with gated execution:
    - filter: checks if the poll action needs to run (via ActionMan)
    - handle: sends notifications (emails and push)
    - mark_done: updates action document state
    """

    def __init__(
        self,
        *,
        db_handler: NewPollDbHandler,
        action_manager: ActionMan,
    ) -> None:
        self._new_poll_adapter = _NewPollServiceAdapter(
            db_handler=db_handler,
            action_manager=action_manager,
        )
        super().__init__(self._new_poll_adapter)

    def name(self) -> str:
        return "new_poll_listener"

    def on_registered(self) -> None:
        return

    def on_unregistered(self) -> None:
        return


def _poll_date(*, envelope: EventEnvelope, db_handler: NewPollDbHandler) -> str:
    if envelope.doc is not None:
        to_dict = getattr(envelope.doc, "to_dict", None)
        if callable(to_dict):
            doc_payload = to_dict()
            if isinstance(doc_payload, Mapping):
                raw_date = doc_payload.get("date")
                if isinstance(raw_date, str):
                    return raw_date

    poll_id = envelope.document_id()
    if poll_id is None:
        return ""

    # Fallback for snapshot-like docs where event payload is unavailable.
    poll_dict = db_handler.poll_repo.get_poll(poll_id)
    if isinstance(poll_dict, Mapping):
        raw_date = poll_dict.get("date")
        if isinstance(raw_date, str):
            return raw_date
    return ""


def _action_document(db_handler: NewPollDbHandler, poll_id: str) -> object:
    return db_handler.db.collection("open_actions").document(poll_id)


def _snapshot_get(document_ref: object) -> DocumentSnapshot:
    raw_snapshot = cast(Any, document_ref).get()
    if isinstance(raw_snapshot, DocumentSnapshot):
        return raw_snapshot
    if raw_snapshot is not None and hasattr(raw_snapshot, "to_dict"):
        return cast(DocumentSnapshot, raw_snapshot)
    raise TypeError("Expected synchronous DocumentSnapshot from Firestore get()")


def _document_set(
    document_ref: object,
    payload: Mapping[str, object],
    *,
    merge: bool,
) -> None:
    cast(Any, document_ref).set(payload, merge=merge)
