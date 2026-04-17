import logging
from collections.abc import Callable, Generator, Sequence
from datetime import datetime
from functools import partial
from typing import cast

from firebase_admin import firestore
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1 import watch
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.action_track import ActionMan
from firebase_sub.push_contract import PushDedupeKeys
from firebase_sub.database.repositories import (
    FirestorePollRepository,
    FirestoreUserRepository,
)
from firebase_sub.my_types import EmailAddr, PollDocument, PollId, UserId

_log = logging.getLogger(__name__)


class RetryablePollDataNotReadyError(RuntimeError):
    """Raised when event handling should retry because dependent data is not ready."""


def _compute_action_key(poll_id: PollId, poll_dict: PollDocument, pub_id: str) -> str:
    """Build the canonical completion action key for email/push dedupe."""
    _ = poll_id
    return PushDedupeKeys.complete_key(
        pub_id=pub_id,
        restaurant_id=poll_dict.get("restaurant"),
        restaurant_time=poll_dict.get("restaurant_time"),
    )


def _with_legacy_alias_key(
    action_dict: dict | None, legacy_key: str, canonical_key: str
) -> dict | None:
    """Add canonical key when legacy key exists to avoid replay resend after migration."""
    if action_dict is None or legacy_key == canonical_key:
        return action_dict

    normalized = dict(action_dict)
    for action_type, values in list(normalized.items()):
        if not isinstance(values, (list, tuple, set)):
            continue
        key_set = set(values)
        if legacy_key in key_set and canonical_key not in key_set:
            key_set.add(canonical_key)
            normalized[action_type] = list(key_set)
    return normalized


