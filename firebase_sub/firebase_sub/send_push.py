import hashlib
import json
import logging
import os
from collections.abc import Callable, Iterable
from dataclasses import dataclass
from datetime import UTC, date, datetime
from typing import Any, Literal, Protocol, TypedDict

from google.cloud.firestore import SERVER_TIMESTAMP
from google.cloud.firestore_v1 import ArrayUnion
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from pywebpush import WebPushException, webpush

from firebase_sub.action_track import CallbackExceptionRetry
from firebase_sub.constants import ADMIN_EMAIL_ADDR
from firebase_sub.my_types import PollDocument, VenueDocument
from firebase_sub.push_contract import (
    PUSH_EVENT_CHAT_MESSAGE_EVENT,
    PUSH_EVENT_CHAT_MESSAGE_GLOBAL,
    PUSH_EVENT_DIAGNOSTIC_PUSH_TEST,
    PUSH_EVENT_POLL_COMPLETED,
    PUSH_EVENT_POLL_OPENED,
    PUSH_EVENT_POLL_RESCHEDULED,
)
from firebase_sub.send_email import resolve_payloads

_log = logging.getLogger("SendPush")
_DEFAULT_BASE_URL = "https://ampubnight.org/"
_TOPIC_MAX_LEN = 32
_TTL_MIN_SECONDS = 60 * 60
_TTL_MAX_SECONDS = 5 * 24 * 60 * 60


@dataclass(frozen=True)
class PushEndpoint:
    endpoint: str
    p256dh: str | None
    auth: str | None
    user_id: str | None
    document: DocumentSnapshot

    def validated(self) -> "ValidPushEndpoint | None":
        if self.p256dh is None or self.auth is None:
            return None
        return ValidPushEndpoint(
            endpoint=self.endpoint,
            p256dh=self.p256dh,
            auth=self.auth,
            user_id=self.user_id,
            document=self.document,
        )


@dataclass(frozen=True)
class ValidPushEndpoint:
    endpoint: str
    p256dh: str
    auth: str
    user_id: str | None
    document: DocumentSnapshot


@dataclass(frozen=True)
class PushDeliveryResult:
    delivered: int = 0
    invalid: int = 0
    retryable_failures: int = 0


JSONPrimitive = str | int | float | bool | None
JSONValue = JSONPrimitive | list["JSONValue"] | dict[str, "JSONValue"]


class _BasePushPayload(TypedDict):
    title: str
    body: str
    url: str
    tag: str
    sentAt: str


class OpenPushPayload(_BasePushPayload):
    eventType: Literal["poll_opened"]
    pollId: str


class DiagnosticPushPayload(_BasePushPayload):
    eventType: Literal["diagnostic_push_test"]
    requestedValue: JSONValue


class CompletePushPayload(_BasePushPayload):
    eventType: Literal["poll_completed", "poll_rescheduled"]
    pollId: str


class ChatPushPayload(_BasePushPayload):
    eventType: Literal["chat_message_sent_global", "chat_message_sent_event"]
    messageId: str
    pollId: str | None


PushPayload = (
    OpenPushPayload | DiagnosticPushPayload | CompletePushPayload | ChatPushPayload
)


class ChatPushActionsRef(Protocol):
    def set(self, document_data: dict[str, object], merge: bool = False) -> Any: ...

    def update(self, field_updates: dict[str, object]) -> Any: ...


def _base_url() -> str:
    return os.getenv("PUBNIGHTPICKER_WEB_BASE_URL", _DEFAULT_BASE_URL).rstrip("/")


def _vapid_private_key() -> str:
    key = os.getenv("WEB_PUSH_VAPID_PRIVATE_KEY", "")
    if not key:
        raise CallbackExceptionRetry("WEB_PUSH_VAPID_PRIVATE_KEY is not set")
    return key


