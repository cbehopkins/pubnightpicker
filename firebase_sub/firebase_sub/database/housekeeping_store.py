import logging
from collections.abc import Callable, Iterable, Mapping
from datetime import UTC, datetime
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.document import DocumentReference
from google.cloud.firestore_v1.transforms import Sentinel

_log = logging.getLogger(__name__)

type WinnerResolver = Callable[[str, list[str]], str | None]


def snapshot_payload(snapshot: object) -> dict[str, object]:
    """Normalize firestore snapshot payloads to a typed mapping."""
    if not hasattr(snapshot, "to_dict"):
        return {}

    raw_payload = cast(DocumentSnapshot, snapshot).to_dict()
    if not isinstance(raw_payload, Mapping):
        return {}

    return cast(dict[str, object], dict(raw_payload))


def as_mapping(value: object) -> Mapping[str, object] | None:
    if isinstance(value, Mapping):
        return cast(Mapping[str, object], value)
    return None


def doc_get(doc_ref: DocumentReference) -> DocumentSnapshot:
    return cast(DocumentSnapshot, cast(Any, doc_ref).get())


def doc_set(
    doc_ref: DocumentReference,
    payload: dict[str, object],
    *,
    merge: bool = False,
) -> None:
    if merge:
        cast(Any, doc_ref).set(payload, merge=True)
        return
    cast(Any, doc_ref).set(payload)


def get_snapshot_or_log(
    snapshot: DocumentSnapshot,
    *,
    doc_id: str,
    context: str,
) -> dict[str, object] | None:
    """Safely get snapshot data with error logging."""
    if not hasattr(snapshot, "to_dict"):
        _log.error(
            "Document %s %s returned unsupported snapshot type",
            doc_id,
            context,
        )
        return None
    return snapshot_payload(snapshot)


def is_push_enabled_for_user(
    user_payload: dict[str, object],
    preference_field: str,
    default: bool,
) -> bool:
    """Check if push notifications are enabled for a specific user preference."""
    preferences = as_mapping(user_payload.get("pushPreferences"))
    if preferences is None:
        return default
    return bool(preferences.get(preference_field, default))


class DocumentFactory:
    """Factory for creating consistent document references for housekeeping flows."""

    def __init__(
        self,
        db: Client,
        *,
        events_collection: str,
        roles_collection: str,
        users_collection: str,
    ):
        self._db = db
        self._events_collection = events_collection
        self._roles_collection = roles_collection
        self._users_collection = users_collection

    def venue_ref(self, venue_id: str) -> DocumentReference:
        return cast(
            DocumentReference,
            self._db.collection(self._events_collection).document(venue_id),
        )

    def role_ref(self, role_id: str) -> DocumentReference:
        return cast(
            DocumentReference,
            self._db.collection(self._roles_collection).document(role_id),
        )

    def user_ref(self, uid: str) -> DocumentReference:
        return cast(
            DocumentReference,
            self._db.collection(self._users_collection).document(uid),
        )


