from functools import partial
import logging
from typing import Callable, Generator, Sequence, cast
from datetime import datetime
from firebase_admin import firestore
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1 import watch
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.action_track import ActionMan
from firebase_sub.my_types import EmailAddr, PollId, UserId

_log = logging.getLogger(__name__)


class DbHandler:
    def __init__(self):
        self.db: Client = firestore.client()
        patch_watch_close(self.my_watch_close_callback)
        self.okay = True

    def my_watch_close_callback(self, reason):
        _log.error(f"Firestore Watch closed! Reason: {reason}")
        self.okay = False
        # This happens in a different thread - so we are blocked unable to exit
        # https://github.com/googleapis/python-firestore/issues/882
        raise SystemExit("Exiting due to watch close.")

    @property
    def pub_collection(self) -> CollectionReference:
        return self.db.collection("pubs")

    @property
    def polls_collection(self) -> CollectionReference:
        return self.db.collection("polls")

    def query_personal_emails(self) -> Generator[tuple[EmailAddr, UserId], None, None]:
        docs_query = self.db.collection("users").where(
            filter=FieldFilter("notificationEmailEnabled", "==", True)
        )
        _log.info("Generating personal email addresses")
        for doc in docs_query.stream():
            record = doc.to_dict()
            pemail = record["notificationEmail"]
            _log.debug(f"{pemail}")
            yield pemail, record["uid"]
        _log.error("Steam closed in query_personal_emails")

    def query_open_emails(self) -> Generator[tuple[EmailAddr, UserId], None, None]:
        docs_query = self.db.collection("users").where(
            filter=FieldFilter("openPollEmailEnabled", "==", True)
        )
        _log.info("Generating open email addresses")
        for doc in docs_query.stream():
            record = doc.to_dict()
            pemail = record["notificationEmail"]
            _log.debug(f"{pemail}")
            yield pemail, record["uid"]
        _log.error("Steam closed in query_open_emails")

    def new_poll_event_handler(self, am: ActionMan, poll_id: PollId) -> None:
        action_document = self.db.collection("open_actions").document(poll_id)
        new_action_dict = am.action_event(
            action_dict=action_document.get().to_dict(),
            action_key=poll_id,
        )
        if new_action_dict:
            action_document.set(new_action_dict, merge=True)

    def complete_poll_event_handler(
        self, pubs_list, am: ActionMan, poll_id: PollId
    ) -> None:
        polls_document = self.polls_collection.document(poll_id)
        action_document = self.db.collection("comp_actions").document(poll_id)

        poll_dict = polls_document.get().to_dict()
        if poll_dict is None:
            _log.error(f"Poll document {poll_id} not found or is empty.")
            return
        pub_id = poll_dict["selected"]
        assert (
            pub_id in pubs_list
        ), f"Someone selected a pub that's not in the pub dict, {pub_id=}"
        new_action_dict = am.action_event(
            action_dict=action_document.get().to_dict(),
            action_key=pub_id,
            poll_dict=poll_dict,
            pub_dict=pubs_list,
        )
        if new_action_dict:
            action_document.set(new_action_dict, merge=True)

    @property
    def query_completed_true(self) -> Query:
        return self.polls_collection.where(filter=FieldFilter("completed", "==", True))

    @property
    def query_completed_false(self) -> Query:
        return self.polls_collection.where(filter=FieldFilter("completed", "==", False))

    @property
    def query_all_polls(self) -> Query:
        """Return a query for all polls (no filters)."""
        return cast(Query, self.polls_collection)

    @staticmethod
    def wrapped_callback(
        doc_snapshot: Sequence[DocumentSnapshot],
        changes: Sequence[DocumentChange],
        read_time: datetime,
        callback: Callable[[str, DocumentSnapshot], None],
        collection: CollectionReference,
    ) -> None:
        assert collection.id != "users", "Users collection should not be watched here."
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
