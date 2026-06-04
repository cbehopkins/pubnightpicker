import logging
import os
from collections.abc import Generator, Iterable, Mapping
from datetime import UTC, date, datetime, timedelta
from typing import Any, cast

from google.api_core.exceptions import FailedPrecondition
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.document import DocumentReference
from google.cloud.firestore_v1.transforms import DELETE_FIELD

from firebase_sub.database.event_recurrence import (
    creation_window_start,
    event_poll_id,
    event_week_completion_start,
    materialized_next_occurrence_iso_state,
    next_occurrence,
    parse_iso_date,
)
from firebase_sub.database.housekeeping import HousekeepingTask
from firebase_sub.my_types import EventRecurrenceRule, VenueType
from firebase_sub.push_contract import (
    PUSH_EVENT_POLL_MANUAL_COMPLETION_REQUIRED,
    PUSH_PREFERENCE_DEFAULTS,
    PUSH_PREFERENCE_FIELD,
)
from firebase_sub.send_push import send_poll_manual_completion_needed_push

NOTIFICATION_REQ_COLLECTION = "notification_req"
NOTIFICATION_ACK_COLLECTION = "notification_ack"
DIAGNOSTICS_DOC_ID = "diagnostics"
PUSH_TEST_DOC_ID = "push_test"
POLLS_COLLECTION = "polls"
VOTES_COLLECTION = "votes"
POLL_ACTION_AUDIT_COLLECTION = "poll_action_audit"
POLL_ACTION_CREATE = "create"
POLL_ACTION_COMPLETE = "complete"
BACKEND_AUTOMATION_ACTOR_UID = "backend:auto"
PUSH_ENDPOINTS_COLLECTION = "push_endpoints"
PUSH_ENDPOINT_RETENTION_DAYS = int(os.getenv("PUSH_ENDPOINT_RETENTION_DAYS", "60"))
PUSH_DIAGNOSTIC_RETENTION_DAYS = 1
POLL_ACTION_AUDIT_RETENTION_DAYS = int(
    os.getenv("POLL_ACTION_AUDIT_RETENTION_DAYS", "90")
)
EVENT_POLL_CREATION_LEAD_DAYS = int(os.getenv("EVENT_POLL_CREATION_LEAD_DAYS", "7"))
EVENTS_COLLECTION = "pubs"
POLL_VENUE_TYPE = VenueType.EVENT.value
USERS_COLLECTION = "users"
ROLES_COLLECTION = "roles"
CAN_COMPLETE_POLL_ROLE = "canCompletePoll"
_log = logging.getLogger(__name__)


def _snapshot_payload(snapshot: object) -> dict[str, object]:
    """Normalize firestore snapshot payloads to a typed mapping."""
    if not hasattr(snapshot, "to_dict"):
        return {}

    raw_payload = cast(DocumentSnapshot, snapshot).to_dict()
    if not isinstance(raw_payload, Mapping):
        return {}

    return cast(dict[str, object], dict(raw_payload))


def _as_mapping(value: object) -> Mapping[str, object] | None:
    if isinstance(value, Mapping):
        return cast(Mapping[str, object], value)
    return None


def _doc_get(doc_ref: DocumentReference) -> DocumentSnapshot:
    return cast(DocumentSnapshot, cast(Any, doc_ref).get())


def _doc_set(
    doc_ref: DocumentReference,
    payload: dict[str, object],
    *,
    merge: bool = False,
) -> None:
    if merge:
        cast(Any, doc_ref).set(payload, merge=True)
        return
    cast(Any, doc_ref).set(payload)


def delete_notification_diagnostics(db: Client) -> None:
    """Delete diagnostics docs used by manual health checks."""
    db.collection(NOTIFICATION_REQ_COLLECTION).document(DIAGNOSTICS_DOC_ID).delete()
    db.collection(NOTIFICATION_ACK_COLLECTION).document(DIAGNOSTICS_DOC_ID).delete()


def delete_notification_docs_for_past_polls(
    db: Client, today: date | None = None
) -> None:
    """Delete req/ack notification docs for polls with dates before today."""
    cutoff = (today or date.today()).isoformat()
    poll_query: Any = db.collection(POLLS_COLLECTION)
    poll_stream = cast(
        Iterable[DocumentSnapshot],
        poll_query.where(filter=FieldFilter("date", "<", cutoff)).stream(),
    )

    for poll_doc in poll_stream:
        poll_id = poll_doc.id
        db.collection(NOTIFICATION_REQ_COLLECTION).document(poll_id).delete()
        db.collection(NOTIFICATION_ACK_COLLECTION).document(poll_id).delete()


