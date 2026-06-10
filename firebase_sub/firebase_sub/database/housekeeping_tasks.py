import logging
import os
from collections.abc import Generator, Iterable, Mapping
from datetime import UTC, date, datetime, timedelta
from typing import cast

from google.api_core.exceptions import FailedPrecondition
from google.cloud.firestore_v1.base_document import DocumentSnapshot
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
from firebase_sub.database.housekeeping_store import (
    HousekeepingRepository,
    PollDataHolder,
)
from firebase_sub.database.housekeeping_store import as_mapping as _as_mapping
from firebase_sub.database.housekeeping_store import doc_set as _doc_set
from firebase_sub.database.housekeeping_store import (
    is_push_enabled_for_user as _is_push_enabled_for_user,
)
from firebase_sub.database.housekeeping_store import (
    snapshot_payload as _snapshot_payload,
)
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
ATTENDANCE_COLLECTION = "attendance"
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


def _repository(db: Client) -> HousekeepingRepository:
    return HousekeepingRepository(
        db,
        events_collection=EVENTS_COLLECTION,
        polls_collection=POLLS_COLLECTION,
        votes_collection=VOTES_COLLECTION,
        attendance_collection=ATTENDANCE_COLLECTION,
        roles_collection=ROLES_COLLECTION,
        users_collection=USERS_COLLECTION,
    )


def delete_notification_diagnostics(db: Client) -> None:
    """Delete diagnostics docs used by manual health checks."""
    repository = _repository(db)
    repository.delete_document(NOTIFICATION_REQ_COLLECTION, DIAGNOSTICS_DOC_ID)
    repository.delete_document(NOTIFICATION_ACK_COLLECTION, DIAGNOSTICS_DOC_ID)


def delete_notification_docs_for_past_polls(
    db: Client, today: date | None = None
) -> None:
    """Delete req/ack notification docs for polls with dates before today."""
    repository = _repository(db)
    cutoff = (today or date.today()).isoformat()
    for poll_id in repository.poll_ids_before_date(cutoff):
        repository.delete_document(NOTIFICATION_REQ_COLLECTION, poll_id)
        repository.delete_document(NOTIFICATION_ACK_COLLECTION, poll_id)


