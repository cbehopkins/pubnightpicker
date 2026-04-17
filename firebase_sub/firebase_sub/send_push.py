import json
import logging
import os
import hashlib
from collections.abc import Callable, Iterable
from dataclasses import dataclass
from datetime import UTC, date, datetime
from typing import Any

from firebase_sub.constants import ADMIN_EMAIL_ADDR
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore import SERVER_TIMESTAMP
from pywebpush import WebPushException, webpush

from firebase_sub.action_track import CallbackExceptionRetry
from firebase_sub.my_types import PollDocument, VenueDocument
from firebase_sub.push_contract import (
    PUSH_EVENT_POLL_COMPLETED,
    PUSH_EVENT_POLL_OPENED,
    PUSH_EVENT_POLL_RESCHEDULED,
)
from firebase_sub.send_email import _resolve_payloads

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


def web_push_enabled() -> bool:
    return os.getenv("ENABLE_WEB_PUSH", "false").lower() == "true"


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
    parent_document = document.reference.parent.parent
    if parent_document is None:
        return None
    return parent_document.id


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
    endpoint.document.reference.set(
        {
            "active": False,
            "disabledAt": SERVER_TIMESTAMP,
            "lastSeenAt": SERVER_TIMESTAMP,
        },
        merge=True,
    )


def _send_to_endpoint(
    endpoint: ValidPushEndpoint,
    payload: dict[str, Any],
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
    payload: dict[str, Any],
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


# FIXME Payloads here should have dataclass
def _build_open_payload(poll_id: str) -> dict[str, Any]:
    return {
        "eventType": PUSH_EVENT_POLL_OPENED,
        "pollId": poll_id,
        "title": "Pub Night voting opened",
        "body": "Voting has opened for this week's pub night. Tap to open the active polls page.",
        "url": f"{_base_url()}/active_polls",
        "tag": f"poll-open:{poll_id}",
        "sentAt": datetime.now(UTC).isoformat(),
    }

# FIXME again - dataclass this
def _build_complete_payload(
    poll_id: str,
    poll_dict: PollDocument,
    pub_dict: dict[str, VenueDocument],
    previously_actioned: bool,
) -> dict[str, Any]:
    poll, selected_venue, restaurant_venue = _resolve_payloads(
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
