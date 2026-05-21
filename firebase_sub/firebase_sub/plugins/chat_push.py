import logging
from collections.abc import Generator, Mapping, Sequence
from typing import Any, Protocol, cast, runtime_checkable

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client

from firebase_sub.push_contract import (
    PUSH_EVENT_CHAT_MESSAGE_EVENT,
    PUSH_EVENT_CHAT_MESSAGE_GLOBAL,
    PUSH_PREFERENCE_FIELD,
)
from firebase_sub.send_push import (
    ValidPushEndpoint,
    build_chat_event_payload,
    build_chat_global_payload,
    endpoint_hash,
    push_endpoint_from_snapshot,
    send_chat_push,
)

_log = logging.getLogger(__name__)


@runtime_checkable
class ChatPushDbHandler(Protocol):
    @property
    def db(self) -> Client: ...

    def _users_with_push_preference(self, preference_field: str) -> set[str]: ...

    def _attendee_uids(self, poll_id: str) -> set[str]: ...

    def _event_chat_participant_uids(self, poll_id: str) -> set[str]: ...

    def _muted_event_chat_uids(
        self, poll_id: str, candidate_uids: set[str]
    ) -> set[str]: ...

    def query_active_push_endpoints_for_user(
        self, user_id: str
    ) -> Generator[DocumentSnapshot, None, None]: ...


def _payload_dict(payload: object) -> dict[str, object]:
    if not isinstance(payload, Mapping):
        return {}

    normalized: dict[str, object] = {}
    payload_map = cast(Mapping[object, object], payload)
    for key, value in payload_map.items():
        normalized[key if isinstance(key, str) else str(key)] = value
    return normalized


def _snapshot_payload(document: DocumentSnapshot) -> dict[str, object]:
    return _payload_dict(document.to_dict())


def _string_list(value: object) -> list[str]:
    if not isinstance(value, list | tuple | set):
        return []

    result: list[str] = []
    iterable_value = cast(Sequence[object] | set[object], value)
    for entry in iterable_value:
        if isinstance(entry, str):
            result.append(entry)
    return result


def process_chat_message_push(
    db_handler: ChatPushDbHandler,
    message_id: str,
    message_doc: DocumentSnapshot,
    *,
    dummy_run: bool = False,
) -> None:
    """Resolve recipients and send chat push notifications for one message."""
    message_data = _snapshot_payload(message_doc)
    scope_type_raw = message_data.get("scopeType")
    if scope_type_raw != "event":
        scope_type = "global"
        scope_id = "main"
    else:
        scope_type = "event"
        scope_id_raw = message_data.get("scopeId")
        scope_id = (
            scope_id_raw if isinstance(scope_id_raw, str) and scope_id_raw else "main"
        )

    author_uid_raw = message_data.get("uid")
    author_uid = author_uid_raw if isinstance(author_uid_raw, str) else ""
    display_name = message_data.get("displayName")
    fallback_name = message_data.get("name")
    sender_name = (
        display_name
        if isinstance(display_name, str) and display_name
        else (
            fallback_name
            if isinstance(fallback_name, str) and fallback_name
            else "Someone"
        )
    )
    text_value = message_data.get("text")
    alt_text_value = message_data.get("message")
    raw_text = (
        text_value
        if isinstance(text_value, str)
        else alt_text_value if isinstance(alt_text_value, str) else ""
    )
    body_text = raw_text[:100]

    preference_field = PUSH_PREFERENCE_FIELD[
        (
            PUSH_EVENT_CHAT_MESSAGE_EVENT
            if scope_type == "event"
            else PUSH_EVENT_CHAT_MESSAGE_GLOBAL
        )
    ]
    eligible_uids = db_handler._users_with_push_preference(preference_field)

    if scope_type == "event":
        attendees = db_handler._attendee_uids(scope_id)
        participants = db_handler._event_chat_participant_uids(scope_id)
        eligible_event_uids = attendees | participants
        eligible_uids &= eligible_event_uids
        muted_uids = db_handler._muted_event_chat_uids(scope_id, eligible_uids)
        eligible_uids -= muted_uids

    eligible_uids.discard(author_uid)
    if not eligible_uids:
        _log.info("No eligible recipients for chat push on message %s", message_id)
        return

    actions_ref = db_handler.db.collection("chat_push_actions").document(message_id)
    actions_snap = cast(DocumentSnapshot, cast(Any, actions_ref).get())
    actions_data = _snapshot_payload(actions_snap)
    already_delivered_eps = set(_string_list(actions_data.get("delivered_endpoints")))

    if scope_type == "event":
        payload = build_chat_event_payload(
            message_id=message_id,
            poll_id=scope_id,
            sender_name=sender_name,
            body_text=body_text,
        )
    else:
        payload = build_chat_global_payload(
            message_id=message_id,
            sender_name=sender_name,
            body_text=body_text,
        )

    endpoints: list[ValidPushEndpoint] = []
    for uid in eligible_uids:
        for endpoint_doc in db_handler.query_active_push_endpoints_for_user(uid):
            raw_ep = push_endpoint_from_snapshot(endpoint_doc)
            if raw_ep is None:
                continue
            valid_ep = raw_ep.validated()
            if valid_ep is None:
                continue
            if endpoint_hash(valid_ep) in already_delivered_eps:
                continue
            endpoints.append(valid_ep)

    if not endpoints:
        _log.info("No remaining undelivered endpoints for message %s", message_id)
        return

    send_chat_push(
        endpoints=endpoints,
        payload=payload,
        actions_ref=actions_ref,
        actions_doc_exists=actions_snap.exists,
        scope_type=scope_type,
        scope_id=scope_id,
        dummy_run=dummy_run,
    )
