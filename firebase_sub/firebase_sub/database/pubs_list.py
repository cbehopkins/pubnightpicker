from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query

from firebase_sub.database.poll_manager import PollManager
from firebase_sub.my_types import Callback, DocumentId, MissingPubError, VenueDocument


class PubsList:
    """Container for pub/venue documents with automatic Firestore sync.

    Maintains a dict-like interface for accessing pubs while delegating lifecycle
    management (watch, restart) to an internal PollManager instance.

    Usage:
        with PubsList(pub_collection) as pubs:
            pubs.start_periodic_restart(10)  # restart every 10 minutes
            venue = pubs["venue-id"]  # access like a dict
    """

    def __init__(
        self,
        collection: CollectionReference,
    ):
        self.pub_collection = collection
        self.unsubscribe: Callback = None
        self._dict: dict[DocumentId, VenueDocument] = {}
        self._poll_manager = PollManager(
            query=cast(Query, collection),
            add=self._add,
            modify=self._add,
            rm=self._remove,
        )

    def __getitem__(self, key: Any) -> VenueDocument:
        if key not in self._dict:
            raise MissingPubError(
                f"Pub ID {key!r} not found in loaded pubs list. "
                "This indicates a coding error or database consistency issue."
            )
        return self._dict[key]

    def __contains__(self, key: object) -> bool:
        return key in self._dict

    def __enter__(self):
        """Start watching the pubs collection."""
        self._poll_manager.__enter__()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Stop watching the pubs collection."""
        self._poll_manager.__exit__(exc_type, exc_val, exc_tb)

    def start_periodic_restart(self, minutes: int):
        """Start periodic restart of the watch every N minutes."""
        self._poll_manager.start_periodic_restart(minutes)
        return self

    def _add(self, document: DocumentSnapshot) -> None:
        data = document.to_dict()
        if data is None:
            return
        self._dict[document.id] = cast(VenueDocument, data)

    def _remove(self, document: DocumentSnapshot) -> None:
        del self._dict[document.id]
