import time
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query

from firebase_sub.database.poll_manager import PollManager
from firebase_sub.my_types import Callback


class PubsList(PollManager):
    def __init__(
        self,
        collection: CollectionReference,
    ):
        self.pub_collection = collection
        self.unsubscribe: Callback = None
        self._dict = {}
        super().__init__(
            query=cast(Query, collection),
            add=self._add,
            modify=self._add,
            rm=self._remove,
        )

    def __getitem__(self, key: Any) -> Any:
        if key not in self._dict:
            # Just in case the database hasn't populated yet
            time.sleep(5)
        return self._dict[key]

    def __contains__(self, key: object) -> bool:
        if key not in self._dict:
            # Just in case the database hasn't populated yet
            time.sleep(5)
        return key in self._dict

    def _add(self, document: DocumentSnapshot) -> None:
        self._dict[document.id] = document.to_dict()

    def _remove(self, document: DocumentSnapshot) -> None:
        del self._dict[document.id]
