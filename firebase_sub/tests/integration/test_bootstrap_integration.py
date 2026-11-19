"""Emulator-backed integration tests for firebase_sub.cli.bootstrap.

These tests call the public seeding helpers directly (bypassing the Click
CLI) so they can work with the real emulator Firestore client injected via
the shared conftest fixtures.

Run requirements:
  - Firestore emulator must be running on FIRESTORE_EMULATOR_HOST
  - Use: tox -e integration  or  firebase emulators:exec ... pytest -m integration
"""

import pytest
from click.testing import CliRunner

from firebase_sub.cli import bootstrap
from firebase_sub.cli.bootstrap import (
    ADMIN_DEFAULT_ROLES,
    SMOKE_ADMIN_UID,
    SMOKE_USER_A_UID,
    SMOKE_USER_B_ENDPOINT_ID,
    SMOKE_USER_B_UID,
    _grant_role,
    _set_doc_if_missing,
    seed_smoke_data,
)


def _run_create_admin(firestore_client, monkeypatch, *arguments: str):
    monkeypatch.setattr(bootstrap, "_get_db", lambda: firestore_client)
    return CliRunner().invoke(
        bootstrap.cli,
        ["create-admin", "--uid", "bootstrap-admin", *arguments],
    )


@pytest.mark.integration
def test_create_admin_creates_default_group_without_changing_meets(
    firestore_client, monkeypatch
):
    meet_data = {"date": "2026-11-20", "completed": False, "selected": None}
    firestore_client.collection("polls").document("meet-1").set(meet_data)

    result = _run_create_admin(firestore_client, monkeypatch)

    groups = list(firestore_client.collection("groups").stream())
    assert result.exit_code == 0, result.output
    assert len(groups) == 1
    group = groups[0]
    assert group.id != "ampubnight"
    assert group.to_dict() == {"name": "ampubnight", "visibility": "public"}
    assert f"groups/{group.id}: created public Group 'ampubnight'" in result.output
    assert (
        firestore_client.collection("polls").document("meet-1").get().to_dict()
        == meet_data
    )


@pytest.mark.integration
def test_create_admin_accepts_custom_name_with_other_groups_present(
    firestore_client, monkeypatch
):
    firestore_client.collection("groups").document("unrelated").set(
        {"name": "unrelated", "visibility": "private"}
    )

    result = _run_create_admin(
        firestore_client, monkeypatch, "--group-name", "Custom Group"
    )

    groups = list(firestore_client.collection("groups").stream())
    groups_by_id = {group.id: group.to_dict() for group in groups}
    assert result.exit_code == 0, result.output
    assert len(groups_by_id) == 2
    assert groups_by_id["unrelated"] == {
        "name": "unrelated",
        "visibility": "private",
    }
    custom_groups = [
        (group_id, data)
        for group_id, data in groups_by_id.items()
        if data.get("name") == "Custom Group"
    ]
    assert len(custom_groups) == 1
    custom_group_id, custom_group = custom_groups[0]
    assert custom_group_id != "Custom Group"
    assert custom_group == {"name": "Custom Group", "visibility": "public"}
    assert (
        f"groups/{custom_group_id}: created public Group 'Custom Group'"
        in result.output
    )


@pytest.mark.integration
def test_create_admin_treats_group_name_case_variants_as_distinct(
    firestore_client, monkeypatch
):
    lower_result = _run_create_admin(firestore_client, monkeypatch)
    upper_result = _run_create_admin(
        firestore_client, monkeypatch, "--group-name", "AmPubNight"
    )

    groups = list(firestore_client.collection("groups").stream())
    names = {group.to_dict()["name"] for group in groups}
    assert lower_result.exit_code == 0, lower_result.output
    assert upper_result.exit_code == 0, upper_result.output
    assert len(groups) == 2
    assert names == {"ampubnight", "AmPubNight"}


