import enum
import logging
from collections.abc import Callable
from dataclasses import dataclass

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionMan
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.pubs_list import PubsList

_log = logging.getLogger(__name__)


class EventType(enum.StrEnum):
    NEW_POLL = "new_poll"
    COMP_POLL = "comp_poll"
    TICK = "tick"
    PUSH_TEST = "push_test"
    PUSH = "push"
    CHAT_MESSAGE = "chat_message"


@dataclass
class Event:
    type: EventType
    doc: DocumentSnapshot | None
    callback: Callable[[DocumentSnapshot | None, PubsList], None]

    def handle_queue_item(
        self,
        pubs_list: PubsList,
    ):
        self.callback(self.doc, pubs_list)
