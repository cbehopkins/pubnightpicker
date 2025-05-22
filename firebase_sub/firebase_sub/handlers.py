
import logging

from typing import Generator, cast
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from firebase_sub.action_track import  ActionMan
from google.cloud.firestore_v1.query import Query
from firebase_sub.my_types import EmailAddr, PollId, UserId
from firebase_admin import firestore

_log = logging.getLogger(__name__)



class DbHandler:
    def __init__(self):
        self.db: Client = firestore.client()
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


    def new_poll_event_handler(self, am: ActionMan, poll_id: PollId) -> None:
        action_document = self.db.collection("open_actions").document(poll_id)
        new_action_dict = am.action_event(
            action_dict=action_document.get().to_dict(),
            action_key=poll_id,
        )
        if new_action_dict:
            action_document.set(new_action_dict, merge=True)


    def complete_poll_event_handler(self, pubs_list, am: ActionMan, poll_id: PollId) -> None:
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
    