def _vapid_claims() -> dict[str, str | int]:
    raw_subject = os.getenv("WEB_PUSH_VAPID_SUBJECT", ADMIN_EMAIL_ADDR).strip()
    if not raw_subject:
        raise CallbackExceptionRetry(
            "WEB_PUSH_VAPID_SUBJECT is empty; expected email or URL"
        )

    if raw_subject.startswith(("mailto:", "https://", "http://")):
        return {"sub": raw_subject}

    if "@" in raw_subject:
        return {"sub": f"mailto:{raw_subject}"}

    raise CallbackExceptionRetry(
        "WEB_PUSH_VAPID_SUBJECT must be an email, mailto:, or http(s) URL"
    )


def _topic_for_poll_id(poll_id: str) -> str:
    sanitized = "".join(
        character if character.isalnum() or character in "-_" else "-"
        for character in poll_id
    )
    if not sanitized:
        return hashlib.sha256(poll_id.encode("utf-8")).hexdigest()[:_TOPIC_MAX_LEN]
    if len(sanitized) <= _TOPIC_MAX_LEN:
        return sanitized
    digest = hashlib.sha256(sanitized.encode("utf-8")).hexdigest()
    prefix_len = _TOPIC_MAX_LEN - 1 - 12
    return f"{sanitized[:prefix_len]}-{digest[:12]}"


def _ttl_for_poll_date(poll_date: str, *, now: datetime | None = None) -> int:
    now_utc = now or datetime.now(UTC)
    try:
        event_day = date.fromisoformat(poll_date)
    except ValueError:
        _log.warning(
            "Failed to parse poll date %r for push TTL. Falling back to minimum TTL.",
            poll_date,
        )
        return _TTL_MIN_SECONDS

    # Policy: midnight UTC on event day, clamped to [1h, 5d].
    event_cutoff = datetime.combine(event_day, datetime.min.time(), tzinfo=UTC)
    ttl_seconds = int((event_cutoff - now_utc).total_seconds())
    return max(_TTL_MIN_SECONDS, min(_TTL_MAX_SECONDS, ttl_seconds))


def _document_user_id(document: DocumentSnapshot) -> str | None:
    reference = document.reference

    path = getattr(reference, "path", None)
    if isinstance(path, str):
        path_parts = path.split("/")
        if len(path_parts) >= 4 and path_parts[-2] == "push_endpoints":
            user_id = path_parts[-3]
            if user_id:
                return user_id

    parent_collection = getattr(reference, "parent", None)
    parent_document = getattr(parent_collection, "parent", None)
    parent_id = getattr(parent_document, "id", None)
    if isinstance(parent_id, str) and parent_id:
        return parent_id

    return None


def _document_set(
    document_ref: object, payload: dict[str, object], *, merge: bool
) -> None:
    getattr(document_ref, "set")(payload, merge=merge)


def _push_endpoint_from_snapshot(document: DocumentSnapshot) -> PushEndpoint | None:
    payload = document.to_dict()
    if payload is None:
        return None
    endpoint = payload.get("endpoint")
    if not endpoint:
        return None
    return PushEndpoint(
        endpoint=endpoint,
        p256dh=payload.get("p256dh"),
        auth=payload.get("auth"),
        user_id=_document_user_id(document),
        document=document,
    )


def _deactivate_endpoint(endpoint: PushEndpoint | ValidPushEndpoint) -> None:
    _document_set(
        endpoint.document.reference,
        {
            "active": False,
            "disabledAt": SERVER_TIMESTAMP,
            "lastSeenAt": SERVER_TIMESTAMP,
        },
        merge=True,
    )


def _send_to_endpoint(
    endpoint: ValidPushEndpoint,
    payload: PushPayload,
    *,
    ttl_seconds: int,
    topic: str,
    dummy_run: bool,
) -> None:
    if dummy_run:
        _log.info(
            "Dummy web push to %s: %s (ttl=%s topic=%s)",
            endpoint.endpoint,
            payload,
            ttl_seconds,
            topic,
        )
        return
    webpush(
        subscription_info={
            "endpoint": endpoint.endpoint,
            "keys": {
                "p256dh": endpoint.p256dh,
                "auth": endpoint.auth,
            },
        },
        data=json.dumps(payload),
        ttl=ttl_seconds,
        headers={"Topic": topic},
        vapid_private_key=_vapid_private_key(),
        vapid_claims=_vapid_claims(),
    )


