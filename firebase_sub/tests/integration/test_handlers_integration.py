from typing import cast

import pytest

from firebase_sub.action_track import ActionMan
from firebase_sub.database.handlers import DbHandler, RetryablePollDataNotReadyError
from firebase_sub.push_contract import PushDedupeKeys


class FakeActionMan:
    def __init__(self, return_value):
        self.return_value = return_value
        self.calls = []

    def action_event(self, **kwargs):
        self.calls.append(kwargs)
        return self.return_value


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
def test_complete_poll_event_handler_persists_action_doc(firestore_client):
    poll_id = "poll-1"
    selected_venue_id = "venue-1"

    firestore_client.collection("polls").document(poll_id).set(
        {
            "selected": selected_venue_id,
            "date": "2026-04-10",
            "completed": True,
            "restaurant": "rest-1",
        }
    )

    handler = DbHandler()
    pubs_list = {
        selected_venue_id: {"name": "The Waterman", "venueType": "pub"},
        "rest-1": {"name": "Starter Place", "venueType": "restaurant"},
    }
    fake_am = FakeActionMan({"email": [selected_venue_id]})

    handler.complete_poll_event_handler(
        pubs_list=pubs_list, am=cast(ActionMan, fake_am), poll_id=poll_id
    )

    action_doc = (
        firestore_client.collection("comp_actions").document(poll_id).get().to_dict()
    )

    assert action_doc is not None
    assert action_doc["email"] == [selected_venue_id]
    assert len(fake_am.calls) == 1
    assert fake_am.calls[0]["poll_dict"]["restaurant"] == "rest-1"
    assert fake_am.calls[0]["action_key"] == PushDedupeKeys.complete_key(
        pub_id=selected_venue_id,
        restaurant_id="rest-1",
        restaurant_time=None,
    )


@pytest.mark.integration
def test_new_poll_event_handler_uses_open_dedupe_key(firestore_client):
    poll_id = "poll-open-1"
    fake_am = FakeActionMan({"email": [PushDedupeKeys.open_key(poll_id)]})
    handler = DbHandler()

    handler.new_poll_event_handler(cast(ActionMan, fake_am), poll_id=poll_id)

    action_doc = (
        firestore_client.collection("open_actions").document(poll_id).get().to_dict()
    )

    assert action_doc is not None
    assert action_doc["email"] == [PushDedupeKeys.open_key(poll_id)]
    assert fake_am.calls[0]["action_key"] == PushDedupeKeys.open_key(poll_id)


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
    docs = list(handler.query_active_push_endpoints())

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
    docs = list(handler.query_active_push_endpoints())

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
    docs = list(handler.query_active_push_endpoints())

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


@pytest.mark.integration
def test_complete_poll_event_handler_no_selected_field_does_not_persist(
    firestore_client,
):
    poll_id = "poll-no-selected"
    firestore_client.collection("polls").document(poll_id).set(
        {
            "date": "2026-04-11",
            "completed": True,
        }
    )

    handler = DbHandler()
    fake_am = FakeActionMan({"email": ["unexpected"]})

    handler.complete_poll_event_handler(
        pubs_list={}, am=cast(ActionMan, fake_am), poll_id=poll_id
    )

    action_doc = (
        firestore_client.collection("comp_actions").document(poll_id).get().to_dict()
    )

    assert action_doc is None
    assert fake_am.calls == []


@pytest.mark.integration
def test_complete_poll_event_handler_raises_retryable_when_selected_pub_missing(
    firestore_client,
):
    poll_id = "poll-missing-pub"

    firestore_client.collection("polls").document(poll_id).set(
        {
            "selected": "missing-venue",
            "date": "2026-04-12",
            "completed": True,
        }
    )

    handler = DbHandler()
    fake_am = FakeActionMan({"email": ["missing-venue"]})

    with pytest.raises(RetryablePollDataNotReadyError, match="not in pubs_list"):
        handler.complete_poll_event_handler(
            pubs_list={}, am=cast(ActionMan, fake_am), poll_id=poll_id
        )