def delete_inactive_push_endpoints(
    db: Client,
    *,
    now: datetime | None = None,
    retention_days: int = PUSH_ENDPOINT_RETENTION_DAYS,
) -> None:
    """Delete inactive push endpoints that have been disabled beyond retention."""
    if retention_days < 0:
        raise ValueError("retention_days must be >= 0")

    current_time = now or datetime.now(UTC)
    cutoff = current_time - timedelta(days=retention_days)

    try:
        endpoint_query: Any = db.collection_group(PUSH_ENDPOINTS_COLLECTION)
        endpoint_stream = cast(
            Iterable[DocumentSnapshot],
            endpoint_query.where(filter=FieldFilter("active", "==", False))
            .where(filter=FieldFilter("disabledAt", "<", cutoff))
            .stream(),
        )
        for endpoint_doc in endpoint_stream:
            endpoint_doc.reference.delete()
        return
    except FailedPrecondition as exc:
        _log.warning(
            "Falling back to local disabledAt filtering for inactive push endpoint cleanup "
            "because composite index is unavailable: %s",
            exc,
        )

    fallback_query: Any = db.collection_group(PUSH_ENDPOINTS_COLLECTION)
    fallback_stream = cast(
        Iterable[DocumentSnapshot],
        fallback_query.where(filter=FieldFilter("active", "==", False)).stream(),
    )

    for endpoint_doc in fallback_stream:
        endpoint_data = endpoint_doc.to_dict() or {}
        disabled_at = endpoint_data.get("disabledAt")
        if isinstance(disabled_at, datetime) and disabled_at < cutoff:
            endpoint_doc.reference.delete()


def _notification_entry_timestamp_ms(value: object) -> int | None:
    if isinstance(value, int | float):
        return int(value)
    payload = _as_mapping(value)
    if payload is not None:
        ts_value = payload.get("ts")
        if isinstance(ts_value, int | float):
            return int(ts_value)
    return None


def delete_stale_push_diagnostic_entries(
    db: Client,
    *,
    now: datetime | None = None,
    retention_days: int = PUSH_DIAGNOSTIC_RETENTION_DAYS,
) -> None:
    """Delete stale uid->timestamp entries from push diagnostic req/ack docs."""
    if retention_days < 0:
        raise ValueError("retention_days must be >= 0")

    cutoff_time = (now or datetime.now(UTC)) - timedelta(days=retention_days)
    cutoff_ms = int(cutoff_time.timestamp() * 1000)

    for collection_name in (NOTIFICATION_REQ_COLLECTION, NOTIFICATION_ACK_COLLECTION):
        push_doc = db.document(f"{collection_name}/{PUSH_TEST_DOC_ID}")
        snapshot = _doc_get(push_doc)
        payload = _snapshot_payload(snapshot)
        stale_keys: dict[str, object] = {
            key: DELETE_FIELD
            for key, value in payload.items()
            if (timestamp_ms := _notification_entry_timestamp_ms(value)) is not None
            and timestamp_ms <= cutoff_ms
        }
        if stale_keys:
            _doc_set(push_doc, stale_keys, merge=True)


def delete_stale_poll_action_audit_entries(
    db: Client,
    *,
    now: datetime | None = None,
    retention_days: int = POLL_ACTION_AUDIT_RETENTION_DAYS,
) -> None:
    """Delete poll action audit records older than retention."""
    if retention_days < 0:
        raise ValueError("retention_days must be >= 0")

    cutoff_time = (now or datetime.now(UTC)) - timedelta(days=retention_days)
    stale_query: Any = db.collection(POLL_ACTION_AUDIT_COLLECTION)
    stale_records = cast(
        Iterable[DocumentSnapshot],
        stale_query.where(filter=FieldFilter("at", "<", cutoff_time)).stream(),
    )

    for audit_doc in stale_records:
        audit_doc.reference.delete()


