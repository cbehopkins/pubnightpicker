from firebase_sub.push_contract import PushDedupeKeys


def test_open_key_uses_open_prefix():
    assert PushDedupeKeys.open_key("poll-1") == "open:poll-1"


def test_complete_key_pub_only_uses_legacy_compact_form():
    assert PushDedupeKeys.complete_key("pub-1", None, None) == "pub-1"


def test_complete_key_with_restaurant_and_time_is_composite():
    assert (
        PushDedupeKeys.complete_key("pub-1", "rest-1", "19:00")
        == "pub-1:rest-1:19:00"
    )
