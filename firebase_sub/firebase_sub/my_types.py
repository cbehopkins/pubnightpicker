from enum import StrEnum, auto


class ActionType(StrEnum):
    TOOT = auto()
    EMAIL = auto()
    PEMAIL = auto()


DocumentId = str
PollId = DocumentId
PubId = DocumentId
ActionDict = dict[ActionType, set[DocumentId]]
