from collections.abc import Iterator
from typing import Any

import pytest
from click.testing import CliRunner

from firebase_sub.cli import audit_user_profiles as module


class _FakeSnapshot:
    def __init__(self, document_id: str, data: dict[str, Any]) -> None:
        self.id = document_id
        self._data = data

    def to_dict(self) -> dict[str, Any]:
        return self._data


class _FakeCollection:
    def __init__(
        self, name: str, documents: list[_FakeSnapshot], writes: list[Any]
    ) -> None:
        self._name = name
        self._documents = documents
        self._writes = writes

    def stream(self) -> Iterator[_FakeSnapshot]:
        return iter(self._documents)

    def document(self, document_id: str) -> str:
        return f"{self._name}/{document_id}"


class _FakeBatch:
    def __init__(self, writes: list[Any]) -> None:
        self._writes = writes

    def set(self, reference: str, payload: dict[str, Any], *, merge: bool) -> None:
        self._writes.append((reference, payload, merge))

    def update(self, reference: str, payload: dict[str, Any]) -> None:
        self._writes.append((reference, payload))

    def commit(self) -> None:
        self._writes.append("committed")


class _FakeDb:
    def __init__(
        self, users: list[_FakeSnapshot], public_users: list[_FakeSnapshot]
    ) -> None:
        self._collections = {"users": users, "user-public": public_users}
        self.writes: list[Any] = []

    def collection(self, name: str) -> _FakeCollection:
        return _FakeCollection(name, self._collections[name], self.writes)

    def batch(self) -> _FakeBatch:
        return _FakeBatch(self.writes)


def test_audit_reports_missing_fields_differences_and_document_id_issues() -> None:
    report = module._audit_user_profiles(
        [
            _FakeSnapshot(
                "u1",
                {
                    "uid": "wrong-uid",
                    "name": "Old name",
                    "photoUrl": "https://private.example/avatar.png",
                    "votesVisible": False,
                },
            ),
            _FakeSnapshot("u2", {"uid": "u2", "name": "No public profile"}),
        ],
        [
            _FakeSnapshot(
                "u1",
                {"uid": "u1", "name": "Preferred name", "votesVisible": True},
            ),
            _FakeSnapshot("orphan", {"uid": "orphan"}),
        ],
    )

    counts = report["counts"]
    assert counts["users_scanned"] == 2
    assert counts["user_public_documents_scanned"] == 2
    assert counts["missing_user_public_documents"] == 1
    assert counts["orphan_user_public_documents"] == 1
    assert counts["users_uid_missing_or_mismatched"] == 1
    assert counts["missing_public_fields"]["photoUrl"] == 1
    assert counts["missing_public_fields"]["name"] == 1
    assert counts["differing_values"]["name"] == 1
    assert counts["differing_values"]["votesVisible"] == 1
    assert set(report["issues"]["u1"]) == {
        "users_uid_missing_or_mismatched",
        "missing_public_field:photoUrl",
        "value_differs:name",
        "value_differs:votesVisible",
    }
    assert report["votes_visible_details"]["u1"] == "users=false, user-public=true"


def test_cli_is_read_only_and_hides_document_ids_without_details(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("sensitive-uid", {"uid": "sensitive-uid", "name": "Name"})],
        [],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(module.main, [])

    assert result.exit_code == 0
    assert "User profile audit (read-only)" in result.output
    assert "missing user-public documents: 1" in result.output
    assert "sensitive-uid" not in result.output


def test_cli_can_include_document_issue_details(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "name": "Name", "votesVisible": False})],
        [],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(module.main, ["--details"])

    assert result.exit_code == 0
    assert (
        "u1: missing_public_field:name, missing_public_field:votesVisible, "
        "missing_user_public_document"
    ) in result.output
    assert "votesVisible: users=false, user-public=missing" in result.output


def test_migration_plan_copies_missing_and_true_wins_conflicts() -> None:
    operations, skipped = module._plan_votes_visible_migration(
        [
            _FakeSnapshot("missing-false", {"votesVisible": False}),
            _FakeSnapshot("missing-true", {"votesVisible": True}),
            _FakeSnapshot("public-false", {"votesVisible": True}),
            _FakeSnapshot("private-false", {"votesVisible": False}),
        ],
        [
            _FakeSnapshot(
                "public-false", {"uid": "public-false", "votesVisible": False}
            ),
            _FakeSnapshot(
                "private-false", {"uid": "private-false", "votesVisible": True}
            ),
        ],
    )

    assert [(item["uid"], item["value"], item["write"]) for item in operations] == [
        ("missing-false", False, True),
        ("missing-true", True, True),
        ("public-false", True, True),
        ("private-false", True, False),
    ]
    assert skipped == []


