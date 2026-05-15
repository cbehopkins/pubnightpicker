from types import SimpleNamespace

import pytest
from click.testing import CliRunner

from firebase_sub.cli import backup, restore


def _delete_doc_tree(doc_ref) -> None:
    for subcollection in doc_ref.collections():
        for subdoc in subcollection.stream():
            _delete_doc_tree(subdoc.reference)
    doc_ref.delete()


def _clear_firestore(client) -> None:
    for collection in client.collections():
        for doc in collection.stream():
            _delete_doc_tree(doc.reference)


@pytest.mark.integration
def test_backup_restore_roundtrip_with_user_data(
    firestore_client, tmp_path, monkeypatch
):
    firestore_client.document("users/u1").set(
        {"name": "User One", "webPushEnabled": True}
    )
    firestore_client.document("user-public/u1").set({"name": "User One"})
    firestore_client.document("users/u1/push_endpoints/e1").set(
        {"active": True, "endpoint": "https://example.invalid/e1"}
    )
    firestore_client.document("roles/canChat").set({"u1": True})
    firestore_client.document("polls/p1").set(
        {"date": "2026-06-01", "completed": False}
    )

    outfile = tmp_path / "backup.json"
    manifest = tmp_path / "backup.manifest.json"

    monkeypatch.setattr(backup, "_DB_HANDLER", SimpleNamespace(db=firestore_client))
    runner = CliRunner()
    backup_result = runner.invoke(
        backup.main,
        ["--outfile", str(outfile), "--manifest-out", str(manifest)],
    )
    assert backup_result.exit_code == 0, backup_result.output
    assert outfile.exists()
    assert manifest.exists()

    _clear_firestore(firestore_client)

    monkeypatch.setattr(restore, "_DB", firestore_client)
    restore_result = runner.invoke(
        restore.main,
        [
            "--infile",
            str(outfile),
            "--manifest",
            str(manifest),
            "--confirm-non-dry-run",
        ],
    )
    assert restore_result.exit_code == 0, restore_result.output

    assert firestore_client.document("users/u1").get().exists
    assert firestore_client.document("user-public/u1").get().exists
    assert firestore_client.document("users/u1/push_endpoints/e1").get().exists
    role_doc = firestore_client.document("roles/canChat").get().to_dict() or {}
    assert role_doc.get("u1") is True
    assert firestore_client.document("polls/p1").get().exists
