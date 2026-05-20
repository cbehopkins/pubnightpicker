import pytest

from firebase_sub.database.handlers import DbHandler


@pytest.mark.integration
def test_query_open_emails_returns_only_enabled(firestore_client):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "notificationEmail": "one@example.com",
            "openPollEmailEnabled": True,
        }
    )
    firestore_client.collection("users").document("u2").set(
        {
            "uid": "u2",
            "notificationEmail": "two@example.com",
            "openPollEmailEnabled": False,
        }
    )

    handler = DbHandler()

    emails = list(handler.query_open_emails())

    assert emails == [("one@example.com", "u1")]


@pytest.mark.integration
def test_query_personal_emails_returns_only_enabled(firestore_client):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "notificationEmail": "one@example.com",
            "notificationEmailEnabled": True,
        }
    )
    firestore_client.collection("users").document("u2").set(
        {
            "uid": "u2",
            "notificationEmail": "two@example.com",
            "notificationEmailEnabled": False,
        }
    )

    handler = DbHandler()

    emails = list(handler.query_personal_emails())

    assert emails == [("one@example.com", "u1")]


@pytest.mark.integration
def test_query_active_push_endpoints_returns_only_active(firestore_client):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "webPushEnabled": True,
        }
    )
    firestore_client.collection("users").document("u1").collection(
        "push_endpoints"
    ).document("ep-active").set(
        {
            "endpoint": "https://push.example/u1",
            "active": True,
        }
    )
    firestore_client.collection("users").document("u2").set(
        {
            "uid": "u2",
            "webPushEnabled": True,
        }
    )
    firestore_client.collection("users").document("u2").collection(
        "push_endpoints"
    ).document("ep-inactive").set(
        {
            "endpoint": "https://push.example/u2",
            "active": False,
        }
    )

    handler = DbHandler()
    docs = list(handler.query_active_push_endpoints("pollOpens"))

    assert len(docs) == 1
    assert docs[0].id == "ep-active"


@pytest.mark.integration
def test_query_active_push_endpoints_excludes_disabled_user_preference(
    firestore_client,
):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "webPushEnabled": True,
        }
    )
    firestore_client.collection("users").document("u2").set(
        {
            "uid": "u2",
            "webPushEnabled": False,
        }
    )

    firestore_client.collection("users").document("u1").collection(
        "push_endpoints"
    ).document("ep-u1").set(
        {
            "endpoint": "https://push.example/u1",
            "active": True,
        }
    )
    firestore_client.collection("users").document("u2").collection(
        "push_endpoints"
    ).document("ep-u2").set(
        {
            "endpoint": "https://push.example/u2",
            "active": True,
        }
    )

    handler = DbHandler()
    docs = list(handler.query_active_push_endpoints("pollOpens"))

    assert len(docs) == 1
    assert docs[0].id == "ep-u1"


@pytest.mark.integration
def test_query_active_push_endpoints_excludes_missing_user_preference(firestore_client):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "webPushEnabled": True,
        }
    )
    firestore_client.collection("users").document("u2").set(
        {
            "uid": "u2",
        }
    )

    firestore_client.collection("users").document("u1").collection(
        "push_endpoints"
    ).document("ep-u1").set(
        {
            "endpoint": "https://push.example/u1",
            "active": True,
        }
    )
    firestore_client.collection("users").document("u2").collection(
        "push_endpoints"
    ).document("ep-u2").set(
        {
            "endpoint": "https://push.example/u2",
            "active": True,
        }
    )

    handler = DbHandler()
    docs = list(handler.query_active_push_endpoints("pollOpens"))

    assert len(docs) == 1
    assert docs[0].id == "ep-u1"


@pytest.mark.integration
def test_query_active_push_endpoints_for_user_filters_active_without_index(
    firestore_client,
):
    firestore_client.collection("users").document("u1").set(
        {
            "uid": "u1",
            "webPushEnabled": True,
        }
    )

    firestore_client.collection("users").document("u1").collection(
        "push_endpoints"
    ).document("ep-active").set(
        {
            "endpoint": "https://push.example/u1-active",
            "active": True,
        }
    )
    firestore_client.collection("users").document("u1").collection(
        "push_endpoints"
    ).document("ep-inactive").set(
        {
            "endpoint": "https://push.example/u1-inactive",
            "active": False,
        }
    )

    handler = DbHandler()
    docs = list(handler.query_active_push_endpoints_for_user("u1"))

    assert len(docs) == 1
    assert docs[0].id == "ep-active"
