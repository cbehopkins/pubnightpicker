import enum
from dataclasses import dataclass

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionMan
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.pubs_list import PubsList


class EventType(enum.StrEnum):
    NEW_POLL = "new_poll"
    COMP_POLL = "comp_poll"


@dataclass
class Event:
    type: EventType
    doc: DocumentSnapshot

    def handle_queue_item(
        self,
        db_handler: DbHandler,
        pubs_list: PubsList,
        open_am: ActionMan,
        complete_am: ActionMan,
    ):
        match self.type:
            case EventType.NEW_POLL:
                db_handler.new_poll_event_handler(open_am, poll_id=self.doc.id)
            case EventType.COMP_POLL:
                db_handler.complete_poll_event_handler(
                    pubs_list, complete_am, poll_id=self.doc.id
                )