def _deliver_pushes(
    *,
    payload: PushPayload,
    ttl_seconds: int,
    topic: str,
    endpoints_src: Callable[[], Iterable[DocumentSnapshot]],
    dummy_run: bool,
) -> PushDeliveryResult:
    delivered = 0
    invalid = 0
    retryable_failures = 0

    for document in endpoints_src():
        raw_endpoint = _push_endpoint_from_snapshot(document)
        if raw_endpoint is None:
            continue
        endpoint = raw_endpoint.validated()
        if endpoint is None:
            _log.warning(
                "Deactivating malformed push endpoint with missing keys for user %s",
                raw_endpoint.user_id,
            )
            _deactivate_endpoint(raw_endpoint)
            invalid += 1
            continue
        try:
            _send_to_endpoint(
                endpoint,
                payload,
                ttl_seconds=ttl_seconds,
                topic=topic,
                dummy_run=dummy_run,
            )
            delivered += 1
        except WebPushException as exc:
            status_code = getattr(getattr(exc, "response", None), "status_code", None)
            response_text = getattr(getattr(exc, "response", None), "text", None)
            if status_code in {404, 410}:
                _log.info(
                    "Deactivating stale push endpoint for user %s", endpoint.user_id
                )
                _deactivate_endpoint(raw_endpoint)
                invalid += 1
                continue
            if status_code in {400, 401, 403}:
                _log.warning(
                    "Deactivating push endpoint after non-retryable failure: "
                    "status=%s user=%s endpoint=%s response=%s",
                    status_code,
                    endpoint.user_id,
                    endpoint.endpoint,
                    response_text,
                )
                _deactivate_endpoint(endpoint)
                invalid += 1
                continue
            retryable_failures += 1
            _log.exception(
                "Retryable web push failure for user %s status=%s response=%s",
                endpoint.user_id,
                status_code,
                response_text,
            )
        except Exception:
            retryable_failures += 1
            _log.exception(
                "Unexpected web push failure for user %s endpoint=%s",
                endpoint.user_id,
                endpoint.endpoint,
            )

    result = PushDeliveryResult(
        delivered=delivered,
        invalid=invalid,
        retryable_failures=retryable_failures,
    )
    _log.info(
        "Push delivery result: delivered=%s invalid=%s retryable_failures=%s",
        result.delivered,
        result.invalid,
        result.retryable_failures,
    )
    if result.retryable_failures:
        raise CallbackExceptionRetry(
            "Retryable push failures "
            f"(retryable={result.retryable_failures}, delivered={result.delivered}, invalid={result.invalid})"
        )
    return result


def _build_open_payload(poll_id: str) -> OpenPushPayload:
    return {
        "eventType": PUSH_EVENT_POLL_OPENED,
        "pollId": poll_id,
        "title": "Pub Night voting opened",
        "body": "Voting has opened for this week's pub night. Tap to open the active polls page.",
        "url": f"{_base_url()}/active_polls",
        "tag": f"poll-open:{poll_id}",
        "sentAt": datetime.now(UTC).isoformat(),
    }


def _build_diagnostic_payload(
    user_id: str, request_value: JSONValue
) -> DiagnosticPushPayload:
    return {
        "eventType": PUSH_EVENT_DIAGNOSTIC_PUSH_TEST,
        "title": "Push diagnostics",
        "body": "This is a test push notification from admin diagnostics.",
        "url": f"{_base_url()}/preferences",
        "tag": f"push-diagnostic:{user_id}",
        "requestedValue": request_value,
        "sentAt": datetime.now(UTC).isoformat(),
    }


