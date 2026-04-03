import enum
from dataclasses import dataclass
from collections.abc import Callable

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionMan
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.pubs_list import PubsList


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
                    "Tick event requires callback. "
                    "This indicates a wiring error."
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
                db_handler.complete_poll_event_handler(
                    pubs_list, complete_am, poll_id=self.doc.id
                )
