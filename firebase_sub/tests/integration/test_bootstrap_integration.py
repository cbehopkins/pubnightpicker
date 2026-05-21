"""Emulator-backed integration tests for firebase_sub.cli.bootstrap.

These tests call the public seeding helpers directly (bypassing the Click
CLI) so they can work with the real emulator Firestore client injected via
the shared conftest fixtures.

Run requirements:
  - Firestore emulator must be running on FIRESTORE_EMULATOR_HOST
  - Use: tox -e integration  or  firebase emulators:exec ... pytest -m integration
"""

import pytest

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


@pytest.mark.integration
class TestSeedSmokeIntegration:
    """seed_smoke_data() creates the correct documents in a real emulator Firestore."""

    def test_smoke_admin_private_doc(self, firestore_client):
        seed_smoke_data(firestore_client)
        doc = (
            firestore_client.collection("users")
            .document(SMOKE_ADMIN_UID)
            .get()
            .to_dict()
        )
        assert doc is not None
        assert doc["uid"] == SMOKE_ADMIN_UID
        assert doc["name"] == "Smoke Admin"
        assert doc["webPushEnabled"] is False
        assert doc["pushPreferences"]["globalChat"] is False

    def test_smoke_admin_public_doc(self, firestore_client):
        seed_smoke_data(firestore_client)
        doc = (
            firestore_client.collection("user-public")
            .document(SMOKE_ADMIN_UID)
            .get()
            .to_dict()
        )
        assert doc is not None
        assert doc["uid"] == SMOKE_ADMIN_UID

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