def _write_poll_action_audit(
    db: Client,
    *,
    poll_id: str,
    poll_date: str,
    action_type: str,
    selected_venue_id: str | None = None,
) -> None:
    now = datetime.now(UTC)
    timestamp_micros = int(now.timestamp() * 1_000_000)
    audit_doc_id = f"{poll_id}_{action_type}_{timestamp_micros}"
    payload: dict[str, object] = {
        "pollId": poll_id,
        "actionType": action_type,
        "actorUid": BACKEND_AUTOMATION_ACTOR_UID,
        "at": now,
        "pollDate": poll_date,
    }
    if selected_venue_id:
        payload["selectedVenueId"] = selected_venue_id

    audit_doc = cast(
        DocumentReference,
        db.collection(POLL_ACTION_AUDIT_COLLECTION).document(audit_doc_id),
    )
    _doc_set(audit_doc, payload)


def _try_write_poll_action_audit(
    db: Client,
    *,
    poll_id: str,
    poll_date: str,
    action_type: str,
    selected_venue_id: str | None = None,
) -> None:
    try:
        _write_poll_action_audit(
            db,
            poll_id=poll_id,
            poll_date=poll_date,
            action_type=action_type,
            selected_venue_id=selected_venue_id,
        )
    except Exception:
        _log.exception(
            "Failed to write poll action audit for poll %s and action %s",
            poll_id,
            action_type,
        )
class pollDataHolder:
    def __init__(self, db: Client, poll_doc: DocumentSnapshot) -> None:
        self._db = db
        self._poll_doc = poll_doc
        self.poll_data: dict[str, object] = _snapshot_payload(poll_doc)

    @property
    def poll_doc(self) -> DocumentSnapshot:
        return self._poll_doc

    @property
    def pubs(self) -> list[str]:
        pd = _as_mapping(self.poll_data.get("pubs"))
        if pd is None:
            return []
        return list(pd)

    def winning_venue_id(self) -> str | None:
        return _resolve_clear_winner(
            db=self._db,
            poll_id=self._poll_doc.id,
            candidate_venue_ids=self.pubs,
        )

    def mark_completed_with_winner(self, winner_venue_id: str) -> None:
        _doc_set(
            cast(DocumentReference, self._poll_doc.reference),
            {
                "completed": True,
                "selected": winner_venue_id,
            },
            merge=True,
        )
def _open_polls(*, db: Client, target_date: str) -> Iterable[pollDataHolder]:
    poll_query: Any = db.collection(POLLS_COLLECTION)
    for poll_doc in cast(
        Iterable[DocumentSnapshot],
        poll_query.where(filter=FieldFilter("completed", "==", False))
        .where(filter=FieldFilter("date", "==", target_date))
        .stream(),
    ):
        poll_stuff = pollDataHolder(db, poll_doc)
        if not poll_stuff.poll_data:
            continue
        if not poll_stuff.pubs or len(poll_stuff.pubs) != 1:
            continue
        yield poll_stuff

def _complete_polls(*, db: Client, target_date: str) -> Iterable[pollDataHolder]:
    poll_query: Any = db.collection(POLLS_COLLECTION)

    for poll_doc in cast(
        Iterable[DocumentSnapshot],
        poll_query.where(filter=FieldFilter("completed", "==", False))
        .where(filter=FieldFilter("date", "==", target_date))
        .stream(),
    ):
        poll_stuff = pollDataHolder(db, poll_doc)
        if not poll_stuff.poll_data:
            continue
        if len(poll_stuff.pubs) < 1:
            continue
        yield poll_stuff

def auto_complete_single_event_polls_due_tomorrow(
    db: Client,
    *,
    today: date | None = None,
) -> None:
    """Auto-complete open polls with a single venue when due tomorrow.

    The selected venue is deterministically the only venue key in ``pubs``.
    """
    current_day = today or date.today()
    target_date = (current_day + timedelta(days=1)).isoformat()

    for poll_stuff in _open_polls(db=db, target_date=target_date):

        selected_venue_id = next(iter(poll_stuff.pubs))
        if not selected_venue_id:
            continue

        poll_stuff.mark_completed_with_winner(selected_venue_id)
        poll_date = poll_stuff.poll_data.get("date")
        _try_write_poll_action_audit(
            db,
            poll_id=poll_stuff.poll_doc.id,
            poll_date=(
                poll_date if isinstance(poll_date, str) and poll_date else target_date
            ),
            action_type=POLL_ACTION_COMPLETE,
            selected_venue_id=selected_venue_id,
        )
        _log.info(
            "Auto-completed single-event poll %s with venue %s",
            poll_stuff.poll_doc.id,
            selected_venue_id,
        )


