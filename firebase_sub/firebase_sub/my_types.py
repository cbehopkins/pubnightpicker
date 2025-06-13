from enum import StrEnum, auto
import enum
from typing import Callable

from google.cloud.firestore_v1.base_document import DocumentSnapshot

ActionTypeKey = str


class ActionType(StrEnum):
    EMAIL = auto()
    PEMAIL = auto()


EmailAddr = str
DocumentId = str
PollId = DocumentId
UserId = DocumentId
PubId = DocumentId
ActionDict = dict[ActionTypeKey, set[DocumentId]]

Callback = Callable[[], None] | None
DocCallback = Callable[[DocumentSnapshot], None] | None
