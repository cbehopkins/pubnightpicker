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

    def __enter__(self) -> "PubsList":
        """Start watching the pubs collection."""
        self._poll_manager.__enter__()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: object,
    ) -> None:
        """Stop watching the pubs collection."""
        self._poll_manager.__exit__(exc_type, exc_val, exc_tb)

    def start_periodic_restart(self, minutes: int) -> None:
        """Retained for compatibility; periodic watch restarts are disabled."""
        del minutes
        raise NotImplementedError("Periodic watch restarts are disabled for PubsList.")

    def _add(self, document: DocumentSnapshot) -> None:
        from datetime import date

        from firebase_sub.database.event_recurrence import (
            materialized_next_occurrence_iso_state,
        )

        data = document.to_dict()
        if data is None:
            return
        recurrence = data.get("recurrence")
        _, next_iso = materialized_next_occurrence_iso_state(
            recurrence,
            data.get("next_occurrence_date"),
            today=date.today(),
        )
        if data.get("next_occurrence_date") != next_iso:
            document.reference.set({"next_occurrence_date": next_iso}, merge=True)
        if next_iso is None:
            data.pop("next_occurrence_date", None)
        else:
            data["next_occurrence_date"] = next_iso
        self._dict[document.id] = cast(VenueDocument, data)

    def _remove(self, document: DocumentSnapshot) -> None:
        del self._dict[document.id]