def auto_complete_multi_option_polls_due_today(
    db: Client,
    *,
    today: date | None = None,
) -> None:
    """Auto-complete open multi-option polls due today when winner is clear.

    Rules:
    - poll must contain at least two venues in ``pubs`` # FIXME <- this is an incorrect rule - if we only have 1 venue we should still complete with it.
    - a single top-voted venue must exist in ``votes/{poll_id}``
    - top-voted venue must have ``food`` (or ``hasFood``) enabled
    """
    target_date = (today or date.today()).isoformat()

    for poll_stuff in _complete_polls(db=db, target_date=target_date):
        poll_data = poll_stuff.poll_data
        winner_venue_id = poll_stuff.winning_venue_id()
        poll_date = poll_data.get("date")
        resolved_poll_date = (
            poll_date if isinstance(poll_date, str) and poll_date else target_date
        )
        if winner_venue_id is None:
            _notify_manual_completion_needed(
                db=db,
                poll_id=poll_stuff.poll_doc.id,
                poll_date=resolved_poll_date,
            )
            _log.info(
                "Skipping auto-complete for poll %s because there is no clear winner",
                poll_stuff.poll_doc.id,
            )
            continue

        if not _venue_has_food(db=db, venue_id=winner_venue_id):
            _notify_manual_completion_needed(
                db=db,
                poll_id=poll_stuff.poll_doc.id,
                poll_date=resolved_poll_date,
            )
            _log.info(
                "Skipping auto-complete for poll %s because winner %s has no food",
                poll_stuff.poll_doc.id,
                winner_venue_id,
            )
            continue

        poll_stuff.mark_completed_with_winner(winner_venue_id)
        _try_write_poll_action_audit(
            db,
            poll_id=poll_stuff.poll_doc.id,
            poll_date=resolved_poll_date,
            action_type=POLL_ACTION_COMPLETE,
            selected_venue_id=winner_venue_id,
        )
        _log.info(
            "Auto-completed multi-option poll %s with winner %s",
            poll_stuff.poll_doc.id,
            winner_venue_id,
        )



def _resolve_clear_winner(
    *,
    db: Client,
    poll_id: str,
    candidate_venue_ids: list[str],
) -> str | None:
    votes_doc = cast(
        DocumentReference, db.collection(VOTES_COLLECTION).document(poll_id)
    )
    vote_snapshot = _doc_get(votes_doc)
    if not hasattr(vote_snapshot, "to_dict"):
        _log.error(
            "Votes document %s returned unsupported async snapshot",
            poll_id,
        )
        return None

    vote_doc = _snapshot_payload(vote_snapshot)

    counts: dict[str, int] = {}
    for venue_id in candidate_venue_ids:
        voters = vote_doc.get(venue_id)
        if isinstance(voters, list):
            counts[venue_id] = len(cast(list[object], voters))
        else:
            counts[venue_id] = 0

    top_vote_count = max(counts.values(), default=0)
    if top_vote_count <= 0:
        return None

    winners = [
        venue_id for venue_id, count in counts.items() if count == top_vote_count
    ]
    if len(winners) != 1:
        return None

    return winners[0]


def _iter_manual_completion_notification_endpoints(
    *,
    db: Client,
) -> Generator[DocumentSnapshot, None, None]:
    role_doc = cast(
        DocumentReference,
        db.collection(ROLES_COLLECTION).document(CAN_COMPLETE_POLL_ROLE),
    )
    role_snapshot = _doc_get(role_doc)
    if not hasattr(role_snapshot, "to_dict"):
        _log.error(
            "Role document %s returned unsupported async snapshot",
            CAN_COMPLETE_POLL_ROLE,
        )
        return
    role_payload = _snapshot_payload(role_snapshot)

    preference_field = PUSH_PREFERENCE_FIELD[PUSH_EVENT_POLL_MANUAL_COMPLETION_REQUIRED]
    preference_default = PUSH_PREFERENCE_DEFAULTS.get(preference_field, True)

    for uid, has_role in role_payload.items():
        if not uid or not bool(has_role):
            continue

        user_reference = cast(
            DocumentReference,
            db.collection(USERS_COLLECTION).document(uid),
        )
        user_snapshot = _doc_get(user_reference)
        if not hasattr(user_snapshot, "to_dict"):
            continue
        user_payload = _snapshot_payload(user_snapshot)
        if not bool(user_payload.get("webPushEnabled")):
            continue

        push_preferences = _as_mapping(user_payload.get("pushPreferences"))
        if push_preferences is not None:
            push_enabled_for_type = bool(
                push_preferences.get(preference_field, preference_default)
            )
        else:
            push_enabled_for_type = preference_default

        if not push_enabled_for_type:
            continue

        endpoint_stream = cast(
            Iterable[DocumentSnapshot],
            cast(Any, user_reference.collection(PUSH_ENDPOINTS_COLLECTION))
            .where(filter=FieldFilter("active", "==", True))
            .stream(),
        )
        for endpoint_doc in endpoint_stream:
            yield endpoint_doc


