from firebase_sub.push_contract import PushDedupeKeys


def test_open_key_uses_poll_scoped_prefix():
    assert PushDedupeKeys.open_key("poll-123") == "open:poll-123"


def test_complete_key_normalizes_missing_restaurant_fields():
    assert (
        PushDedupeKeys.complete_key(
            pub_id="pub-1",
            restaurant_id=None,
            restaurant_time=None,
        )
        == "pub-1"
    )


def test_complete_key_includes_restaurant_and_time():
    assert (
        PushDedupeKeys.complete_key(
            pub_id="pub-7",
            restaurant_id="rest-2",
            restaurant_time="18:30",
        )
        == "pub-7:rest-2:18:30"
    )