def _build_complete_payload(
    poll_id: str,
    poll_dict: PollDocument,
    pub_dict: dict[str, VenueDocument],
    previously_actioned: bool,
) -> CompletePushPayload:
    poll, selected_venue, restaurant_venue = resolve_payloads(
        poll_dict=poll_dict,
        pub_dict=pub_dict,
    )
    event_type = (
        PUSH_EVENT_POLL_RESCHEDULED
        if previously_actioned
        else PUSH_EVENT_POLL_COMPLETED
    )
    title = (
        f"Pub Night rescheduled: {selected_venue.name}"
        if previously_actioned
        else f"Pub Night @ {selected_venue.name}"
    )
    restaurant_suffix = (
        f" Pre-pub meal at {restaurant_venue.name}." if restaurant_venue else ""
    )
    time_suffix = f" Meet at {poll.restaurant_time}." if poll.restaurant_time else ""
    return {
        "eventType": event_type,
        "pollId": poll_id,
        "title": title,
        "body": f"{poll.date}: {selected_venue.name}.{restaurant_suffix}{time_suffix}".strip(),
        "url": f"{_base_url()}/current_events",
        "tag": f"poll-complete:{poll_id}",
        "sentAt": datetime.now(UTC).isoformat(),
    }


def send_poll_open_push(
    *,
    poll_id: str,
    poll_date: str,
    previously_actioned: bool,
    endpoints_src: Callable[[], Iterable[DocumentSnapshot]],
    dummy_run: bool = False,
) -> PushDeliveryResult:
    _ = previously_actioned
    payload = _build_open_payload(poll_id)
    ttl_seconds = _ttl_for_poll_date(poll_date)
    topic = _topic_for_poll_id(poll_id)
    return _deliver_pushes(
        payload=payload,
        ttl_seconds=ttl_seconds,
        topic=topic,
        endpoints_src=endpoints_src,
        dummy_run=dummy_run,
    )


def send_poll_complete_push(
    poll_id: str,
    poll_dict: PollDocument,
    pub_dict: dict[str, VenueDocument],
    *,
    previously_actioned: bool,
    endpoints_src: Callable[[], Iterable[DocumentSnapshot]],
    dummy_run: bool = False,
) -> PushDeliveryResult:
    payload = _build_complete_payload(
        poll_id=poll_id,
        poll_dict=poll_dict,
        pub_dict=pub_dict,
        previously_actioned=previously_actioned,
    )
    ttl_seconds = _ttl_for_poll_date(poll_dict["date"])
    topic = _topic_for_poll_id(poll_id)
    return _deliver_pushes(
        payload=payload,
        ttl_seconds=ttl_seconds,
        topic=topic,
        endpoints_src=endpoints_src,
        dummy_run=dummy_run,
    )


def send_diagnostic_push(
    *,
    user_id: str,
    request_value: JSONValue,
    endpoints_src: Callable[[], Iterable[DocumentSnapshot]],
    dummy_run: bool = False,
) -> PushDeliveryResult:
    payload = _build_diagnostic_payload(user_id=user_id, request_value=request_value)
    topic = _topic_for_poll_id(f"diag-{user_id}")
    return _deliver_pushes(
        payload=payload,
        ttl_seconds=_TTL_MIN_SECONDS,
        topic=topic,
        endpoints_src=endpoints_src,
        dummy_run=dummy_run,
    )


def _build_chat_global_payload(
    message_id: str,
    sender_name: str,
    body_text: str,
) -> ChatPushPayload:
    return {
        "eventType": PUSH_EVENT_CHAT_MESSAGE_GLOBAL,
        "messageId": message_id,
        "pollId": None,
        "title": f"{sender_name} in Global Chat",
        "body": body_text[:100],
        "url": f"{_base_url()}/chat",
        "tag": "chat:main",
        "sentAt": datetime.now(UTC).isoformat(),
    }


def _build_chat_event_payload(
    message_id: str,
    poll_id: str,
    sender_name: str,
    body_text: str,
) -> ChatPushPayload:
    return {
        "eventType": PUSH_EVENT_CHAT_MESSAGE_EVENT,
        "messageId": message_id,
        "pollId": poll_id,
        "title": f"{sender_name} in Event Chat",
        "body": body_text[:100],
        "url": f"{_base_url()}/chat/event/{poll_id}",
        "tag": f"chat:{poll_id}",
        "sentAt": datetime.now(UTC).isoformat(),
    }


def _endpoint_hash(endpoint: ValidPushEndpoint) -> str:
    """Return a 32-char hex hash identifying this user+endpoint combination."""
    raw = f"{endpoint.user_id}__{endpoint.endpoint}"
    return hashlib.sha256(raw.encode()).hexdigest()[:32]


