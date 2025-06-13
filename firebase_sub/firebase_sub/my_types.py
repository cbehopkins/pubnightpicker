import datetime
from enum import StrEnum, auto
from typing import Callable, Sequence

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange

ActionTypeKey = str
CollectionSnapshotCallback = Callable[
    [Sequence[DocumentSnapshot], Sequence[DocumentChange], datetime.datetime], None
]


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
