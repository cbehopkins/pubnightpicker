import logging
from typing import Any, Mapping, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client

_log = logging.getLogger(__name__)


class NotificationAckMirrorHandler:
    """Mirror request document keys/values into matching ack documents."""

    def __init__(
        self,
        db: Client,
        request_collection_name: str = "notification_req",
        ack_collection_name: str = "notification_ack",
    ):
        self.db = db
        self.request_collection_name = request_collection_name
        self.ack_collection_name = ack_collection_name

    @staticmethod
    def _build_patch(
        request_payload: Mapping[str, Any], ack_payload: Mapping[str, Any]
    ) -> dict[str, Any]:
        return {
            key: value
            for key, value in request_payload.items()
            if key not in ack_payload or ack_payload[key] != value
        }

    def mirror_request_document(self, request_document: DocumentSnapshot) -> None:
        doc_id = request_document.id
        try:
            request_payload = cast(dict[str, Any] | None, request_document.to_dict())
            if request_payload is None:
                request_payload = {}

            ack_document = self.db.collection(self.ack_collection_name).document(doc_id)
            ack_snapshot = cast(DocumentSnapshot, ack_document.get())
            ack_payload = cast(dict[str, Any] | None, ack_snapshot.to_dict())
            if ack_payload is None:
                ack_payload = {}

            patch = self._build_patch(request_payload=request_payload, ack_payload=ack_payload)
            if not patch:
                _log.info("Notification mirror no-op for doc %s (already in sync)", doc_id)
                return

            ack_document.set(patch, merge=True)
            _log.info(
                "Notification mirror: mirrored %s keys for doc %s",
                len(patch),
                doc_id,
            )
        except Exception:
            _log.exception("Notification mirror failed for doc %s", doc_id)
