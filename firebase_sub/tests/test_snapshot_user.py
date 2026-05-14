from types import SimpleNamespace

from firebase_sub.cli.snapshot_user import _document_record, _serialize_auth_user


def test_document_record_includes_path_and_collection_fields():
    record = _document_record("users/u1/push_endpoints/e1", {"active": True})
    assert record["schema_version"] == 2
    assert record["path"] == "users/u1/push_endpoints/e1"
    assert record["collection"] == "push_endpoints"
    assert record["id"] == "e1"
    assert record["data"] == {"active": True}


def test_document_record_rejects_invalid_path():
    try:
        _document_record("users/u1/push_endpoints", {"active": True})
    except ValueError as exc:
        assert "even segments" in str(exc)
    else:
        raise AssertionError("Expected ValueError for invalid record path")


def test_serialize_auth_user_extracts_core_fields():
    provider = SimpleNamespace(
        provider_id="password",
        uid="provider-uid",
        email="u@example.com",
        display_name="U",
        phone_number=None,
    )
    user = SimpleNamespace(
        uid="u1",
        email="u@example.com",
        display_name="User",
        phone_number="+12025550123",
        photo_url="https://example.com/u.png",
        disabled=False,
        email_verified=True,
        custom_claims={"admin": True},
        provider_data=[provider],
        tokens_valid_after_timestamp=123456789,
        user_metadata=SimpleNamespace(
            creation_timestamp=1000,
            last_sign_in_timestamp=2000,
            last_refresh_timestamp=3000,
        ),
    )

    payload = _serialize_auth_user(user)
    assert payload["uid"] == "u1"
    assert payload["email"] == "u@example.com"
    assert payload["custom_claims"] == {"admin": True}
    assert payload["providers"][0]["provider_id"] == "password"
    assert payload["user_metadata"]["creation_timestamp"] == 1000
