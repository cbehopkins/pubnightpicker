import os
from datetime import UTC, date, datetime, timedelta
from typing import cast

from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client

from firebase_sub.database.housekeeping import HousekeepingTask

NOTIFICATION_REQ_COLLECTION = "notification_req"
NOTIFICATION_ACK_COLLECTION = "notification_ack"
DIAGNOSTICS_DOC_ID = "diagnostics"
POLLS_COLLECTION = "polls"
PUSH_ENDPOINTS_COLLECTION = "push_endpoints"
PUSH_ENDPOINT_RETENTION_DAYS = int(
    os.getenv("PUSH_ENDPOINT_RETENTION_DAYS", "60")
)


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
        poll_id = cast(str, poll_doc.id)
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
    ]
