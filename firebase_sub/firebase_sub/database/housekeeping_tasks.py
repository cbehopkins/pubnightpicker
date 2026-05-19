import logging
import os
from collections.abc import Mapping
from datetime import UTC, date, datetime, timedelta
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.transforms import DELETE_FIELD
from google.cloud.firestore_v1.client import Client

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

NOTIFICATION_REQ_COLLECTION = "notification_req"
NOTIFICATION_ACK_COLLECTION = "notification_ack"
DIAGNOSTICS_DOC_ID = "diagnostics"
PUSH_TEST_DOC_ID = "push_test"
POLLS_COLLECTION = "polls"
POLL_ACTION_AUDIT_COLLECTION = "poll_action_audit"
POLL_ACTION_COMPLETE = "complete"
POLL_ACTION_SYSTEM_ACTOR_UID = "system:event_recurrence"
PUSH_ENDPOINTS_COLLECTION = "push_endpoints"
PUSH_ENDPOINT_RETENTION_DAYS = int(os.getenv("PUSH_ENDPOINT_RETENTION_DAYS", "60"))
PUSH_DIAGNOSTIC_RETENTION_DAYS = 1
POLL_ACTION_AUDIT_RETENTION_DAYS = int(
    os.getenv("POLL_ACTION_AUDIT_RETENTION_DAYS", "90")
)
EVENT_POLL_CREATION_LEAD_DAYS = int(os.getenv("EVENT_POLL_CREATION_LEAD_DAYS", "7"))
EVENTS_COLLECTION = "pubs"
POLL_VENUE_TYPE = VenueType.EVENT.value
logger = logging.getLogger(__name__)


def delete_notification_diagnostics(db: Client) -> None:
    """Delete diagnostics docs used by manual health checks."""
    db.collection(NOTIFICATION_REQ_COLLECTION).document(DIAGNOSTICS_DOC_ID).delete()
    db.collection(NOTIFICATION_ACK_COLLECTION).document(DIAGNOSTICS_DOC_ID).delete()


def delete_notification_docs_for_past_polls(
    db: Client, today: date | None = None
) -> None:
    """Delete req/ack notification docs for polls with dates before today."""
    cutoff = (today or date.today()).isoformat()
    poll_stream = (
        db.collection(POLLS_COLLECTION)
        .where(filter=FieldFilter("date", "<", cutoff))
        .stream()
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
    endpoint_stream = (
        db.collection_group(PUSH_ENDPOINTS_COLLECTION)
        .where(filter=FieldFilter("active", "==", False))
        .where(filter=FieldFilter("disabledAt", "<", cutoff))
        .stream()
    )

    for endpoint_doc in endpoint_stream:
        endpoint_doc.reference.delete()


def _notification_entry_timestamp_ms(value: object) -> int | None:
    if isinstance(value, int | float):
        return int(value)
    if isinstance(value, dict):
        ts_value = value.get("ts")
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
        snapshot = push_doc.get()
        payload = snapshot.to_dict() or {}
        stale_keys = {
            key: DELETE_FIELD
            for key, value in payload.items()
            if (timestamp_ms := _notification_entry_timestamp_ms(value)) is not None
            and timestamp_ms <= cutoff_ms
        }
        if stale_keys:
            push_doc.set(stale_keys, merge=True)


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
    stale_records = (
        db.collection(POLL_ACTION_AUDIT_COLLECTION)
        .where(filter=FieldFilter("at", "<", cutoff_time))
        .stream()
    )

    for audit_doc in stale_records:
        audit_doc.reference.delete()


def _write_poll_action_audit(
    db: Client,
    *,
    poll_id: str,
    action_type: str,
    actor_uid: str,
    poll_date: str,
    selected_venue_id: str | None = None,
    venue_name: str | None = None,
) -> None:
    now = datetime.now(UTC)
    timestamp_micros = int(now.timestamp() * 1_000_000)
    audit_doc_id = f"{poll_id}_{action_type}_{timestamp_micros}"
    payload: dict[str, object] = {
        "pollId": poll_id,
        "actionType": action_type,
        "actorUid": actor_uid,
        "at": now,
        "pollDate": poll_date,
    }
    if selected_venue_id:
        payload["selectedVenueId"] = selected_venue_id
    if venue_name:
        payload["venueName"] = venue_name

    db.collection(POLL_ACTION_AUDIT_COLLECTION).document(audit_doc_id).set(payload)


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
            venue_doc.reference.set(
                {"next_occurrence_date": occurrence_date.isoformat()},
                merge=True,
            )
            logger.info(
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
) -> tuple[str, Any, DocumentSnapshot, dict[str, object]]:
    poll_id = event_poll_id(venue_doc.id, occurrence_date)
    poll_ref = db.document(f"{POLLS_COLLECTION}/{poll_id}")
    poll_snapshot = poll_ref.get()
    poll_data = cast(dict[str, object], poll_snapshot.to_dict() or {})
    event_name = cast(str, venue_data.get("name") or venue_doc.id)

    if (
        today >= creation_window_start(occurrence_date, lead_days=creation_lead_days)
        and not poll_snapshot.exists
    ):
        poll_ref.set(
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
            }
        )
        db.collection("votes").document(poll_id).set({"any": []})
        db.collection("attendance").document(poll_id).set({})
        logger.info(
            "Created event recurrence poll %s for venue %s on %s",
            poll_id,
            venue_doc.id,
            occurrence_date.isoformat(),
        )
        poll_snapshot = poll_ref.get()
        poll_data = cast(dict[str, object], poll_snapshot.to_dict() or {})

    return poll_id, poll_ref, poll_snapshot, poll_data