class HousekeepingRepository:
    """Database access boundary for housekeeping orchestration."""

    def __init__(
        self,
        db: Client,
        *,
        events_collection: str,
        polls_collection: str,
        votes_collection: str,
        attendance_collection: str,
        roles_collection: str,
        users_collection: str,
    ) -> None:
        self._db = db
        self._events_collection = events_collection
        self._polls_collection = polls_collection
        self._votes_collection = votes_collection
        self._attendance_collection = attendance_collection
        self._factory = DocumentFactory(
            db,
            events_collection=events_collection,
            roles_collection=roles_collection,
            users_collection=users_collection,
        )

    def poll_holder(self, poll_doc: DocumentSnapshot) -> "PollDataHolder":
        return PollDataHolder(poll_doc, resolve_winner=self.resolve_clear_winner)

    def delete_document(self, collection_name: str, doc_id: str) -> None:
        self._db.collection(collection_name).document(doc_id).delete()

    def poll_ids_before_date(self, cutoff: str) -> Iterable[str]:
        poll_query: Any = self._db.collection(self._polls_collection)
        poll_stream = cast(
            Iterable[DocumentSnapshot],
            poll_query.where(filter=FieldFilter("date", "<", cutoff)).stream(),
        )
        for poll_doc in poll_stream:
            yield poll_doc.id

    def uncompleted_polls_on_date(self, target_date: str) -> Iterable["PollDataHolder"]:
        poll_query: Any = self._db.collection(self._polls_collection)
        poll_stream = cast(
            Iterable[DocumentSnapshot],
            poll_query.where(filter=FieldFilter("completed", "==", False))
            .where(filter=FieldFilter("date", "==", target_date))
            .stream(),
        )
        for poll_doc in poll_stream:
            yield self.poll_holder(poll_doc)

    def event_documents(self) -> Iterable[DocumentSnapshot]:
        event_query: Any = self._db.collection(self._events_collection)
        return cast(Iterable[DocumentSnapshot], event_query.stream())

    def push_diagnostic_state(
        self,
        collection_name: str,
        doc_id: str,
    ) -> tuple[DocumentReference, dict[str, object]]:
        doc_ref = self._db.document(f"{collection_name}/{doc_id}")
        return doc_ref, snapshot_payload(doc_get(doc_ref))

    def stale_audit_documents(
        self,
        audit_collection: str,
        cutoff_time: datetime,
    ) -> Iterable[DocumentSnapshot]:
        stale_query: Any = self._db.collection(audit_collection)
        return cast(
            Iterable[DocumentSnapshot],
            stale_query.where(filter=FieldFilter("at", "<", cutoff_time)).stream(),
        )

    def inactive_push_endpoint_candidates(
        self,
        collection_name: str,
        *,
        cutoff: datetime,
    ) -> Iterable[DocumentSnapshot]:
        endpoint_query: Any = self._db.collection_group(collection_name)
        return cast(
            Iterable[DocumentSnapshot],
            endpoint_query.where(filter=FieldFilter("active", "==", False))
            .where(filter=FieldFilter("disabledAt", "<", cutoff))
            .stream(),
        )

    def inactive_push_endpoint_fallback_candidates(
        self,
        collection_name: str,
    ) -> Iterable[DocumentSnapshot]:
        fallback_query: Any = self._db.collection_group(collection_name)
        return cast(
            Iterable[DocumentSnapshot],
            fallback_query.where(filter=FieldFilter("active", "==", False)).stream(),
        )

    def role_payload(self, role_id: str) -> dict[str, object] | None:
        role_snapshot = doc_get(self._factory.role_ref(role_id))
        return get_snapshot_or_log(
            role_snapshot,
            doc_id=role_id,
            context="role document",
        )

    def user_payload(self, uid: str) -> dict[str, object] | None:
        user_snapshot = doc_get(self._factory.user_ref(uid))
        return get_snapshot_or_log(
            user_snapshot,
            doc_id=uid,
            context="user document",
        )

    def active_push_endpoints(self, uid: str, *, collection_name: str) -> Iterable[DocumentSnapshot]:
        user_reference = self._factory.user_ref(uid)
        return cast(
            Iterable[DocumentSnapshot],
            cast(Any, user_reference.collection(collection_name))
            .where(filter=FieldFilter("active", "==", True))
            .stream(),
        )

    def venue_payload(self, venue_id: str) -> dict[str, object] | None:
        venue_snapshot = doc_get(self._factory.venue_ref(venue_id))
        return get_snapshot_or_log(
            venue_snapshot,
            doc_id=venue_id,
            context="venue document",
        )

    def resolve_clear_winner(
        self,
        poll_id: str,
        candidate_venue_ids: list[str],
    ) -> str | None:
        votes_doc = cast(
            DocumentReference,
            self._db.collection(self._votes_collection).document(poll_id),
        )
        vote_snapshot = doc_get(votes_doc)
        vote_doc = get_snapshot_or_log(
            vote_snapshot,
            doc_id=poll_id,
            context="votes document",
        )
        if vote_doc is None:
            return None

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

    def event_poll_ref(self, poll_id: str) -> DocumentReference:
        return self._db.document(f"{self._polls_collection}/{poll_id}")

    def event_poll_state(self, poll_id: str) -> tuple[DocumentReference, DocumentSnapshot]:
        poll_ref = self.event_poll_ref(poll_id)
        return poll_ref, doc_get(poll_ref)

    def initialize_event_poll(
        self,
        poll_ref: DocumentReference,
        poll_id: str,
        poll_data: dict[str, object],
    ) -> None:
        doc_set(poll_ref, poll_data)
        doc_set(
            cast(DocumentReference, self._db.collection(self._votes_collection).document(poll_id)),
            {"any": []},
        )
        doc_set(
            cast(
                DocumentReference,
                self._db.collection(self._attendance_collection).document(poll_id),
            ),
            {},
        )

    def merge_venue_state(
        self,
        venue_ref: DocumentReference,
        payload: dict[str, object],
    ) -> None:
        doc_set(venue_ref, payload, merge=True)

    def set_next_occurrence_date(
        self,
        venue_ref: DocumentReference,
        next_occurrence_iso: str,
    ) -> None:
        self.merge_venue_state(
            venue_ref,
            {"next_occurrence_date": next_occurrence_iso},
        )

    def clear_next_occurrence_date(
        self,
        venue_ref: DocumentReference,
        delete_field: Sentinel,
    ) -> None:
        self.merge_venue_state(
            venue_ref,
            {"next_occurrence_date": cast(object, delete_field)},
        )

    def write_poll_action_audit(
        self,
        *,
        audit_collection: str,
        actor_uid: str,
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
            "actorUid": actor_uid,
            "at": now,
            "pollDate": poll_date,
        }
        if selected_venue_id:
            payload["selectedVenueId"] = selected_venue_id

        audit_doc = cast(
            DocumentReference,
            self._db.collection(audit_collection).document(audit_doc_id),
        )
        doc_set(audit_doc, payload)

    def try_write_poll_action_audit(
        self,
        *,
        audit_collection: str,
        actor_uid: str,
        poll_id: str,
        poll_date: str,
        action_type: str,
        logger: logging.Logger,
        selected_venue_id: str | None = None,
    ) -> None:
        try:
            self.write_poll_action_audit(
                audit_collection=audit_collection,
                actor_uid=actor_uid,
                poll_id=poll_id,
                poll_date=poll_date,
                action_type=action_type,
                selected_venue_id=selected_venue_id,
            )
        except Exception:
            logger.exception(
                "Failed to write poll action audit for poll %s and action %s",
                poll_id,
                action_type,
            )


class PollDataHolder:
    def __init__(
        self,
        poll_doc: DocumentSnapshot,
        *,
        resolve_winner: WinnerResolver,
    ) -> None:
        self._poll_doc = poll_doc
        self._resolve_winner = resolve_winner
        self.poll_data: dict[str, object] = snapshot_payload(poll_doc)

    @property
    def poll_doc(self) -> DocumentSnapshot:
        return self._poll_doc

    @property
    def poll_id(self) -> str:
        return self._poll_doc.id

    @property
    def pubs(self) -> list[str]:
        pubs = as_mapping(self.poll_data.get("pubs"))
        if pubs is None:
            return []
        return list(pubs)

    def winning_venue_id(self) -> str | None:
        return self._resolve_winner(self.poll_id, self.pubs)

    def mark_completed_with_winner(self, winner_venue_id: str) -> None:
        doc_set(
            cast(DocumentReference, self._poll_doc.reference),
            {
                "completed": True,
                "selected": winner_venue_id,
            },
            merge=True,
        )
