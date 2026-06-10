from typing import cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionMan
from firebase_sub.common.retry import retry
from firebase_sub.database.handlers import RetryablePollDataNotReadyError
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.my_types import ActionDict, PollId
from firebase_sub.plugins.protocols import CompletePollDbHandler, EventPlugin
from firebase_sub.push_contract import PushDedupeKeys


class CompletePollListenerPlugin(EventPlugin):
    """Listener plugin that processes COMP_POLL events for completed polls."""

    def __init__(
        self,
        *,
        db_handler: CompletePollDbHandler,
        action_manager: ActionMan,
        max_retries: int,
        retry_delay_seconds: float,
    ) -> None:
        self._db_handler = db_handler
        self._action_manager = action_manager
        self._pubs_list: PubsList | None = None
        self._pending_updates: dict[PollId, ActionDict] = {}

        @retry(
            retry_errors=(RetryablePollDataNotReadyError,),
            max_retries=max_retries,
            delay_seconds=retry_delay_seconds,
            operation_name="completed poll event after pubs not ready",
        )
        def _retrying_handler(
            document: DocumentSnapshot | None,
            pubs_list: PubsList,
        ) -> None:
            self._run_complete_poll_handler(document=document, pubs_list=pubs_list)

        self._retrying_handler = _retrying_handler

    def name(self) -> str:
        return "complete_poll_listener"

    def on_registered(self) -> None:
        return

    def on_unregistered(self) -> None:
        return

    def set_pubs_list(self, pubs_list: PubsList) -> None:
        """Bind runtime pubs cache required by complete-poll handlers."""
        self._pubs_list = pubs_list

    def filter(self, envelope: EventEnvelope) -> bool:
        """Check if complete-poll actions still need to run for this event."""
        if envelope.doc is None or envelope.type != EventType.COMP_POLL:
            return False

        poll_id = envelope.document_id()
        if poll_id is None:
            return False

        poll_dict_raw = self._db_handler.poll_repo.get_poll(poll_id)
        if not isinstance(poll_dict_raw, dict):
            return False
        poll_dict = poll_dict_raw

        if "selected" not in poll_dict:
            return False
        pub_id = poll_dict["selected"]

        action_dict = self._db_handler.action_dict(poll_id)
        complete_action_key = PushDedupeKeys.complete_key(
            pub_id=pub_id,
            restaurant_id=poll_dict.get("restaurant"),
            restaurant_time=poll_dict.get("restaurant_time"),
        )
        return self._action_manager.filter(
            action_dict=action_dict,
            action_key=complete_action_key,
        )

    def handle(self, envelope: EventEnvelope) -> None:
        """Run complete-poll handler with retry semantics."""
        if envelope.doc is None or envelope.type != EventType.COMP_POLL:
            return

        if self._pubs_list is None:
            raise RetryablePollDataNotReadyError(
                "complete_poll listener has no pubs_list bound"
            )

        self._retrying_handler(envelope.doc, self._pubs_list)

    def mark_done(self, envelope: EventEnvelope) -> None:
        """Persist success state after handle."""
        if envelope.doc is None or envelope.type != EventType.COMP_POLL:
            return

        poll_id = envelope.document_id()
        if poll_id is None:
            return

        pending_update = self._pending_updates.pop(poll_id, None)
        if not pending_update:
            return

        self._db_handler.mark_done(poll_id, pending_update)

    def _run_complete_poll_handler(
        self,
        *,
        document: DocumentSnapshot | None,
        pubs_list: PubsList,
    ) -> None:
        if document is None:
            raise ValueError(
                "Completed Event has no document. This indicates a coding error."
            )

        poll_id = document.id
        poll_dict_raw = self._db_handler.poll_repo.get_poll(poll_id)
        if poll_dict_raw is None:
            # No poll of that ID, so clear any associated updates
            self._pending_updates.pop(poll_id, None)
            return
        poll_dict = poll_dict_raw
        if "selected" not in poll_dict:
            # Poll has no selected pub, so clear any associated updates
            self._pending_updates.pop(poll_id, None)
            return
        pub_id = poll_dict["selected"]
        if pub_id not in pubs_list:
            raise RetryablePollDataNotReadyError(
                "Poll "
                f"{poll_id} selected pub {pub_id} that is not in pubs_list. "
                "This usually indicates startup race while pubs list is warming."
            )

        action_dict = self._db_handler.action_dict(poll_id)
        complete_action_key = PushDedupeKeys.complete_key(
            pub_id=pub_id,
            restaurant_id=poll_dict.get("restaurant"),
            restaurant_time=poll_dict.get("restaurant_time"),
        )
        new_action_dict = self._action_manager.action_event(
            action_dict=action_dict,
            action_key=complete_action_key,
            poll_id=poll_id,
            poll_dict=poll_dict,
            pub_dict=cast(dict[str, dict[str, object]], pubs_list),
        )
        if new_action_dict is None:
            # Nothing needed to be actioned, so ensure any pending update is cleared
            self._pending_updates.pop(poll_id, None)
        else:
            self._pending_updates[poll_id] = new_action_dict
