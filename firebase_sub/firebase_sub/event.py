import enum
import logging
import time
from collections.abc import Callable
from dataclasses import dataclass

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionMan
from firebase_sub.database.handlers import DbHandler, RetryablePollDataNotReadyError
from firebase_sub.database.pubs_list import PubsList


_log = logging.getLogger(__name__)
_COMP_POLL_MAX_RETRIES = 10
_COMP_POLL_RETRY_DELAY_SECONDS = 1.0


class EventType(enum.StrEnum):
    NEW_POLL = "new_poll"
    COMP_POLL = "comp_poll"
    TICK = "tick"


@dataclass
class Event:
    type: EventType
    doc: DocumentSnapshot | None
    callback: Callable[[], None] | None = None

    def handle_queue_item(
        self,
        db_handler: DbHandler,
        pubs_list: PubsList,
        open_am: ActionMan,
        complete_am: ActionMan,
    ):
        if self.type == EventType.TICK:
            if self.callback is None:
                raise ValueError(
                    "Tick event requires callback. " "This indicates a wiring error."
                )
            self.callback()
            return

        if self.doc is None:
            raise ValueError(
                f"Event has no document: type={self.type}. This indicates a coding error."
            )
        match self.type:
            case EventType.NEW_POLL:
                db_handler.new_poll_event_handler(open_am, poll_id=self.doc.id)
            case EventType.COMP_POLL:
                for attempt in range(1, _COMP_POLL_MAX_RETRIES + 1):
                    try:
                        db_handler.complete_poll_event_handler(
                            pubs_list, complete_am, poll_id=self.doc.id
                        )
                        break
                    except RetryablePollDataNotReadyError:
                        if attempt == _COMP_POLL_MAX_RETRIES:
                            raise
                        _log.warning(
                            "Retrying completed poll event for %s after pubs not ready "
                            "(attempt %s/%s)",
                            self.doc.id,
                            attempt,
                            _COMP_POLL_MAX_RETRIES,
                        )
                        time.sleep(_COMP_POLL_RETRY_DELAY_SECONDS)
