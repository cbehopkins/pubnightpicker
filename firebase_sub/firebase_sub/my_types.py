import datetime
from enum import StrEnum, auto
from typing import Callable, NotRequired, Sequence, TypedDict

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange

ActionTypeKey = str
CollectionSnapshotCallback = Callable[
    [Sequence[DocumentSnapshot], Sequence[DocumentChange], datetime.datetime], None
]


class ActionType(StrEnum):
    EMAIL = auto()
    PEMAIL = auto()


class VenueType(StrEnum):
    PUB = "pub"
    EVENT = "event"
    RESTAURANT = "restaurant"


EmailAddr = str
DocumentId = str
PollId = DocumentId
UserId = DocumentId
PubId = DocumentId
ActionDict = dict[ActionTypeKey, set[DocumentId]]


class PollDocument(TypedDict):
    selected: DocumentId
    date: str
    completed: NotRequired[bool]
    restaurant: NotRequired[DocumentId]


class VenueDocument(TypedDict):
    name: str
    venueType: NotRequired[str]
    web_site: NotRequired[str]
    address: NotRequired[str]
    map: NotRequired[str]


Callback = Callable[[], None] | None
DocCallback = Callable[[DocumentSnapshot], None] | None
