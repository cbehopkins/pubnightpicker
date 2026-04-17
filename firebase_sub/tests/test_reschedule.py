"""Tests for poll/restaurant rescheduling behaviour.

Phase 1 — pub rescheduling already works today.
Phase 2 — restaurant/time rescheduling requires the composite-key fix (Phase 3).
"""

from firebase_sub.action_track import ActionMan, ActionType
from firebase_sub.database.handlers import _compute_action_key
from firebase_sub.my_types import PollDocument, VenueDocument
from firebase_sub.send_email import send_ampub_email

# ---------------------------------------------------------------------------
# Helpers shared between tests
# ---------------------------------------------------------------------------


class _FakeAddress:
    def __init__(self, **kwargs):
        self.kwargs = kwargs


class _FakeMail:
    def __init__(self, **kwargs):
        self.kwargs = kwargs


class _FakeClient:
    def __init__(self):
        self.sent = []

    def send(self, mail):
        self.sent.append(mail)


def _pub_poll() -> PollDocument:
    return {"selected": "pub_A", "date": "2026-04-07"}


def _pub_dict() -> dict[str, VenueDocument]:
    return {"pub_A": {"name": "The Swan", "venueType": "pub"}}


# ---------------------------------------------------------------------------
# Phase 1 — pub rescheduling (should pass without any code changes)
# ---------------------------------------------------------------------------


def test_pub_reschedule_email_subject_has_rescheduled_prefix(monkeypatch):
    """When previously_actioned=True the subject line contains 'RESCHEDULED::'."""
    fake_client = _FakeClient()
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Mail", _FakeMail)
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Address", _FakeAddress)
    monkeypatch.setattr(
        "firebase_sub.send_email._mail_client", lambda dummy_run=True: fake_client
    )

    send_ampub_email(
        _pub_poll(),
        _pub_dict(),
        previously_actioned=True,
        emails_src=lambda: [("test@example.com", None)],
    )

    assert len(fake_client.sent) == 1
    assert fake_client.sent[0].kwargs["subject"] == "Pub Night @ RESCHEDULED::The Swan"


def test_pub_reschedule_email_body_has_rescheduled_text(monkeypatch):
    """When previously_actioned=True the body includes a rescheduled preamble."""
    fake_client = _FakeClient()
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Mail", _FakeMail)
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Address", _FakeAddress)
    monkeypatch.setattr(
        "firebase_sub.send_email._mail_client", lambda dummy_run=True: fake_client
    )

    send_ampub_email(
        _pub_poll(),
        _pub_dict(),
        previously_actioned=True,
        emails_src=lambda: [("test@example.com", None)],
    )

    body = fake_client.sent[0].kwargs["text"]
    assert "This week's event has been rescheduled" in body


def test_action_man_pub_change_fires_with_previously_actioned():
    """Changing the pub (different action_key) fires the callback again with previously_actioned=True."""
    am = ActionMan()
    calls: list[dict] = []

    def my_callback(*, previously_actioned: bool, dummy_run: bool, **kwargs):
        calls.append({"previously_actioned": previously_actioned})

    am.bind(ActionType.EMAIL, my_callback)

    # First run — pub_A, no prior action
    action_dict, actioned = am.run(action_dict={}, action_key="pub_A")
    assert actioned
    assert calls[-1]["previously_actioned"] is False

    # Second run — pub changes to pub_B (same action_dict ↦ pub_A already actioned)
    action_dict, actioned = am.run(action_dict=action_dict, action_key="pub_B")
    assert actioned
    assert calls[-1]["previously_actioned"] is True


def test_action_man_same_pub_fires_only_once():
    """Running for the same pub twice never fires the callback a second time."""
    am = ActionMan()
    run_count = 0

    def my_callback(*, previously_actioned: bool, dummy_run: bool, **kwargs):
        nonlocal run_count
        run_count += 1

    am.bind(ActionType.EMAIL, my_callback)

    action_dict, _ = am.run(action_dict={}, action_key="pub_A")
    am.run(action_dict=action_dict, action_key="pub_A")
    assert run_count == 1