@pytest.mark.integration
def test_create_admin_reuses_matching_group_unchanged_and_preserves_meets(
    firestore_client, monkeypatch
):
    original_group = {
        "name": "ampubnight",
        "visibility": "private",
        "customField": {"retain": True},
    }
    firestore_client.collection("groups").document("existing-group").set(original_group)
    firestore_client.collection("groups").document("different-group").set(
        {"name": "another-group", "visibility": "public"}
    )
    meet_data = {"date": "2026-11-20", "groupId": "legacy-value", "custom": 7}
    firestore_client.collection("polls").document("meet-1").set(meet_data)

    first_result = _run_create_admin(firestore_client, monkeypatch)
    second_result = _run_create_admin(firestore_client, monkeypatch)

    assert first_result.exit_code == 0, first_result.output
    assert second_result.exit_code == 0, second_result.output
    assert "groups/existing-group: reused existing Group 'ampubnight' unchanged" in (
        second_result.output
    )
    assert (
        firestore_client.collection("groups").document("existing-group").get().to_dict()
        == original_group
    )
    assert firestore_client.collection("groups").document(
        "different-group"
    ).get().to_dict() == {"name": "another-group", "visibility": "public"}
    assert len(list(firestore_client.collection("groups").stream())) == 2
    assert (
        firestore_client.collection("polls").document("meet-1").get().to_dict()
        == meet_data
    )


@pytest.mark.integration
def test_create_admin_refuses_duplicate_exact_name_groups_without_writes(
    firestore_client, monkeypatch
):
    first_group = {"name": "ampubnight", "visibility": "public"}
    second_group = {"name": "ampubnight", "visibility": "private"}
    firestore_client.collection("groups").document("first-group").set(first_group)
    firestore_client.collection("groups").document("second-group").set(second_group)
    firestore_client.collection("polls").document("meet-1").set({"custom": True})

    result = _run_create_admin(firestore_client, monkeypatch)

    assert result.exit_code != 0
    assert "found 2 Groups named 'ampubnight'" in result.output
    assert len(list(firestore_client.collection("groups").stream())) == 2
    assert (
        firestore_client.collection("groups").document("first-group").get().to_dict()
        == first_group
    )
    assert (
        firestore_client.collection("groups").document("second-group").get().to_dict()
        == second_group
    )
    assert firestore_client.collection("polls").document("meet-1").get().to_dict() == {
        "custom": True
    }
    assert (
        firestore_client.collection("users").document("bootstrap-admin").get().exists
        is False
    )


