import enum
import logging
from dataclasses import dataclass

from google.cloud.firestore_v1.base_document import DocumentSnapshot

_log = logging.getLogger(__name__)


class EventType(enum.StrEnum):
    NEW_POLL = "new_poll"
    COMP_POLL = "comp_poll"
    TICK = "tick"
    PUSH_TEST = "push_test"
    PUSH = "push"
    CHAT_MESSAGE = "chat_message"
    ADMIN_DELETE_REQUEST = "admin_delete_request"


@dataclass
class EventEnvelope:
    """Event envelope carrying metadata for dispatcher-based routing.

    The envelope separates routing/dispatch concern from callback-driven
    execution. It carries stable identifiers and timestamps while avoiding
    direct Firestore snapshot dependency in dispatcher interfaces.
    """

    type: EventType
    doc: DocumentSnapshot | None

    def document_id(self) -> str | None:
        """Return the document ID if present."""
        return self.doc.id if self.doc is not None else None


@dataclass
class Event:
    type: EventType
    doc: DocumentSnapshot | None