def _complete_event_poll_and_advance_occurrence(
    db: Client,
    *,
    venue_doc: DocumentSnapshot,
    venue_data: Mapping[str, object],
    recurrence: EventRecurrenceRule | None,
    occurrence_date: date,
    poll_id: str,
    poll_ref: Any,
    poll_snapshot: DocumentSnapshot,
    poll_data: Mapping[str, object],
    today: date,
) -> None:
    if today < event_week_completion_start(occurrence_date):
        return

    if poll_snapshot.exists and not bool(poll_data.get("completed")):
        poll_ref.set(
            {"completed": True, "selected": venue_doc.id},
            merge=True,
        )
        event_name = cast(str | None, venue_data.get("name"))
        try:
            _write_poll_action_audit(
                db,
                poll_id=poll_id,
                action_type=POLL_ACTION_COMPLETE,
                actor_uid=POLL_ACTION_SYSTEM_ACTOR_UID,
                poll_date=occurrence_date.isoformat(),
                selected_venue_id=venue_doc.id,
                venue_name=event_name,
            )
        except Exception:
            logger.exception(
                "Failed to write poll action audit entry for recurring event poll %s",
                poll_id,
            )
        logger.info(
            "Completed event recurrence poll %s for venue %s",
            poll_id,
            venue_doc.id,
        )

    current_iso_str, next_iso = materialized_next_occurrence_iso_state(
        recurrence,
        venue_data.get("next_occurrence_date"),
        today=today,
    )

    if next_iso == current_iso_str:
        return

    if next_iso is not None:
        venue_doc.reference.set(
            {"next_occurrence_date": next_iso},
            merge=True,
        )
        logger.info(
            "Advanced next_occurrence_date for venue %s to %s",
            venue_doc.id,
            next_iso,
        )
        return

    venue_doc.reference.set({"next_occurrence_date": DELETE_FIELD}, merge=True)
    if recurrence is None:
        logger.info(
            "Cleared next_occurrence_date for venue %s because recurrence is missing",
            venue_doc.id,
        )
    else:
        logger.info(
            "Cleared next_occurrence_date for venue %s because recurrence ended",
            venue_doc.id,
        )


def maintain_event_recurrence_polls(
    db: Client,
    *,
    today: date | None = None,
    creation_lead_days: int = EVENT_POLL_CREATION_LEAD_DAYS,
) -> None:
    """Materialize and complete event polls based on venue recurrence settings."""
    current_day = today or date.today()
    event_stream = db.collection(EVENTS_COLLECTION).stream()

    for venue_doc in event_stream:
        try:
            venue_data = cast(dict[str, object], venue_doc.to_dict() or {})
            if venue_data.get("venueType") != POLL_VENUE_TYPE:
                continue

            recurrence, occurrence_date = _resolve_event_occurrence_date(
                venue_doc,
                venue_data,
                today=current_day,
            )

            if occurrence_date is None:
                if recurrence is None:
                    logger.debug(
                        "Skipping event venue %s because recurrence is not configured",
                        venue_doc.id,
                    )
                else:
                    logger.warning(
                        "Skipping event venue %s because recurrence did not produce a valid occurrence date",
                        venue_doc.id,
                    )
                continue

            poll_id, poll_ref, poll_snapshot, poll_data = _create_event_poll_if_due(
                db,
                venue_doc=venue_doc,
                venue_data=venue_data,
                occurrence_date=occurrence_date,
                today=current_day,
                creation_lead_days=creation_lead_days,
            )

            _complete_event_poll_and_advance_occurrence(
                db,
                venue_doc=venue_doc,
                venue_data=venue_data,
                recurrence=recurrence,
                occurrence_date=occurrence_date,
                poll_id=poll_id,
                poll_ref=poll_ref,
                poll_snapshot=poll_snapshot,
                poll_data=poll_data,
                today=current_day,
            )
        except Exception:
            logger.exception(
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