class DbHandler:
    def __init__(self):
        self.db: Client = firestore.client()
        # patch_watch_close(self.my_watch_close_callback)
        self.okay = True
        self.poll_repo = FirestorePollRepository(self.db)
        self.user_repo = FirestoreUserRepository(self.db)

    def my_watch_close_callback(self, reason):
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

    def query_active_push_endpoints(self):
        """Query active web push endpoints across users with web push enabled."""
        query = self.db.collection_group("push_endpoints").where(
            filter=FieldFilter("active", "==", True)
        )
        user_preference_cache: dict[str, bool] = {}
        for endpoint_doc in query.stream():
            parent_user_doc = endpoint_doc.reference.parent.parent
            if parent_user_doc is None:
                continue
            user_id = parent_user_doc.id
            if user_id not in user_preference_cache:
                user_payload = parent_user_doc.get().to_dict() or {}
                if "webPushEnabled" not in user_payload:
                    # Temporary rollout observability: missing preference now defaults off.
                    _log.warning(
                        "Skipping push endpoints for user %s because webPushEnabled is missing",
                        user_id,
                    )
                    user_preference_cache[user_id] = False
                else:
                    user_preference_cache[user_id] = bool(
                        user_payload["webPushEnabled"]
                    )
                    if not user_preference_cache[user_id]:
                        # Temporary rollout observability: explicit opt-out.
                        _log.info(
                            "Skipping push endpoints for user %s because webPushEnabled is false",
                            user_id,
                        )
            if user_preference_cache[user_id]:
                yield endpoint_doc

    def query_active_push_endpoints_for_user(self, user_id: str):
        """Query active web push endpoints for one user when web push is enabled."""
        if not user_id:
            return
        user_document = self.db.collection("users").document(user_id).get()
        user_payload = user_document.to_dict() or {}
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

        query = (
            self.db.collection("users")
            .document(user_id)
            .collection("push_endpoints")
            .where(filter=FieldFilter("active", "==", True))
        )
        for endpoint_doc in query.stream():
            yield endpoint_doc

    def new_poll_event_handler(self, am: ActionMan, poll_id: PollId) -> None:
        action_document = self.db.collection("open_actions").document(poll_id)
        action_snapshot = cast(DocumentSnapshot, action_document.get())
        poll_dict_raw = self.poll_repo.get_poll(poll_id)
        try:
            if not isinstance(poll_dict_raw, dict):
                raise TypeError("poll payload is not a dict")
            raw_date = poll_dict_raw["date"]
            if not isinstance(raw_date, str):
                raise TypeError("poll date is not a string")
            poll_date = raw_date
        except (KeyError, TypeError) as exc:
            _log.warning(
                "Poll %s has missing/invalid date for open push TTL; using default TTL path (%s)",
                poll_id,
                exc,
            )
            poll_date = ""
        canonical_open_key = PushDedupeKeys.open_key(poll_id)
        action_dict = _with_legacy_alias_key(
            action_snapshot.to_dict(),
            legacy_key=poll_id,
            canonical_key=canonical_open_key,
        )
        new_action_dict = am.action_event(
            action_dict=action_dict,
            action_key=canonical_open_key,
            poll_id=poll_id,
            poll_date=poll_date,
        )
        if new_action_dict:
            action_document.set(new_action_dict, merge=True)

    def complete_poll_event_handler(
        self, pubs_list, am: ActionMan, poll_id: PollId
    ) -> None:
        poll_dict_raw = self.poll_repo.get_poll(poll_id)
        action_document = self.db.collection("comp_actions").document(poll_id)
        if poll_dict_raw is None:
            return
        poll_dict = cast(PollDocument, poll_dict_raw)
        if "selected" not in poll_dict:
            _log.error("Poll document %s has no selected field", poll_id)
            return
        pub_id = poll_dict["selected"]
        if pub_id not in pubs_list:
            raise RetryablePollDataNotReadyError(
                "Poll "
                f"{poll_id} selected pub {pub_id} that is not in pubs_list. "
                "This usually indicates startup race while pubs list is warming."
            )
        action_snapshot = cast(DocumentSnapshot, action_document.get())
        canonical_complete_key = _compute_action_key(poll_id, poll_dict, pub_id)
        action_dict = action_snapshot.to_dict()
        new_action_dict = am.action_event(
            action_dict=action_dict,
            action_key=canonical_complete_key,
            poll_id=poll_id,
            poll_dict=poll_dict,
            pub_dict=pubs_list,
        )
        if new_action_dict:
            action_document.set(new_action_dict, merge=True)

    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query:
        """Query polls by completion state, optionally constrained by minimum date."""
        query = self.poll_repo.get_polls_by_status(completed=completed)
        if min_date is None:
            return query
        return query.where(filter=FieldFilter("date", ">=", min_date))

    @property
    def query_completed_true(self) -> Query:
        """Query completed polls."""
        return self.query_polls_by_status(completed=True)

    @property
    def query_completed_false(self) -> Query:
        """Query open (incomplete) polls."""
        return self.query_polls_by_status(completed=False)

    @property
    def query_all_polls(self) -> Query:
        """Return a query for all polls (no filters)."""
        return self.poll_repo.get_all_polls()

    @property
    def query_notification_requests(self) -> Query:
        """Return a query for notification request health-check documents."""
        return cast(Query, self.db.collection("notification_req"))

    @staticmethod
    def wrapped_callback(
        doc_snapshot: Sequence[DocumentSnapshot],
        changes: Sequence[DocumentChange],
        read_time: datetime,
        callback: Callable[[str, DocumentSnapshot], None],
        collection: CollectionReference,
    ) -> None:
        if collection.id == "users":
            raise ValueError("Users collection should not be watched here.")
        for change in changes:
            if change.type.name == "ADDED":
                callback(collection.id, change.document)
            elif change.type.name == "MODIFIED":
                callback(collection.id, change.document)
            elif change.type.name == "REMOVED":
                pass

    def all_events_except_users(
        self, callback: Callable[[str, DocumentSnapshot], None]
    ) -> None:
        collections = self.db.collections()
        collection: CollectionReference
        for collection in collections:
            if collection.id in ["users", "roles"]:
                continue
            bound_callback = partial(
                self.wrapped_callback, callback=callback, collection=collection
            )
            collection.on_snapshot(bound_callback)


def patch_watch_close(callback):
    orig_close = watch.Watch.close

    def new_close(self, reason=None):
        callback(reason)
        # Call the original close
        return orig_close(self, reason)

    watch.Watch.close = new_close