def test_migration_is_dry_run_unless_apply_is_explicit(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "votesVisible": False})],
        [_FakeSnapshot("u1", {"uid": "u1"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(module.main, ["--migrate-votes-visible"])

    assert result.exit_code == 0
    assert "writes planned: 1" in result.output
    assert "writes applied" not in result.output
    assert db.writes == []


def test_migration_apply_aborts_on_unexpected_public_value_type(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "votesVisible": False})],
        [_FakeSnapshot("u1", {"uid": "u1", "votesVisible": "false"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--migrate-votes-visible", "--apply"],
    )

    assert result.exit_code != 0
    assert "no writes were applied" in result.output
    assert db.writes == []


def test_migration_apply_batches_public_field_updates(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "votesVisible": False})],
        [],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--migrate-votes-visible", "--apply"],
    )

    assert result.exit_code == 0
    assert "writes applied: 1" in result.output
    assert db.writes == [
        ("user-public/u1", {"votesVisible": False}, True),
        "committed",
    ]


def test_custom_photo_migration_copies_missing_urls_and_preserves_public_values() -> (
    None
):
    operations, skipped = module._plan_custom_photo_migration(
        [
            _FakeSnapshot(
                "missing-url", {"customPhotoUrl": "https://custom.example/a.png"}
            ),
            _FakeSnapshot(
                "empty-public", {"customPhotoUrl": "https://custom.example/b.png"}
            ),
            _FakeSnapshot("empty-private", {"customPhotoUrl": ""}),
            _FakeSnapshot(
                "keep-public", {"customPhotoUrl": "https://legacy.example/a.png"}
            ),
            _FakeSnapshot("invalid-private", {"customPhotoUrl": 12}),
            _FakeSnapshot(
                "invalid-public", {"customPhotoUrl": "https://private.example/a.png"}
            ),
        ],
        [
            _FakeSnapshot(
                "empty-public", {"uid": "empty-public", "customPhotoUrl": ""}
            ),
            _FakeSnapshot(
                "empty-private", {"uid": "empty-private", "customPhotoUrl": ""}
            ),
            _FakeSnapshot(
                "keep-public",
                {
                    "uid": "keep-public",
                    "customPhotoUrl": "https://public.example/a.png",
                },
            ),
            _FakeSnapshot(
                "invalid-public", {"uid": "invalid-public", "customPhotoUrl": True}
            ),
        ],
    )

    assert [(item["uid"], item["value"], item["write"]) for item in operations] == [
        ("missing-url", "https://custom.example/a.png", True),
        ("empty-public", "https://custom.example/b.png", True),
        ("keep-public", "https://public.example/a.png", False),
    ]
    assert skipped == [
        {"uid": "invalid-private", "reason": "private customPhotoUrl is not a string"},
        {
            "uid": "invalid-public",
            "reason": "public customPhotoUrl is not a string: bool",
        },
    ]


