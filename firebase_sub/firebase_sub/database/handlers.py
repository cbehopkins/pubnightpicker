import logging
from collections.abc import Callable, Generator, Mapping, Sequence
from datetime import datetime
from functools import partial
from typing import Any, cast

from firebase_admin import firestore
from google.cloud.firestore_v1 import watch
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.database.repositories import (
    FirestorePollRepository,
    FirestoreUserRepository,
)
from firebase_sub.my_types import EmailAddr, UserId
from firebase_sub.push_contract import PUSH_PREFERENCE_DEFAULTS

_log = logging.getLogger(__name__)


class RetryablePollDataNotReadyError(RuntimeError):
    """Raised when event handling should retry because dependent data is not ready."""


def _payload_dict(payload: object) -> dict[str, object]:
    if not isinstance(payload, Mapping):
        return {}

    normalized: dict[str, object] = {}
    payload_map = cast(Mapping[object, object], payload)
    for key, value in payload_map.items():
        if isinstance(key, str):
            normalized[key] = value
            continue
        normalized[str(key)] = value
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


def _snapshot_get(document_ref: object) -> DocumentSnapshot:
    raw_snapshot = cast(Any, document_ref).get()
    if isinstance(raw_snapshot, DocumentSnapshot):
        return raw_snapshot
    if raw_snapshot is not None and hasattr(raw_snapshot, "to_dict"):
        # Accept snapshot-like test doubles that expose Firestore-compatible surface.
        return cast(DocumentSnapshot, raw_snapshot)
    raise TypeError("Expected synchronous DocumentSnapshot from Firestore get()")


def _query_where(query: Query, *, field: str, op: str, value: object) -> Query:
    query_any: Any = query
    return cast(Query, query_any.where(filter=FieldFilter(field, op, value)))


def _endpoint_parent_user_id(document: DocumentSnapshot) -> str | None:
    # Expected path: users/<uid>/push_endpoints/<endpoint_id>
    path_parts = document.reference.path.split("/")
    if len(path_parts) < 4:
        return None
    if path_parts[-2] != "push_endpoints":
        return None
    user_id = path_parts[-3]
    return user_id if user_id else None