@pytest.mark.integration
class TestSeedSmokeIntegration:
    """seed_smoke_data() creates the correct documents in a real emulator Firestore."""

    def test_smoke_admin_private_doc(self, firestore_client):
        seed_smoke_data(firestore_client)
        snapshot = firestore_client.collection("users").document(SMOKE_ADMIN_UID).get()
        doc = snapshot.to_dict()
        assert doc is not None
        assert snapshot.id == SMOKE_ADMIN_UID
        assert "uid" not in doc
        assert "name" not in doc
        assert doc["webPushEnabled"] is False
        assert doc["pushPreferences"]["globalChat"] is False

    def test_smoke_admin_public_doc(self, firestore_client):
        seed_smoke_data(firestore_client)
        snapshot = (
            firestore_client.collection("user-public").document(SMOKE_ADMIN_UID).get()
        )
        doc = snapshot.to_dict()
        assert doc is not None
        assert snapshot.id == SMOKE_ADMIN_UID
        assert "uid" not in doc
        assert doc["name"] == "Smoke Admin"

    def test_smoke_admin_roles(self, firestore_client):
        seed_smoke_data(firestore_client)
        roles_ref = firestore_client.collection("roles")

        admin_role = roles_ref.document("admin").get().to_dict() or {}
        assert admin_role.get(SMOKE_ADMIN_UID) is True

        for role in ADMIN_DEFAULT_ROLES:
            role_doc = roles_ref.document(role).get().to_dict() or {}
            assert (
                role_doc.get(SMOKE_ADMIN_UID) is True
            ), f"Expected {SMOKE_ADMIN_UID} in roles/{role}"

    def test_smoke_user_a_docs(self, firestore_client):
        seed_smoke_data(firestore_client)
        priv = (
            firestore_client.collection("users")
            .document(SMOKE_USER_A_UID)
            .get()
            .to_dict()
        )
        pub = (
            firestore_client.collection("user-public")
            .document(SMOKE_USER_A_UID)
            .get()
            .to_dict()
        )
        assert priv is not None
        assert pub is not None
        assert priv["webPushEnabled"] is False
        # User A has canChat
        role_doc = (
            firestore_client.collection("roles").document("canChat").get().to_dict()
            or {}
        )
        assert role_doc.get(SMOKE_USER_A_UID) is True

    def test_smoke_user_b_push_enabled(self, firestore_client):
        seed_smoke_data(firestore_client)
        priv = (
            firestore_client.collection("users")
            .document(SMOKE_USER_B_UID)
            .get()
            .to_dict()
        )
        assert priv is not None
        assert priv["webPushEnabled"] is True
        assert priv["pushPreferences"]["globalChat"] is True

    def test_smoke_user_b_push_endpoint(self, firestore_client):
        seed_smoke_data(firestore_client)
        ep = (
            firestore_client.collection("users")
            .document(SMOKE_USER_B_UID)
            .collection("push_endpoints")
            .document(SMOKE_USER_B_ENDPOINT_ID)
            .get()
            .to_dict()
        )
        assert ep is not None
        assert ep["active"] is True
        assert ep["endpoint"].startswith("https://")
        assert ep["p256dh"]
        assert ep["auth"]

    def test_seed_smoke_is_idempotent(self, firestore_client):
        """Running seed_smoke_data twice must not overwrite or duplicate data."""
        seed_smoke_data(firestore_client)

        # Overwrite a field manually to verify idempotency preserves it
        firestore_client.collection("users").document(SMOKE_USER_A_UID).set(
            {"name": "Custom Name"}, merge=True
        )

        seed_smoke_data(firestore_client)

        doc = (
            firestore_client.collection("users")
            .document(SMOKE_USER_A_UID)
            .get()
            .to_dict()
        )
        # The second seed call must not overwrite the manually set name
        assert doc["name"] == "Custom Name"

    def test_seed_smoke_result_counts(self, firestore_client):
        """First call reports writes; second call reports skips."""
        first = seed_smoke_data(firestore_client)
        second = seed_smoke_data(firestore_client)

        assert len(first.wrote_docs) > 0, "First seed should report written docs"
        assert (
            len(second.skipped_docs) > 0
        ), "Second seed should report skipped docs (already exist)"


@pytest.mark.integration
class TestSetDocIfMissingIntegration:
    def test_writes_when_absent(self, firestore_client):
        _set_doc_if_missing(
            firestore_client, "users", "test-absent", {"x": 1}, dry_run=False
        )
        doc = (
            firestore_client.collection("users").document("test-absent").get().to_dict()
        )
        assert doc == {"x": 1}

    def test_skips_when_present(self, firestore_client):
        firestore_client.collection("users").document("test-present").set({"x": 99})
        written = _set_doc_if_missing(
            firestore_client, "users", "test-present", {"x": 1}, dry_run=False
        )
        assert written is False
        doc = (
            firestore_client.collection("users")
            .document("test-present")
            .get()
            .to_dict()
        )
        assert doc["x"] == 99  # unchanged


@pytest.mark.integration
class TestGrantRoleIntegration:
    def test_grants_new_role(self, firestore_client):
        result = _grant_role(
            firestore_client, role="test-role", uid="test-uid", dry_run=False
        )
        assert result.already_granted is False
        doc = (
            firestore_client.collection("roles").document("test-role").get().to_dict()
            or {}
        )
        assert doc.get("test-uid") is True

    def test_detects_existing_grant(self, firestore_client):
        firestore_client.collection("roles").document("test-role2").set({"uid2": True})
        result = _grant_role(
            firestore_client, role="test-role2", uid="uid2", dry_run=False
        )
        assert result.already_granted is True
