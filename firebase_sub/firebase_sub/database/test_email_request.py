import logging
from collections.abc import Callable
from typing import Any, cast

from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client

from firebase_sub.my_types import EmailAddr
from firebase_sub.send_email import send_test_email

_log = logging.getLogger(__name__)

USERS_COLLECTION = "users"
TEST_EMAIL_REQ_FIELD = "testEmailReq"
TEST_EMAIL_ACK_FIELD = "testEmailAck"

type TestEmailSender = Callable[..., bool]


def resolve_test_email_address(payload: dict[str, Any]) -> EmailAddr | None:
    for field in ("notificationEmail", "email"):
        if isinstance(value := payload.get(field), str) and (value := value.strip()):
            return value
    return None


class TestEmailRequestHandler:
    """Send a test email when users/{uid}.testEmailReq differs from testEmailAck."""

    __test__ = False

    def __init__(
        self,
        db: Client,
        *,
        dummy_run: bool = False,
        sender: TestEmailSender = send_test_email,
    ) -> None:
        self._db = db
        self._dummy_run = dummy_run
        self._sender = sender

    def handle_request_document(self, document: DocumentSnapshot | None) -> None:
        if document is None:
            return
        user_ref = cast(
            Any, self._db.collection(USERS_COLLECTION).document(document.id)
        )
        # Re-read so rapid repeat requests coalesce onto the latest value.
        payload = cast(dict[str, Any] | None, user_ref.get().to_dict()) or {}

        request = payload.get(TEST_EMAIL_REQ_FIELD)
        if not isinstance(request, str) or not request:
            return
        if payload.get(TEST_EMAIL_ACK_FIELD) == request:
            return

        address = resolve_test_email_address(payload)
        if address is None:
            _log.warning(
                "Test email request for uid=%s has no email address; acknowledging",
                document.id,
            )
        else:
            try:
                sent = self._sender(address, dummy_run=self._dummy_run)
            except Exception:
                # Swallowed so user-controlled input cannot crash-loop the worker.
                _log.exception(
                    "Test email send failed for uid=%s; leaving request pending",
                    document.id,
                )
                return
            if not sent:
                _log.warning(
                    "Test email for uid=%s rate limited; leaving request pending",
                    document.id,
                )
                return

        user_ref.update({TEST_EMAIL_ACK_FIELD: request})
        _log.info("Test email request acknowledged for uid=%s", document.id)