# ---------------------------------------------------------------------------
# Phase 2 — composite key unit tests for _compute_action_key
#           (fail until _compute_action_key is extracted in handlers.py)
# ---------------------------------------------------------------------------


def test_compute_action_key_pub_only():
    """A poll with no restaurant produces a canonical complete key."""
    poll: PollDocument = {"selected": "pub_A", "date": "2026-04-01"}
    assert _compute_action_key("poll-1", poll, "pub_A") == "pub_A"


def test_compute_action_key_with_restaurant():
    """The restaurant ID is encoded in the key when present."""
    poll: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
    }
    key = _compute_action_key("poll-1", poll, "pub_A")
    assert key.startswith("pub_A:")
    assert "rest_B" in key


def test_compute_action_key_with_restaurant_and_time():
    """Both restaurant ID and time are encoded in the key."""
    poll: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
        "restaurant_time": "7pm",
    }
    key = _compute_action_key("poll-1", poll, "pub_A")
    assert "rest_B" in key
    assert "7pm" in key


def test_restaurant_added_triggers_reschedule_email():
    """Adding a restaurant to a poll that was already emailed fires a new rescheduled notification."""
    am = ActionMan()
    calls: list[dict] = []

    def my_callback(*, previously_actioned: bool, dummy_run: bool, **kwargs):
        calls.append({"previously_actioned": previously_actioned})

    am.bind(ActionType.EMAIL, my_callback)

    # Initial state: pub_A, no restaurant
    poll_no_restaurant: PollDocument = {"selected": "pub_A", "date": "2026-04-01"}
    initial_key = _compute_action_key("poll-1", poll_no_restaurant, "pub_A")
    action_dict, actioned = am.run(action_dict={}, action_key=initial_key)
    assert actioned
    assert calls[-1]["previously_actioned"] is False

    # Restaurant added — composite key must differ from the initial one
    poll_with_restaurant: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
    }
    new_key = _compute_action_key("poll-1", poll_with_restaurant, "pub_A")
    assert new_key != initial_key, "Keys must differ when restaurant is added"

    action_dict, actioned = am.run(action_dict=action_dict, action_key=new_key)
    assert actioned, "Adding a restaurant should trigger a new email"
    assert calls[-1]["previously_actioned"] is True, "Should be flagged as a reschedule"


def test_restaurant_time_change_triggers_reschedule_email():
    """Changing restaurant_time on an already-emailed poll fires a rescheduled notification."""
    am = ActionMan()
    calls: list[dict] = []

    def my_callback(*, previously_actioned: bool, dummy_run: bool, **kwargs):
        calls.append({"previously_actioned": previously_actioned})

    am.bind(ActionType.EMAIL, my_callback)

    poll_no_time: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
    }
    initial_key = _compute_action_key("poll-1", poll_no_time, "pub_A")
    action_dict, actioned = am.run(action_dict={}, action_key=initial_key)
    assert actioned
    assert calls[-1]["previously_actioned"] is False

    poll_with_time: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
        "restaurant_time": "7pm",
    }
    new_key = _compute_action_key("poll-1", poll_with_time, "pub_A")
    assert new_key != initial_key, "Keys must differ when restaurant_time is added"

    action_dict, actioned = am.run(action_dict=action_dict, action_key=new_key)
    assert actioned, "Adding a restaurant time should trigger a new email"
    assert calls[-1]["previously_actioned"] is True, "Should be flagged as a reschedule"


def test_unchanged_restaurant_does_not_retrigger():
    """If the restaurant and time have not changed, no second email is sent."""
    am = ActionMan()
    run_count = 0

    def my_callback(*, previously_actioned: bool, dummy_run: bool, **kwargs):
        nonlocal run_count
        run_count += 1

    am.bind(ActionType.EMAIL, my_callback)

    poll_dict: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
        "restaurant_time": "7pm",
    }
    key = _compute_action_key("poll-1", poll_dict, "pub_A")
    action_dict, _ = am.run(action_dict={}, action_key=key)
    am.run(action_dict=action_dict, action_key=key)
    assert run_count == 1
