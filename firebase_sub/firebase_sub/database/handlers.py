import logging
from datetime import datetime
from functools import partial
from typing import Callable, Generator, Sequence, cast

from firebase_admin import firestore
from google.cloud.firestore_v1 import watch
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.action_track import ActionMan
from firebase_sub.database.repositories import (
    FirestorePollRepository,
    FirestoreUserRepository,
)
from firebase_sub.my_types import EmailAddr, PollDocument, PollId, UserId

_log = logging.getLogger(__name__)


def _compute_action_key(poll_dict: PollDocument, pub_id: str) -> str:
    """Build a composite action key encoding pub ID, restaurant ID, and meeting time.

    Storing this composite value in the comp_actions EMAIL set means that any change
    to the restaurant or its meeting time produces a new key and therefore triggers a
    fresh (rescheduled) notification, while an identical poll state is still deduplicated.
    """
    restaurant_id = poll_dict.get("restaurant") or ""
    restaurant_time = poll_dict.get("restaurant_time") or ""
    return f"{pub_id}:{restaurant_id}:{restaurant_time}"


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

    def new_poll_event_handler(self, am: ActionMan, poll_id: PollId) -> None:
        action_document = self.db.collection("open_actions").document(poll_id)
        action_snapshot = cast(DocumentSnapshot, action_document.get())
        new_action_dict = am.action_event(
            action_dict=action_snapshot.to_dict(),
            action_key=poll_id,
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
            raise ValueError(
                f"Poll {poll_id} selected pub {pub_id} that is not in pubs_list. "
                "This indicates a coding error or database consistency issue."
            )
        action_snapshot = cast(DocumentSnapshot, action_document.get())
        new_action_dict = am.action_event(
            action_dict=action_snapshot.to_dict(),
            action_key=_compute_action_key(poll_dict, pub_id),
            poll_dict=poll_dict,
            pub_dict=pubs_list,
        )
        if new_action_dict:
            action_document.set(new_action_dict, merge=True)

    @property
    def query_completed_true(self) -> Query:
        """Query completed polls."""
        return self.poll_repo.get_polls_by_status(completed=True)

    @property
    def query_completed_false(self) -> Query:
        """Query open (incomplete) polls."""
        return self.poll_repo.get_polls_by_status(completed=False)

    @property
    def query_all_polls(self) -> Query:
        """Return a query for all polls (no filters)."""
        return self.poll_repo.get_all_polls()

    @property
    def query_notification_requests(self) -> Query:
        """Return a query for notification request health-check documents."""
        return self.db.collection("notification_req").order_by("__name__")

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