def build_chat_global_payload(
    message_id: str,
    sender_name: str,
    body_text: str,
) -> ChatPushPayload:
    return _build_chat_global_payload(
        message_id=message_id,
        sender_name=sender_name,
        body_text=body_text,
    )


def build_chat_event_payload(
    message_id: str,
    poll_id: str,
    sender_name: str,
    body_text: str,
) -> ChatPushPayload:
    return _build_chat_event_payload(
        message_id=message_id,
        poll_id=poll_id,
        sender_name=sender_name,
        body_text=body_text,
    )


def endpoint_hash(endpoint: ValidPushEndpoint) -> str:
    return _endpoint_hash(endpoint)


def push_endpoint_from_snapshot(document: DocumentSnapshot) -> PushEndpoint | None:
    return _push_endpoint_from_snapshot(document)


def send_chat_push(
    endpoints: list[ValidPushEndpoint],
    *,
    payload: ChatPushPayload,
    actions_ref: ChatPushActionsRef,
    actions_doc_exists: bool,
    scope_type: str,
    scope_id: str,
    dummy_run: bool = False,
) -> list[str]:
    """Send a chat push notification to a pre-resolved list of endpoints.

    Writes a delivery record to ``actions_ref`` immediately after each
    successful endpoint send so that retries can skip already-delivered
    endpoints even when a later endpoint causes a retryable failure.

    Returns a deduplicated list of user_ids successfully delivered to.
    Raises CallbackExceptionRetry if any retryable failures occurred.
    Deactivates invalid/stale endpoints as a side-effect.
    """
    delivered_uids: list[str] = []
    retryable_failures = 0
    _doc_initialized = actions_doc_exists

    for endpoint in endpoints:
        ep_hash = _endpoint_hash(endpoint)
        try:
            _send_to_endpoint(
                endpoint,
                payload,
                ttl_seconds=_TTL_MIN_SECONDS,
                topic=f"chat-{hashlib.sha256(payload['tag'].encode()).hexdigest()[:_TOPIC_MAX_LEN]}",
                dummy_run=dummy_run,
            )
            uid = endpoint.user_id
            if uid:
                delivered_uids.append(uid)
            # Write idempotency record inline so retries can skip this endpoint.
            write_data: dict[str, object] = {
                "delivered_endpoints": ArrayUnion([ep_hash])
            }
            if uid:
                write_data["notified"] = ArrayUnion([uid])
            if not _doc_initialized:
                write_data["scopeType"] = scope_type
                write_data["scopeId"] = scope_id
                write_data["createdAt"] = SERVER_TIMESTAMP
                actions_ref.set(write_data, merge=True)
                _doc_initialized = True
            else:
                actions_ref.update(write_data)
        except WebPushException as exc:
            status_code = getattr(getattr(exc, "response", None), "status_code", None)
            response_text = getattr(getattr(exc, "response", None), "text", None)
            if status_code in {404, 410}:
                _log.info(
                    "Deactivating stale push endpoint for user %s", endpoint.user_id
                )
                _deactivate_endpoint(endpoint)
                continue
            if status_code in {400, 401, 403}:
                _log.warning(
                    "Deactivating push endpoint after non-retryable failure: "
                    "status=%s user=%s response=%s",
                    status_code,
                    endpoint.user_id,
                    response_text,
                )
                _deactivate_endpoint(endpoint)
                continue
            retryable_failures += 1
            _log.exception(
                "Retryable web push failure for user %s status=%s response=%s",
                endpoint.user_id,
                status_code,
                response_text,
            )
        except Exception:
            retryable_failures += 1
            _log.exception(
                "Unexpected web push failure for user %s endpoint=%s",
                endpoint.user_id,
                endpoint.endpoint,
            )

    unique_notified = list(dict.fromkeys(delivered_uids))
    _log.info(
        "Chat push delivery: endpoints_delivered=%s unique_users=%s retryable_failures=%s",
        len(delivered_uids),
        len(unique_notified),
        retryable_failures,
    )
    if retryable_failures:
        raise CallbackExceptionRetry(
            f"Retryable chat push failures (retryable={retryable_failures}, delivered={len(delivered_uids)})"
        )
    return unique_notified