class DbHandler:
    def __init__(self):
        self.db: Client = firestore.client()
        # patch_watch_close(self.my_watch_close_callback)
        self.okay = True
        self.poll_repo = FirestorePollRepository(self.db)
        self.user_repo = FirestoreUserRepository(self.db)

    def my_watch_close_callback(self, reason: object | None) -> None:
        # This is no longer called as we often close a watch
        # At the moment we restart the watch regularly
        # to make sure we keep a live connection
        _log.error(f"Firestore Watch closed! Reason: {reason}")
        self.okay = False
        # This happens in a different thread - so we are blocked unable to exit
        # https://github.com/googleapis/python-firestore/issues/882
        raise SystemExit("Exiting due to watch close.")

    @property
    def pub_collection(self) -> CollectionReference:
        return self.db.collection("pubs")

    def query_personal_emails(self) -> Generator[tuple[EmailAddr, UserId], None, None]:
        """Query users who want personal email notifications (via personal email)."""
        yield from self.user_repo.query_users_by_email_preference(
            "notificationEmailEnabled"
        )

    def query_open_emails(self) -> Generator[tuple[EmailAddr, UserId], None, None]:
        """Query users who want poll-open notifications."""
        yield from self.user_repo.query_users_by_email_preference(
            "openPollEmailEnabled"
        )

    def query_active_push_endpoints(
        self, preference_field: str
    ) -> Generator[DocumentSnapshot, None, None]:
        """Query active web push endpoints across users with web push enabled.

        Filters by both the webPushEnabled master switch and the per-type
        pushPreferences field identified by ``preference_field`` (e.g.
        "pollOpens", "pollCompletes", "globalChat", "eventChat").
        """
        collection_group_any: Any = self.db.collection_group("push_endpoints")
        query = cast(
            Query,
            collection_group_any.where(filter=FieldFilter("active", "==", True)),
        )
        user_preference_cache: dict[str, bool] = {}
        for endpoint_doc in query.stream():
            user_id = _endpoint_parent_user_id(endpoint_doc)
            if user_id is None:
                continue
            if user_id not in user_preference_cache:
                user_snapshot = _snapshot_get(
                    self.db.collection("users").document(user_id)
                )
                user_payload = _snapshot_payload(user_snapshot)
                if "webPushEnabled" not in user_payload:
                    # Temporary rollout observability: missing preference now defaults off.
                    _log.warning(
                        "Skipping push endpoints for user %s because webPushEnabled is missing",
                        user_id,
                    )
                    user_preference_cache[user_id] = False
                elif not bool(user_payload.get("webPushEnabled")):
                    # Temporary rollout observability: explicit opt-out.
                    _log.info(
                        "Skipping push endpoints for user %s because webPushEnabled is false",
                        user_id,
                    )
                    user_preference_cache[user_id] = False
                else:
                    push_prefs = _payload_dict(user_payload.get("pushPreferences"))
                    default = PUSH_PREFERENCE_DEFAULTS.get(preference_field, True)
                    user_preference_cache[user_id] = bool(
                        push_prefs.get(preference_field, default)
                    )
                    if not user_preference_cache[user_id]:
                        _log.info(
                            "Skipping push endpoints for user %s because pushPreferences.%s is false",
                            user_id,
                            preference_field,
                        )
            if user_preference_cache[user_id]:
                yield endpoint_doc

    def query_active_push_endpoints_for_user(
        self, user_id: str
    ) -> Generator[DocumentSnapshot, None, None]:
        """Query active web push endpoints for one user when web push is enabled."""
        if not user_id:
            return
        user_snapshot = _snapshot_get(self.db.collection("users").document(user_id))
        user_payload = _snapshot_payload(user_snapshot)
        if "webPushEnabled" not in user_payload:
            _log.warning(
                "Skipping push endpoints for user %s because webPushEnabled is missing",
                user_id,
            )
            return
        if not bool(user_payload.get("webPushEnabled")):
            _log.info(
                "Skipping push endpoints for user %s because webPushEnabled is false",
                user_id,
            )
            return

        user_document_ref = self.db.collection("users").document(user_id)
        endpoint_collection = cast(Any, user_document_ref.collection("push_endpoints"))
        # TODO(revisit-indexed-query): switch back to .where(active == True)
        # once COLLECTION-scope index rollout for push_endpoints.active is
        # confirmed in all deployed projects.
        for endpoint_doc in endpoint_collection.stream():
            payload = _snapshot_payload(endpoint_doc)
            if not bool(payload.get("active")):
                continue
            yield endpoint_doc

    def _users_with_push_preference(self, preference_field: str) -> set[str]:
        """Return uids of users with webPushEnabled=True and the given pushPreferences field True."""
        uids: set[str] = set()
        users_collection: CollectionReference = self.db.collection("users")
        for user_doc in users_collection.stream():
            user_payload = _snapshot_payload(user_doc)
            if not bool(user_payload.get("webPushEnabled")):
                continue
            push_prefs = _payload_dict(user_payload.get("pushPreferences"))
            default = PUSH_PREFERENCE_DEFAULTS.get(preference_field, True)
            if bool(push_prefs.get(preference_field, default)):
                uids.add(user_doc.id)
        return uids

    def _attendee_uids(self, poll_id: str) -> set[str]:
        """Return uids of users attending the given poll (in any canCome array)."""
        attendance_doc = _snapshot_get(
            self.db.collection("attendance").document(poll_id)
        )
        attendance_data = _snapshot_payload(attendance_doc)
        uids: set[str] = set()
        for venue_data in attendance_data.values():
            if not isinstance(venue_data, Mapping):
                continue
            can_come = _string_list(
                cast(Mapping[str, object], venue_data).get("canCome")
            )
            uids.update(can_come)
        return uids

    def _event_chat_participant_uids(self, poll_id: str) -> set[str]:
        """Return uids that have already authored messages in the event chat."""
        if not poll_id:
            return set()

        query = cast(Query, self.db.collection("messages"))
        query = _query_where(query, field="scopeType", op="==", value="event")
        query = _query_where(query, field="scopeId", op="==", value=poll_id)

        participants: set[str] = set()
        for message_doc in query.stream():
            message_data = _snapshot_payload(message_doc)
            uid = message_data.get("uid")
            if isinstance(uid, str) and uid:
                participants.add(uid)
        return participants

    def _muted_event_chat_uids(
        self, poll_id: str, candidate_uids: set[str]
    ) -> set[str]:
        """Return candidate uids that muted notifications for a specific event."""
        if not poll_id or not candidate_uids:
            return set()

        muted: set[str] = set()
        for uid in candidate_uids:
            user_snapshot = _snapshot_get(self.db.collection("users").document(uid))
            user_payload = _snapshot_payload(user_snapshot)
            push_prefs = _payload_dict(user_payload.get("pushPreferences"))
            muted_poll_ids = _string_list(push_prefs.get("eventChatMutedPollIds"))
            if poll_id in muted_poll_ids:
                muted.add(uid)
        return muted

    @property
    def query_messages(self) -> Query:
        """Return a query for the messages collection (used for chat push listener)."""
        return cast(Query, self.db.collection("messages"))

    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query:
        """Query polls by completion state, optionally constrained by minimum date."""
        query = self.poll_repo.get_polls_by_status(completed=completed)
        if min_date is None:
            return query
        return _query_where(query, field="date", op=">=", value=min_date)

    @property
    def query_completed_true(self) -> Query:
        """Query completed polls."""
        return self.query_polls_by_status(completed=True)

    @property
    def query_completed_false(self) -> Query:
        """Query open (incomplete) polls."""
        return self.query_polls_by_status(completed=False)

    @property
    def query_all_polls(self) -> CollectionReference:
        """Return a query for all polls (no filters)."""
        return self.poll_repo.get_all_polls()

    @property
    def query_notification_requests(self) -> Query:
        """Return a query for notification request health-check documents."""
        return cast(Query, self.db.collection("notification_req"))

    @property
    def query_admin_delete_requests(self) -> Query:
        """Return a query for admin delete requests."""
        return cast(Query, self.db.collection("admin_delete_requests"))

    @staticmethod
    def wrapped_callback(
        doc_snapshot: Sequence[DocumentSnapshot],
        changes: Sequence[DocumentChange],
        read_time: datetime,
        callback: Callable[[str, DocumentSnapshot], None],
        collection: CollectionReference,
    ) -> None:
        collection_id = str(cast(Any, collection).id)
        if collection_id == "users":
            raise ValueError("Users collection should not be watched here.")
        for change in changes:
            change_type_name = str(getattr(cast(Any, change).type, "name", ""))
            if change_type_name == "ADDED":
                callback(collection_id, cast(Any, change).document)
            elif change_type_name == "MODIFIED":
                callback(collection_id, cast(Any, change).document)
            elif change_type_name == "REMOVED":
                pass

    def all_events_except_users(
        self, callback: Callable[[str, DocumentSnapshot], None]
    ) -> None:
        collections = self.db.collections()
        for raw_collection in collections:
            collection = cast(CollectionReference, raw_collection)
            collection_id = str(cast(Any, collection).id)
            if collection_id in ["users", "roles"]:
                continue
            bound_callback = partial(
                self.wrapped_callback, callback=callback, collection=collection
            )
            cast(Any, collection).on_snapshot(bound_callback)


def patch_watch_close(callback: Callable[[str | None], None]) -> None:
    orig_close = getattr(watch.Watch, "close")

    def new_close(self: watch.Watch, reason: str | None = None) -> None:
        callback(reason)
        # Call the original close
        orig_close(self, reason)

    setattr(watch.Watch, "close", new_close)