def _notify_manual_completion_needed(
    *,
    db: Client,
    poll_id: str,
    poll_date: str,
) -> None:
    try:
        result = send_poll_manual_completion_needed_push(
            poll_id=poll_id,
            poll_date=poll_date,
            endpoints_src=lambda: _iter_manual_completion_notification_endpoints(db=db),
        )
        _log.info(
            "Manual completion push for poll %s: delivered=%s invalid=%s retryable_failures=%s",
            poll_id,
            result.delivered,
            result.invalid,
            result.retryable_failures,
        )
    except Exception:
        _log.exception(
            "Failed to send manual completion push for poll %s",
            poll_id,
        )


def _venue_has_food(*, db: Client, venue_id: str) -> bool:
    venue_doc = cast(
        DocumentReference, db.collection(EVENTS_COLLECTION).document(venue_id)
    )
    venue_snapshot = _doc_get(venue_doc)
    if not hasattr(venue_snapshot, "to_dict"):
        _log.error(
            "Venue document %s returned unsupported async snapshot",
            venue_id,
        )
        return False

    venue_data = _snapshot_payload(venue_snapshot)

    food_value = venue_data.get("food")
    if isinstance(food_value, bool):
        return food_value

    has_food_value = venue_data.get("hasFood")
    return isinstance(has_food_value, bool) and has_food_value


def _resolve_event_occurrence_date(
    venue_doc: DocumentSnapshot,
    venue_data: Mapping[str, object],
    *,
    today: date,
) -> tuple[EventRecurrenceRule | None, date | None]:
    recurrence = cast(EventRecurrenceRule | None, venue_data.get("recurrence"))
    occurrence_date = parse_iso_date(venue_data.get("next_occurrence_date"))

    if occurrence_date is None and recurrence is not None:
        reference_date = parse_iso_date(recurrence.get("start_date")) or today
        occurrence_date = next_occurrence(recurrence, reference_date)
        if occurrence_date is not None:
            venue_ref = cast(DocumentReference, venue_doc.reference)
            _doc_set(
                venue_ref,
                {"next_occurrence_date": occurrence_date.isoformat()},
                merge=True,
            )
            _log.info(
                "Set next_occurrence_date for venue %s to %s",
                venue_doc.id,
                occurrence_date.isoformat(),
            )

    return recurrence, occurrence_date


def _create_event_poll_if_due(
    db: Client,
    *,
    venue_doc: DocumentSnapshot,
    venue_data: Mapping[str, object],
    occurrence_date: date,
    today: date,
    creation_lead_days: int,
) -> None:
    poll_id = event_poll_id(venue_doc.id, occurrence_date)
    poll_ref = db.document(f"{POLLS_COLLECTION}/{poll_id}")
    poll_snapshot = _doc_get(poll_ref)
    event_name = cast(str, venue_data.get("name") or venue_doc.id)

    if (
        today >= creation_window_start(occurrence_date, lead_days=creation_lead_days)
        and not poll_snapshot.exists
    ):
        _doc_set(
            poll_ref,
            {
                "date": occurrence_date.isoformat(),
                "completed": False,
                "pubs": {
                    venue_doc.id: {
                        "name": event_name,
                        "venueType": POLL_VENUE_TYPE,
                    }
                },
                "eventVenueId": venue_doc.id,
                "eventOccurrenceDate": occurrence_date.isoformat(),
            },
        )
        _doc_set(
            cast(DocumentReference, db.collection("votes").document(poll_id)),
            {"any": []},
        )
        _doc_set(
            cast(DocumentReference, db.collection("attendance").document(poll_id)),
            {},
        )
        _try_write_poll_action_audit(
            db,
            poll_id=poll_id,
            poll_date=occurrence_date.isoformat(),
            action_type=POLL_ACTION_CREATE,
        )
        _log.info(
            "Created event recurrence poll %s for venue %s on %s",
            poll_id,
            venue_doc.id,
            occurrence_date.isoformat(),
        )


