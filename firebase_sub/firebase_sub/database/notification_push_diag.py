import logging
from collections.abc import Callable, Iterable
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.transforms import DELETE_FIELD

from firebase_sub.send_push import send_diagnostic_push

_log = logging.getLogger(__name__)

PUSH_TEST_DOC_ID = "push_test"
NOTIFICATION_REQ_COLLECTION = "notification_req"
NOTIFICATION_ACK_COLLECTION = "notification_ack"


class NotificationPushTestHandler:
    """Process notification_req/push_test uid->timestamp requests."""

    def __init__(
        self,
        db,
        query_active_push_endpoints_for_user: Callable[
            [str], Iterable[DocumentSnapshot]
        ],
        *,
        dummy_push: bool = False,
    ):
        self.db = db
        self.query_active_push_endpoints_for_user = query_active_push_endpoints_for_user
        self.dummy_push = dummy_push

    def _ack_document(self):
        return self.db.collection(NOTIFICATION_ACK_COLLECTION).document(
            PUSH_TEST_DOC_ID
        )

    def _request_document(self):
        return self.db.collection(NOTIFICATION_REQ_COLLECTION).document(
            PUSH_TEST_DOC_ID
        )

    def _delete_request_key(self, uid: str) -> None:
        try:
            self._request_document().set({uid: DELETE_FIELD}, merge=True)
        except Exception:
            _log.exception(
                "Failed to clear processed push test request key for uid=%s", uid
            )

    def is_push_test_request(self, request_document: DocumentSnapshot | None) -> bool:
        if request_document is None:
            raise ValueError("request_document cannot be None")
        return request_document.id == PUSH_TEST_DOC_ID

    def handle_request_document(
        self, request_document: DocumentSnapshot | None
    ) -> None:
        if request_document is None:
            raise ValueError("request_document cannot be None")
        if request_document.id != PUSH_TEST_DOC_ID:
            raise ValueError(
                f"request_document must have id={PUSH_TEST_DOC_ID!r}; got {request_document.id!r}"
            )
        request_payload = cast(dict[str, Any] | None, request_document.to_dict()) or {}
        ack_document = self._ack_document()
        ack_snapshot = cast(DocumentSnapshot, ack_document.get())
        ack_payload = cast(dict[str, Any] | None, ack_snapshot.to_dict()) or {}

        for uid, request_value in request_payload.items():
            if not isinstance(uid, str) or not uid:
                continue
            if ack_payload.get(uid) == request_value:
                # Consume duplicate stale requests so they are not retried later.
                self._delete_request_key(uid)
                continue

            try:
                result = send_diagnostic_push(
                    user_id=uid,
                    request_value=request_value,
                    endpoints_src=lambda uid=uid: self.query_active_push_endpoints_for_user(
                        uid
                    ),
                    dummy_run=self.dummy_push,
                )
            except Exception:
                _log.exception("Push test request failed for uid=%s", uid)
                self._delete_request_key(uid)
                continue

            if result.delivered <= 0:
                _log.warning(
                    "Push test request produced no deliveries for uid=%s (invalid=%s)",
                    uid,
                    result.invalid,
                )
                self._delete_request_key(uid)
                continue

            ack_document.set({uid: request_value}, merge=True)
            self._delete_request_key(uid)
            _log.info("Push test request acknowledged for uid=%s", uid)
