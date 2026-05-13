import json

from firebase_sub.cli.restore import (
    _collection_allowed,
    _record_path,
    _record_uid,
    _root_collection,
    _validate_restore_intent,
    _verify_manifest,
)


def test_record_path_prefers_v2_path_field():
    record = {
        "path": "users/u1/push_endpoints/e1",
        "collection": "ignored",
        "id": "ignored",
        "data": {},
    }
    assert _record_path(record) == "users/u1/push_endpoints/e1"


def test_record_path_supports_legacy_fields():
    record = {
        "collection": "polls",
        "id": "2026-01-01",
        "data": {},
    }
    assert _record_path(record) == "polls/2026-01-01"


def test_record_path_rejects_invalid_segments():
    record = {
        "path": "users/u1/push_endpoints",
        "data": {},
    }
    try:
        _record_path(record)
    except ValueError as exc:
        assert "Invalid document path" in str(exc)
    else:
        raise AssertionError("Expected ValueError for odd segment path")


def test_record_uid_extracts_user_docs_and_subcollections():
    assert _record_uid("users/u1") == "u1"
    assert _record_uid("users/u1/push_endpoints/e1") == "u1"
    assert _record_uid("user-public/u2") == "u2"
    assert _record_uid("roles/canChat") is None
    assert _record_uid("polls/p1") is None


def test_verify_manifest_accepts_matching_checksum(tmp_path):
    data_file = tmp_path / "backup.json"
    data_file.write_text("[]\n", encoding="utf-8")

    from hashlib import sha256

    digest = sha256(data_file.read_bytes()).hexdigest()
    manifest_file = tmp_path / "backup.json.manifest.json"
    manifest_file.write_text(
        json.dumps({"sha256": digest, "data_file": "backup.json"}),
        encoding="utf-8",
    )

    _verify_manifest(data_file, manifest_file)


def test_verify_manifest_rejects_mismatch(tmp_path):
    data_file = tmp_path / "backup.json"
    data_file.write_text("[]\n", encoding="utf-8")

    manifest_file = tmp_path / "backup.json.manifest.json"
    manifest_file.write_text(
        json.dumps({"sha256": "deadbeef", "data_file": "backup.json"}),
        encoding="utf-8",
    )

    try:
        _verify_manifest(data_file, manifest_file)
    except ValueError as exc:
        assert "checksum mismatch" in str(exc)
    else:
        raise AssertionError("Expected ValueError for checksum mismatch")


def test_root_collection_extracts_first_segment():
    assert _root_collection("users/u1") == "users"
    assert _root_collection("users/u1/push_endpoints/e1") == "users"


def test_collection_allowed_respects_allow_and_deny():
    assert _collection_allowed("users/u1", allow_collections=set(), deny_collections=set())
    assert not _collection_allowed(
        "users/u1",
        allow_collections=set(),
        deny_collections={"users"},
    )
    assert _collection_allowed(
        "polls/p1",
        allow_collections={"polls"},
        deny_collections=set(),
    )
    assert not _collection_allowed(
        "users/u1",
        allow_collections={"polls"},
        deny_collections=set(),
    )


def test_validate_restore_intent_requires_confirmation_for_broad_write():
    try:
        _validate_restore_intent(
            dry_run=False,
            uid=None,
            allow_collections=set(),
            confirm_non_dry_run=False,
        )
    except ValueError as exc:
        assert "Refusing broad non-dry-run restore" in str(exc)
    else:
        raise AssertionError("Expected ValueError for unscoped non-dry-run restore")


def test_validate_restore_intent_allows_scoped_or_confirmed_writes():
    _validate_restore_intent(
        dry_run=False,
        uid="u1",
        allow_collections=set(),
        confirm_non_dry_run=False,
    )
    _validate_restore_intent(
        dry_run=False,
        uid=None,
        allow_collections={"polls"},
        confirm_non_dry_run=False,
    )
    _validate_restore_intent(
        dry_run=False,
        uid=None,
        allow_collections=set(),
        confirm_non_dry_run=True,
    )