def delete_inactive_push_endpoints(
    db: Client,
    *,
    now: datetime | None = None,
    retention_days: int = PUSH_ENDPOINT_RETENTION_DAYS,
) -> None:
    """Delete inactive push endpoints that have been disabled beyond retention."""
    if retention_days < 0:
        raise ValueError("retention_days must be >= 0")

    repository = _repository(db)
    current_time = now or datetime.now(UTC)
    cutoff = current_time - timedelta(days=retention_days)

    try:
        endpoint_stream = repository.inactive_push_endpoint_candidates(
            PUSH_ENDPOINTS_COLLECTION,
            cutoff=cutoff,
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

    fallback_stream = repository.inactive_push_endpoint_fallback_candidates(
        PUSH_ENDPOINTS_COLLECTION,
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

    repository = _repository(db)
    cutoff_time = (now or datetime.now(UTC)) - timedelta(days=retention_days)
    cutoff_ms = int(cutoff_time.timestamp() * 1000)

    for collection_name in (NOTIFICATION_REQ_COLLECTION, NOTIFICATION_ACK_COLLECTION):
        push_doc, payload = repository.push_diagnostic_state(
            collection_name,
            PUSH_TEST_DOC_ID,
        )
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
    stale_records = _repository(db).stale_audit_documents(
        POLL_ACTION_AUDIT_COLLECTION,
        cutoff_time,
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
    _repository(db).write_poll_action_audit(
        audit_collection=POLL_ACTION_AUDIT_COLLECTION,
        actor_uid=BACKEND_AUTOMATION_ACTOR_UID,
        poll_id=poll_id,
        poll_date=poll_date,
        action_type=action_type,
        selected_venue_id=selected_venue_id,
    )


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


def _open_polls(db: Client, *, target_date: str) -> Iterable[PollDataHolder]:
    for poll_stuff in _repository(db).uncompleted_polls_on_date(target_date):
        if not poll_stuff.poll_data:
            continue
        if not poll_stuff.pubs or len(poll_stuff.pubs) != 1:
            continue
        yield poll_stuff


def _complete_polls(db: Client, *, target_date: str) -> Iterable[PollDataHolder]:
    for poll_stuff in _repository(db).uncompleted_polls_on_date(target_date):
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

    for poll_stuff in _open_polls(db, target_date=target_date):

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
    - poll must contain at least one venue in ``pubs``
    - a single top-voted venue must exist in ``votes/{poll_id}``
    - top-voted venue must have ``food``
    """
    target_date = (today or date.today()).isoformat()

    for poll_stuff in _complete_polls(db, target_date=target_date):
        poll_data = poll_stuff.poll_data
        winner_venue_id = poll_stuff.winning_venue_id()
        poll_date = poll_data.get("date")
        resolved_poll_date = (
            poll_date if isinstance(poll_date, str) and poll_date else target_date
        )
        if winner_venue_id is None:
            _notify_manual_completion_needed(
                db,
                poll_id=poll_stuff.poll_doc.id,
                poll_date=resolved_poll_date,
            )
            _log.info(
                "Skipping auto-complete for poll %s because there is no clear winner",
                poll_stuff.poll_doc.id,
            )
            continue

        if not _venue_has_food(db, venue_id=winner_venue_id):
            _notify_manual_completion_needed(
                db,
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


def _iter_manual_completion_notification_endpoints(
    db: Client,
) -> Generator[DocumentSnapshot, None, None]:
    repository = _repository(db)
    role_payload = repository.role_payload(CAN_COMPLETE_POLL_ROLE)
    if role_payload is None:
        return

    preference_field = PUSH_PREFERENCE_FIELD[PUSH_EVENT_POLL_MANUAL_COMPLETION_REQUIRED]
    preference_default = PUSH_PREFERENCE_DEFAULTS.get(preference_field, True)

    for uid, has_role in role_payload.items():
        if not uid or not bool(has_role):
            continue

        user_payload = repository.user_payload(uid)
        if user_payload is None:
            continue
        if not bool(user_payload.get("webPushEnabled")):
            continue

        if not _is_push_enabled_for_user(
            user_payload, preference_field, preference_default
        ):
            continue

        endpoint_stream = repository.active_push_endpoints(
            uid,
            collection_name=PUSH_ENDPOINTS_COLLECTION,
        )
        for endpoint_doc in endpoint_stream:
            yield endpoint_doc


def _notify_manual_completion_needed(
    db: Client,
    *,
    poll_id: str,
    poll_date: str,
) -> None:
    try:
        result = send_poll_manual_completion_needed_push(
            poll_id=poll_id,
            poll_date=poll_date,
            endpoints_src=lambda: _iter_manual_completion_notification_endpoints(db),
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


def _venue_has_food(db: Client, *, venue_id: str) -> bool:
    venue_data = _repository(db).venue_payload(venue_id)
    if venue_data is None:
        return False

    food_value = venue_data.get("food")
    return isinstance(food_value, bool) and food_value


def _resolve_event_occurrence_date(
    db: Client,
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
            _repository(db).set_next_occurrence_date(
                cast(DocumentReference, venue_doc.reference),
                occurrence_date.isoformat(),
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
    repository = _repository(db)
    poll_id = event_poll_id(venue_doc.id, occurrence_date)
    poll_ref, poll_snapshot = repository.event_poll_state(poll_id)
    event_name = cast(str, venue_data.get("name") or venue_doc.id)

    if (
        today >= creation_window_start(occurrence_date, lead_days=creation_lead_days)
        and not poll_snapshot.exists
    ):
        repository.initialize_event_poll(
            poll_ref,
            poll_id,
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
    db: Client,
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
        _repository(db).set_next_occurrence_date(
            cast(DocumentReference, venue_doc.reference),
            next_iso,
        )
        _log.info(
            "Advanced next_occurrence_date for venue %s to %s",
            venue_doc.id,
            next_iso,
        )
        return

    _repository(db).clear_next_occurrence_date(
        cast(DocumentReference, venue_doc.reference),
        DELETE_FIELD,
    )
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
    event_stream = _repository(db).event_documents()

    for venue_doc in event_stream:
        try:
            venue_data = _snapshot_payload(venue_doc)
            if venue_data.get("venueType") != POLL_VENUE_TYPE:
                continue

            recurrence, occurrence_date = _resolve_event_occurrence_date(
                db,
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
                db,
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
