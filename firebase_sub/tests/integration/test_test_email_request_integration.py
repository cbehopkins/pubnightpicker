import pytest

from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.test_email_request import TestEmailRequestHandler


@pytest.mark.integration
def test_test_email_request_sends_and_acks(firestore_client):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "email": "login@example.com",
            "notificationEmail": "alerts@example.com",
            "testEmailReq": "uuid-1",
        }
    )
    sent: list[str] = []

    def sender(email: str, *, dummy_run: bool) -> bool:
        del dummy_run
        sent.append(email)
        return True

    handler = TestEmailRequestHandler(firestore_client, sender=sender)
    snapshot = firestore_client.collection("users").document("u1").get()

    handler.handle_request_document(snapshot)
    handler.handle_request_document(snapshot)

    user = firestore_client.collection("users").document("u1").get().to_dict()
    assert user["testEmailAck"] == "uuid-1"
    assert user["email"] == "login@example.com"
    assert sent == ["alerts@example.com"]


@pytest.mark.integration
def test_test_email_request_query_only_returns_requesting_users(firestore_client):
    users = firestore_client.collection("users")
    users.document("u1").set({"uid": "u1", "testEmailReq": "uuid-1"})
    users.document("u2").set({"uid": "u2"})

    query = DbHandler().query_test_email_requests

    assert [doc.id for doc in query.stream()] == ["u1"]