def _advance_event_occurrence_if_due(
    *,
    venue_doc: DocumentSnapshot,
    venue_data: Mapping[str, object],
    recurrence: EventRecurrenceRule | None,
    occurrence_date: date,
    today: date,
) -> None:
    if today < event_week_completion_start(occurrence_date):
        return

    current_iso_str, next_iso = materialized_next_occurrence_iso_state(
        recurrence,
        venue_data.get("next_occurrence_date"),
        today=today,
    )

    if next_iso == current_iso_str:
        return

    if next_iso is not None:
        venue_ref = cast(DocumentReference, venue_doc.reference)
        _doc_set(
            venue_ref,
            {"next_occurrence_date": next_iso},
            merge=True,
        )
        _log.info(
            "Advanced next_occurrence_date for venue %s to %s",
            venue_doc.id,
            next_iso,
        )
        return

    venue_ref = cast(DocumentReference, venue_doc.reference)
    _doc_set(venue_ref, {"next_occurrence_date": DELETE_FIELD}, merge=True)
    if recurrence is None:
        _log.info(
            "Cleared next_occurrence_date for venue %s because recurrence is missing",
            venue_doc.id,
        )
    else:
        _log.info(
            "Cleared next_occurrence_date for venue %s because recurrence ended",
            venue_doc.id,
        )


def maintain_event_recurrence_polls(
    db: Client,
    *,
    today: date | None = None,
    creation_lead_days: int = EVENT_POLL_CREATION_LEAD_DAYS,
) -> None:
    """Materialize recurring event polls and update next occurrence metadata."""
    current_day = today or date.today()
    event_query: Any = db.collection(EVENTS_COLLECTION)
    event_stream = cast(Iterable[DocumentSnapshot], event_query.stream())

    for venue_doc in event_stream:
        try:
            venue_data = _snapshot_payload(venue_doc)
            if venue_data.get("venueType") != POLL_VENUE_TYPE:
                continue

            recurrence, occurrence_date = _resolve_event_occurrence_date(
                venue_doc,
                venue_data,
                today=current_day,
            )

            if occurrence_date is None:
                if recurrence is None:
                    _log.debug(
                        "Skipping event venue %s because recurrence is not configured",
                        venue_doc.id,
                    )
                else:
                    _log.warning(
                        "Skipping event venue %s because recurrence did not produce a valid occurrence date",
                        venue_doc.id,
                    )
                continue

            _create_event_poll_if_due(
                db,
                venue_doc=venue_doc,
                venue_data=venue_data,
                occurrence_date=occurrence_date,
                today=current_day,
                creation_lead_days=creation_lead_days,
            )

            _advance_event_occurrence_if_due(
                venue_doc=venue_doc,
                venue_data=venue_data,
                recurrence=recurrence,
                occurrence_date=occurrence_date,
                today=current_day,
            )
        except Exception:
            _log.exception(
                "Failed to process recurring event venue %s",
                venue_doc.id,
            )


def build_housekeeping_tasks(db: Client) -> list[HousekeepingTask]:
    return [
        HousekeepingTask(
            name="delete_notification_diagnostics",
            callback=lambda: delete_notification_diagnostics(db),
        ),
        HousekeepingTask(
            name="delete_notification_docs_for_past_polls",
            callback=lambda: delete_notification_docs_for_past_polls(db),
        ),
        HousekeepingTask(
            name="delete_inactive_push_endpoints",
            callback=lambda: delete_inactive_push_endpoints(db),
        ),
        HousekeepingTask(
            name="delete_stale_push_diagnostic_entries",
            callback=lambda: delete_stale_push_diagnostic_entries(db),
        ),
        HousekeepingTask(
            name="delete_stale_poll_action_audit_entries",
            callback=lambda: delete_stale_poll_action_audit_entries(db),
        ),
        HousekeepingTask(
            name="maintain_event_recurrence_polls",
            callback=lambda: maintain_event_recurrence_polls(db),
        ),
    ]
