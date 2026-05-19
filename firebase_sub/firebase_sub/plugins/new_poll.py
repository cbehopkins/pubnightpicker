from contextlib import AbstractContextManager, nullcontext
from collections.abc import Mapping
from typing import Any

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from typing import cast

from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.action_track import ActionMan
from firebase_sub.plugins.protocols import (
    EventPlugin,
    NewPollDbHandler,
)


class NewPollListenerPlugin(EventPlugin):
    """Listener plugin that processes NEW_POLL events for open polls.

    Implements EventPlugin contract with gated execution:
    - filter: checks if the poll action needs to run (via ActionMan)
    - handle: sends notifications (emails and push)
    - mark_done: updates action document state
    """

    def __init__(
        self,
        *,
        db_handler: NewPollDbHandler,
        action_manager: ActionMan,
    ) -> None:
        self._db_handler = db_handler
        self._action_manager = action_manager

    def name(self) -> str:
        return "new_poll_listener"

    def on_registered(self) -> None:
        return

    def on_unregistered(self) -> None:
        return

    def build_manager(self) -> AbstractContextManager[object]:
        """Events are now produced externally by event producers."""
        # No-op manager since Firestore watches are managed by event producers
        return nullcontext()

    def filter(self, envelope: EventEnvelope) -> bool:
        """Check if the poll action needs to run.

        Queries the action document to determine if any action (email/push)
        is still pending for this poll.

        Returns:
            True if at least one action type needs to run; False otherwise.
        """
        if envelope.doc is None or envelope.type != EventType.NEW_POLL:
            return False

        poll_id = envelope.document_id()
        if poll_id is None:
            return False

        action_document = self._action_document(poll_id)
        action_snapshot = self._snapshot_get(action_document)
        action_dict = action_snapshot.to_dict() or {}
        open_action_key = poll_id

        return self._action_manager.filter(
            action_dict=action_dict,
            action_key=open_action_key,
        )

    def handle(self, envelope: EventEnvelope) -> None:
        """Execute the poll open actions (email and push notifications).

        Calls the registered ActionMan callbacks to send notifications.
        Does not update state; that's done by mark_done.
        """
        if envelope.doc is None or envelope.type != EventType.NEW_POLL:
            return

        poll_id = envelope.document_id()
        if poll_id is None:
            return

        # Get poll data for the callbacks
        poll_dict_raw = self._db_handler.poll_repo.get_poll(poll_id)
        if poll_dict_raw is None:
            return

        try:
            raw_date = poll_dict_raw["date"]
            poll_date = raw_date
        except (KeyError, TypeError):
            poll_date = ""

        # Run the action callbacks
        open_action_key = poll_id

        action_document = self._action_document(poll_id)
        action_snapshot = self._snapshot_get(action_document)
        action_dict = action_snapshot.to_dict() or {}

        # Run the action_event to trigger callbacks
        # (This will call the email/push callbacks for any pending actions)
        self._action_manager.action_event(
            action_dict=action_dict,
            action_key=open_action_key,
            poll_id=poll_id,
            poll_date=poll_date,
        )

    def mark_done(self, envelope: EventEnvelope) -> None:
        """Update the action document state after successful handler execution.

        Marks all action types as completed so filter won't trigger again.
        """
        if envelope.doc is None or envelope.type != EventType.NEW_POLL:
            return

        poll_id = envelope.document_id()
        if poll_id is None:
            return

        action_document = self._action_document(poll_id)
        action_snapshot = self._snapshot_get(action_document)
        action_dict = action_snapshot.to_dict() or {}
        open_action_key = poll_id

        new_action_dict = self._action_manager.mark_done(
            action_dict=action_dict,
            action_key=open_action_key,
        )
        self._document_set(action_document, new_action_dict, merge=True)

    def _action_document(self, poll_id: str):
        return self._db_handler.db.collection("open_actions").document(poll_id)

    @staticmethod
    def _snapshot_get(document_ref: object) -> DocumentSnapshot:
        raw_snapshot = cast(Any, document_ref).get()
        if not isinstance(raw_snapshot, DocumentSnapshot):
            raise TypeError(
                "Expected synchronous DocumentSnapshot from Firestore get()"
            )
        return raw_snapshot

    @staticmethod
    def _document_set(
        document_ref: object,
        payload: Mapping[str, object],
        *,
        merge: bool,
    ) -> None:
        cast(Any, document_ref).set(payload, merge=merge)

    def _new_poll_handler(
        self,
        document: DocumentSnapshot | None,
        pubs_list: PubsList,
    ) -> None:
        """Legacy callback-based handler (kept for backward compatibility)."""
        del pubs_list
        if document is None:
            raise ValueError(
                "New Event has no document. This indicates a coding error."
            )
        self._db_handler.new_poll_event_handler(
            self._action_manager, poll_id=document.id
        )
