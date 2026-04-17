"""Locked contract tests for completion action keys.

Do not change expected values in this file without an explicit migration plan,
backfill script, and release note.
"""

from firebase_sub.database.handlers import _compute_action_key
from firebase_sub.my_types import PollDocument


def test_complete_action_key_contract_pub_only():
    poll: PollDocument = {"selected": "pub_A", "date": "2026-04-01"}
    assert _compute_action_key("poll-1", poll, "pub_A") == "pub_A"


def test_complete_action_key_contract_pub_restaurant():
    poll: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
    }
    assert _compute_action_key("poll-1", poll, "pub_A") == "pub_A:rest_B:"


def test_complete_action_key_contract_pub_restaurant_time():
    poll: PollDocument = {
        "selected": "pub_A",
        "date": "2026-04-01",
        "restaurant": "rest_B",
        "restaurant_time": "19:00",
    }
    assert _compute_action_key("poll-1", poll, "pub_A") == "pub_A:rest_B:19:00"
