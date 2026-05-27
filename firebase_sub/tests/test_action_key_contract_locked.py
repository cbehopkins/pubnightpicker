"""Locked contract tests for completion action keys.

Do not change expected values in this file without an explicit migration plan,
backfill script, and release note.
"""

from firebase_sub.push_contract import PushDedupeKeys


def test_complete_action_key_contract_pub_only():
    assert PushDedupeKeys.complete_key("pub_A", None, None) == "pub_A"


def test_complete_action_key_contract_pub_restaurant():
    assert PushDedupeKeys.complete_key("pub_A", "rest_B", None) == "pub_A:rest_B:"


def test_complete_action_key_contract_pub_restaurant_time():
    assert (
        PushDedupeKeys.complete_key("pub_A", "rest_B", "19:00") == "pub_A:rest_B:19:00"
    )
