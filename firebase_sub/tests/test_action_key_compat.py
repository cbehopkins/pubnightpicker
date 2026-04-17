from firebase_sub.database.handlers import (
    _compute_action_key,
    _with_legacy_alias_key,
)


def test_open_legacy_key_is_aliased_to_canonical_key():
    action_dict = {"email": ["poll-1"]}

    normalized = _with_legacy_alias_key(
        action_dict,
        legacy_key="poll-1",
        canonical_key="open:poll-1",
    )

    assert normalized is not None
    assert "poll-1" in normalized["email"]
    assert "open:poll-1" in normalized["email"]


def test_complete_legacy_key_is_aliased_to_canonical_key():
    poll_dict = {
        "selected": "pub-1",
        "date": "2026-04-16",
        "restaurant": "rest-1",
        "restaurant_time": "19:00",
    }
    legacy_key = "pub-1:rest-1:19:00"
    canonical_key = _compute_action_key("poll-1", poll_dict, "pub-1")

    normalized = _with_legacy_alias_key(
        {"email": [legacy_key]},
        legacy_key=legacy_key,
        canonical_key=canonical_key,
    )

    assert normalized is not None
    assert legacy_key in normalized["email"]
    assert canonical_key in normalized["email"]


def test_complete_accidental_poll_prefixed_key_is_aliased_to_canonical_key():
    poll_dict = {
        "selected": "pub-1",
        "date": "2026-04-16",
        "restaurant": "rest-1",
        "restaurant_time": "19:00",
    }
    accidental_key = "complete:poll-1:pub-1:rest-1:19:00"
    canonical_key = _compute_action_key("poll-1", poll_dict, "pub-1")

    normalized = _with_legacy_alias_key(
        {"email": [accidental_key]},
        legacy_key=accidental_key,
        canonical_key=canonical_key,
    )

    assert normalized is not None
    assert accidental_key in normalized["email"]
    assert canonical_key in normalized["email"]
