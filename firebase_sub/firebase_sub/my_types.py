import datetime
from collections.abc import Callable, Sequence
from enum import StrEnum, auto
from typing import Literal, NotRequired, TypedDict

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange

ActionTypeKey = str
CollectionSnapshotCallback = Callable[
    [Sequence[DocumentSnapshot], Sequence[DocumentChange], datetime.datetime], None
]


class ActionType(StrEnum):
    EMAIL = auto()
    PEMAIL = auto()
    PUSH = auto()


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
    restaurant_time: NotRequired[str]


class EventRecurrenceRule(TypedDict, total=False):
    frequency: Literal["once", "weekly", "monthly", "yearly"]
    start_date: str
    date: str
    interval: int
    weekdays: list[int]
    weekday: int
    nth: int
    month: int
    month_day: int


class VenueDocument(TypedDict):
    name: str
    venueType: NotRequired[str]
    web_site: NotRequired[str]
    address: NotRequired[str]
    map: NotRequired[str]
    recurrence: NotRequired[EventRecurrenceRule]
    next_occurrence_date: NotRequired[str]
    recurrence_last_materialized_date: NotRequired[str]


Callback = Callable[[], None] | None
DocCallback = Callable[[DocumentSnapshot], None] | None


class MissingPubError(KeyError):
    """Raised when a pub/venue is not found in the pubs list.

    Subclasses KeyError so existing exception handlers continue to work.
    Indicates a coding error or database consistency issue.
    """
