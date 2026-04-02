"""Repository layer for database access. Abstracts Firestore specifics from business logic.

This module provides abstraction interfaces and implementations for accessing polls and user data,
decoupling business logic from Firebase/Firestore internals.
"""

import logging
from typing import Generator, Protocol

from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query

from firebase_sub.my_types import EmailAddr, PollDocument, PollId, UserId

_log = logging.getLogger(__name__)


class PollRepository(Protocol):
    """Interface for poll data access.

    Implementers provide methods to query and retrieve poll documents from persistent storage.
    """

    def get_poll(self, poll_id: PollId) -> PollDocument | None:
        """Get a single poll by ID.

        Args:
            poll_id: The ID of the poll to retrieve

        Returns:
            The poll document, or None if not found or document is empty
        """
        ...

    def get_polls_by_status(self, completed: bool) -> Query:
        """Get a query for polls filtered by completion status.

        Args:
            completed: If True, return query for completed polls. If False, for open polls.

        Returns:
            A Query object that can be used with watch() or stream()
        """
        ...

    def get_all_polls(self) -> Query:
        """Get a query for all polls (no filters)."""
        ...

    def update_poll_action(
        self,
        poll_id: PollId,
        action_dict: dict | None,
    ) -> None:
        """Update the action record for a poll.

        This is used to track which notifications have been sent for a poll.
        """
        ...


class UserRepository(Protocol):
    """Interface for user data access.

    Implementers provide methods to query user email addresses and preferences.
    """

    def query_users_by_email_preference(
        self, preference: str
    ) -> Generator[tuple[EmailAddr, UserId], None, None]:
        """Query users by email notification preference.

        Args:
            preference: The field name to filter on, e.g. "notificationEmailEnabled",
                       "openPollEmailEnabled"

        Yields:
            Tuples of (email_address, user_id)

        Raises:
            ValueError: If the preference field is invalid
        """
        ...


class FirestorePollRepository:
    """Firestore implementation of PollRepository."""

    def __init__(self, db: Client):
        self.db = db
        self._polls_collection: CollectionReference | None = None

    @property
    def polls_collection(self) -> CollectionReference:
        if self._polls_collection is None:
            self._polls_collection = self.db.collection("polls")
        return self._polls_collection

    def get_poll(self, poll_id: PollId) -> PollDocument | None:
        """Retrieve a single poll document."""
        doc = self.polls_collection.document(poll_id).get()
        poll_dict = doc.to_dict()
        if poll_dict is None:
            _log.error(f"Poll document {poll_id} not found or is empty.")
            return None
        return poll_dict

    def get_polls_by_status(self, completed: bool) -> Query:
        """Return query for polls filtered by completion status."""
        return self.polls_collection.where(
            filter=FieldFilter("completed", "==", completed)
        )

    def get_all_polls(self) -> Query:
        """Return query for all polls."""
        return self.polls_collection

    def update_poll_action(
        self,
        poll_id: PollId,
        action_dict: dict | None,
    ) -> None:
        """Update the action record for a poll."""
        if action_dict:
            action_document = self.db.collection("comp_actions").document(poll_id)
            action_document.set(action_dict, merge=True)


class FirestoreUserRepository:
    """Firestore implementation of UserRepository."""

    # Valid email preference fields that can be queried
    _VALID_PREFERENCES = {"notificationEmailEnabled", "openPollEmailEnabled"}

    def __init__(self, db: Client):
        self.db = db

    def query_users_by_email_preference(
        self, preference: str
    ) -> Generator[tuple[EmailAddr, UserId], None, None]:
        """Query users by email notification preference.

        Supports:
        - "notificationEmailEnabled": users who want personal email notifications
        - "openPollEmailEnabled": users who want notifications when polls open
        """
        if preference not in self._VALID_PREFERENCES:
            raise ValueError(
                f"Unknown email preference: {preference!r}. "
                f"Valid options: {self._VALID_PREFERENCES}"
            )

        docs_query = self.db.collection("users").where(
            filter=FieldFilter(preference, "==", True)
        )
        for doc in docs_query.stream():
            record = doc.to_dict()
            if record is None:
                _log.warning("Skipping users doc %s with no payload", doc.id)
                continue
            email = record.get("notificationEmail")
            uid = record.get("uid")
            if email and uid:
                _log.debug(f"User {uid}: {email}")
                yield email, uid
            else:
                _log.warning(
                    f"User doc {doc.id} missing email or uid fields: email={email}, uid={uid}"
                )