def test_custom_photo_migration_batches_public_field_backfills(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"customPhotoUrl": "https://custom.example/a.png"})],
        [_FakeSnapshot("u1", {"uid": "u1"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--migrate-custom-photo", "--apply"],
    )

    assert result.exit_code == 0
    assert "writes applied: 1" in result.output
    assert db.writes == [
        ("user-public/u1", {"customPhotoUrl": "https://custom.example/a.png"}, True),
        "committed",
    ]


def test_public_profile_migration_fills_missing_fields_and_preserves_public_values() -> (
    None
):
    operations, skipped = module._plan_public_profile_migration(
        [
            _FakeSnapshot(
                "u1",
                {
                    "uid": "u1",
                    "name": "Private name",
                    "photoUrl": "https://private.example/avatar.png",
                    "votesVisible": False,
                    "customPhotoUrl": "https://private.example/custom.png",
                },
            ),
            _FakeSnapshot(
                "u2",
                {
                    "uid": "u2",
                    "name": "Old private name",
                    "photoUrl": "https://private.example/old.png",
                    "votesVisible": False,
                    "customPhotoUrl": "https://private.example/old-custom.png",
                },
            ),
            _FakeSnapshot("u3", {"name": "New public profile"}),
            _FakeSnapshot("u4", {"customPhotoUrl": True}),
            _FakeSnapshot("u5", {"customPhotoUrl": False}),
            _FakeSnapshot("u6", {"customPhotoUrl": True}),
        ],
        [
            _FakeSnapshot(
                "u1",
                {
                    "uid": "u1",
                    "name": "",
                    "photoUrl": None,
                    "votesVisible": None,
                    "customPhotoUrl": "",
                },
            ),
            _FakeSnapshot(
                "u2",
                {
                    "uid": "u2",
                    "name": "Public name",
                    "photoUrl": "https://public.example/avatar.png",
                    "votesVisible": True,
                    "customPhotoUrl": "https://public.example/custom.png",
                },
            ),
            _FakeSnapshot(
                "u4",
                {
                    "uid": "u4",
                    "photoUrl": "https://public.example/legacy-custom.png",
                    "customPhotoUrl": False,
                },
            ),
            _FakeSnapshot("u5", {"uid": "u5"}),
            _FakeSnapshot("u6", {"uid": "u6"}),
        ],
    )

    assert operations == [
        {
            "uid": "u1",
            "data": {
                "name": "Private name",
                "photoUrl": "https://private.example/avatar.png",
                "votesVisible": False,
                "customPhotoUrl": "https://private.example/custom.png",
            },
        },
        {
            "uid": "u3",
            "data": {"name": "New public profile"},
        },
        {
            "uid": "u4",
            "data": {"customPhotoUrl": "https://public.example/legacy-custom.png"},
        },
        {"uid": "u5", "data": {"customPhotoUrl": ""}},
    ]
    assert skipped == [
        {
            "uid": "u6",
            "reason": "legacy customPhotoUrl is true but user-public.photoUrl has no usable URL",
        }
    ]


def test_public_profile_migration_apply_refuses_true_custom_flag_without_url(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"customPhotoUrl": True})],
        [_FakeSnapshot("u1", {"uid": "u1"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--migrate-public-profile", "--apply", "--details"],
    )

    assert result.exit_code != 0
    assert (
        "u1: legacy customPhotoUrl is true but user-public.photoUrl has no usable URL"
        in result.output
    )
    assert db.writes == []


def test_public_profile_migration_is_dry_run_without_apply(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"name": "Private name"})],
        [_FakeSnapshot("u1", {"uid": "u1"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(module.main, ["--migrate-public-profile", "--details"])

    assert result.exit_code == 0
    assert "documents to update: 1" in result.output
    assert "u1: set name" in result.output
    assert db.writes == []


def test_public_profile_migration_batches_updates(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "name": "Private name"})],
        [_FakeSnapshot("u1", {"uid": "u1", "name": ""})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--migrate-public-profile", "--apply"],
    )

    assert result.exit_code == 0
    assert "documents updated: 1" in result.output
    assert db.writes == [
        ("user-public/u1", {"name": "Private name"}, True),
        "committed",
    ]


def test_legacy_cleanup_plan_only_selects_profile_fields() -> None:
    operations = module._plan_legacy_profile_cleanup(
        [
            _FakeSnapshot(
                "u1",
                {
                    "uid": "u1",
                    "email": "u1@example.com",
                    "name": "Legacy name",
                    "photoUrl": "https://example.com/avatar.png",
                    "votesVisible": False,
                    "customPhotoUrl": "https://example.com/avatar.png",
                },
            ),
            _FakeSnapshot("u2", {"uid": "u2", "email": "u2@example.com"}),
        ]
    )

    assert operations == [
        {
            "uid": "u1",
            "fields": ["uid", "name", "photoUrl", "votesVisible", "customPhotoUrl"],
        },
        {"uid": "u2", "fields": ["uid"]},
    ]


def test_legacy_cleanup_is_dry_run_unless_apply_is_explicit(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "name": "Legacy name"})],
        [_FakeSnapshot("u1", {"uid": "u1", "name": "Public name"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(module.main, ["--delete-legacy-profile-fields"])

    assert result.exit_code == 0
    assert "fields to delete: 1" in result.output
    assert "fields deleted" not in result.output
    assert db.writes == []


def test_legacy_cleanup_apply_refuses_missing_public_values(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [_FakeSnapshot("u1", {"uid": "u1", "votesVisible": False})],
        [_FakeSnapshot("u1", {"uid": "u1"})],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--delete-legacy-profile-fields", "--apply"],
    )

    assert result.exit_code != 0
    assert "no fields were deleted" in result.output
    assert "u1: votesVisible" in result.output
    assert db.writes == []


def test_false_legacy_custom_photo_flag_is_not_a_cleanup_blocker() -> None:
    users = [_FakeSnapshot("u1", {"customPhotoUrl": False})]
    public_users = [_FakeSnapshot("u1", {"uid": "u1", "customPhotoUrl": ""})]
    report = module._audit_user_profiles(users, public_users)

    assert module._legacy_cleanup_blockers(report, users, public_users) == {}


def test_true_legacy_custom_photo_blocker_reports_recoverable_url() -> None:
    users = [_FakeSnapshot("u1", {"customPhotoUrl": True})]
    public_users = [
        _FakeSnapshot(
            "u1",
            {"uid": "u1", "photoUrl": "https://example.com/custom.png"},
        )
    ]
    report = module._audit_user_profiles(users, public_users)

    blockers = module._legacy_cleanup_blockers(report, users, public_users)
    assert blockers == {
        "u1": [
            "customPhotoUrl (legacy true; source URL available in "
            "user-public.photoUrl; run --migrate-public-profile)"
        ]
    }


def test_true_legacy_custom_photo_blocker_reports_missing_url_source() -> None:
    users = [_FakeSnapshot("u1", {"customPhotoUrl": True})]
    public_users = [_FakeSnapshot("u1", {"uid": "u1"})]
    report = module._audit_user_profiles(users, public_users)

    blockers = module._legacy_cleanup_blockers(report, users, public_users)
    assert blockers == {
        "u1": ["customPhotoUrl (legacy true; no usable URL in user-public.photoUrl)"]
    }


def test_legacy_cleanup_apply_batches_field_deletions(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [
            _FakeSnapshot(
                "u1",
                {
                    "uid": "u1",
                    "name": "Legacy name",
                    "photoUrl": None,
                    "votesVisible": True,
                    "customPhotoUrl": "",
                },
            )
        ],
        [
            _FakeSnapshot(
                "u1",
                {
                    "uid": "u1",
                    "name": "Public name",
                    "photoUrl": None,
                    "votesVisible": True,
                    "customPhotoUrl": "",
                },
            )
        ],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--delete-legacy-profile-fields", "--apply"],
    )

    assert result.exit_code == 0
    assert "fields deleted: 5" in result.output
    assert db.writes == [
        (
            "users/u1",
            {
                "uid": module.firestore.DELETE_FIELD,
                "name": module.firestore.DELETE_FIELD,
                "photoUrl": module.firestore.DELETE_FIELD,
                "votesVisible": module.firestore.DELETE_FIELD,
                "customPhotoUrl": module.firestore.DELETE_FIELD,
            },
        ),
        "committed",
    ]


def test_public_uid_cleanup_is_dry_run_by_default(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [],
        [
            _FakeSnapshot("u1", {"uid": "stale-u1", "name": "One"}),
            _FakeSnapshot("u2", {"name": "Two"}),
        ],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(module.main, ["--delete-public-uid", "--details"])

    assert result.exit_code == 0
    assert "documents with uid field: 1" in result.output
    assert "u1: delete uid" in result.output
    assert db.writes == []


def test_public_uid_cleanup_batches_delete_field_operations(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    db = _FakeDb(
        [],
        [
            _FakeSnapshot("u1", {"uid": "u1", "name": "One"}),
            _FakeSnapshot("u2", {"uid": "wrong-u2", "name": "Two"}),
        ],
    )
    monkeypatch.setattr(module, "_get_db", lambda: db)

    result = CliRunner().invoke(
        module.main,
        ["--delete-public-uid", "--apply"],
    )

    assert result.exit_code == 0
    assert "uid fields deleted: 2" in result.output
    assert db.writes == [
        ("user-public/u1", {"uid": module.firestore.DELETE_FIELD}),
        ("user-public/u2", {"uid": module.firestore.DELETE_FIELD}),
        "committed",
    ]
