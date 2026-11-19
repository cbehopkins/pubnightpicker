import os
import socket
from uuid import uuid4

import firebase_admin
import google.oauth2.credentials
import pytest
from click.testing import CliRunner
from firebase_admin import firestore

from firebase_sub.cli import migrate_meets


class FakeDocumentReference:
    def __init__(self, collection, document_id):
        self.collection = collection
        self.id = document_id
        self.path = f"{collection.name}/{document_id}"

    def set(self, values):
        self.collection.documents[self.id] = dict(values)

    def update(self, values):
        if self.id == self.collection.fail_update_id:
            raise RuntimeError("simulated write failure")
        self.collection.documents[self.id].update(values)


class FakeDocumentSnapshot:
    def __init__(self, reference, data):
        self.reference = reference
        self._data = data

    def to_dict(self):
        return dict(self._data)


class FakeCollection:
    def __init__(self, name, documents=None, fail_update_id=None):
        self.name = name
        self.documents = documents or {}
        self.fail_update_id = fail_update_id

    def document(self, document_id=None):
        if document_id is None:
            document_id = "generated-group-id"
        return FakeDocumentReference(self, document_id)

    def stream(self):
        return [
            FakeDocumentSnapshot(FakeDocumentReference(self, document_id), data)
            for document_id, data in sorted(self.documents.items())
        ]


class FakeDb:
    def __init__(self, groups=None, polls=None, fail_update_id=None):
        self.collections = {
            "groups": FakeCollection("groups", groups),
            "polls": FakeCollection("polls", polls, fail_update_id),
        }

    def collection(self, name):
        return self.collections[name]

@pytest.fixture
def firestore_client():
    host = os.getenv("FIRESTORE_EMULATOR_HOST")
    if not host:
        pytest.skip("Set FIRESTORE_EMULATOR_HOST to run emulator tests")

    hostname, _, port = host.rpartition(":")
    try:
        with socket.create_connection((hostname or "127.0.0.1", int(port)), timeout=1):
            pass
    except OSError:
        pytest.skip(f"Firestore emulator at {host} is not reachable")

    project_id = f"demo-group-migration-{uuid4().hex}"
    app = firebase_admin.initialize_app(
        credential=google.oauth2.credentials.Credentials(token="owner"),
        options={"projectId": project_id},
        name=f"group-migration-tests-{uuid4().hex}",
    )
    yield firestore.client(app=app)
    firebase_admin.delete_app(app)


def test_command_creates_public_group_and_preserves_meet_fields(monkeypatch):
    database = FakeDb(
        polls={"meet-1": {"date": "2026-11-20", "completed": True, "extra": 7}}
    )
    monkeypatch.setattr(migrate_meets, "_get_db", lambda: database)

    result = CliRunner().invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert result.exit_code == 0, result.output
    assert database.collections["groups"].documents == {
        "generated-group-id": {"name": "ampubnight", "visibility": "public"}
    }
    assert database.collections["polls"].documents == {
        "meet-1": {
            "date": "2026-11-20",
            "completed": True,
            "extra": 7,
            "groupId": "generated-group-id",
        }
    }
    assert "Migration complete: 1 Meet(s) migrated" in result.output


def test_command_refuses_existing_group_without_writes(monkeypatch):
    database = FakeDb(
        groups={"existing": {"name": "another-name"}},
        polls={"meet-1": {"date": "2026-11-20"}},
    )
    monkeypatch.setattr(migrate_meets, "_get_db", lambda: database)

    result = CliRunner().invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert result.exit_code != 0
    assert "already contains 1 Group(s); no writes made" in result.output
    assert len(database.collections["groups"].documents) == 1
    assert database.collections["polls"].documents == {
        "meet-1": {"date": "2026-11-20"}
    }


def test_command_reports_partial_failure_and_refuses_rerun(monkeypatch):
    database = FakeDb(
        polls={"a-meet": {"date": "2026-11-20"}, "b-meet": {"date": "2026-11-21"}},
        fail_update_id="b-meet",
    )
    monkeypatch.setattr(migrate_meets, "_get_db", lambda: database)
    runner = CliRunner()

    result = runner.invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert result.exit_code != 0
    assert "1 of 2 Meets migrated" in result.output
    assert "failed to update polls/b-meet" in result.output
    assert "simulated write failure" in result.output
    assert database.collections["polls"].documents["a-meet"]["groupId"] == (
        "generated-group-id"
    )
    assert "groupId" not in database.collections["polls"].documents["b-meet"]

    database.collections["polls"].fail_update_id = None
    rerun_result = runner.invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert rerun_result.exit_code != 0
    assert "already contains 1 Group(s); no writes made" in rerun_result.output


@pytest.mark.integration
def test_migration_creates_generated_group_and_backfills_every_meet(
    firestore_client, monkeypatch
):
    meets = {
        "current-open": {
            "date": "2026-11-20",
            "completed": False,
            "selected": None,
            "customField": {"keep": True},
        },
        "past-open": {
            "date": "2026-11-10",
            "completed": False,
            "selected": None,
        },
        "current-completed": {
            "date": "2026-11-17",
            "completed": True,
            "selected": "pub-1",
        },
        "past-completed": {
            "date": "2026-11-01",
            "completed": True,
            "selected": "pub-2",
            "polls": ["legacy-poll"],
        },
    }
    for meet_id, data in meets.items():
        firestore_client.collection("polls").document(meet_id).set(data)

    monkeypatch.setattr(migrate_meets, "_get_db", lambda: firestore_client)
    result = CliRunner().invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert result.exit_code == 0, result.output
    groups = list(firestore_client.collection("groups").stream())
    assert len(groups) == 1
    group_id = groups[0].id
    assert groups[0].to_dict() == {"name": "ampubnight", "visibility": "public"}
    assert f"Group ID: {group_id}" in result.output
    assert "4 Meet(s) migrated" in result.output

    for meet_id, original_data in meets.items():
        migrated_data = (
            firestore_client.collection("polls").document(meet_id).get().to_dict()
        )
        assert migrated_data == {**original_data, "groupId": group_id}


@pytest.mark.integration
def test_migration_refuses_existing_group_without_writes(firestore_client, monkeypatch):
    firestore_client.collection("groups").document("unrelated").set(
        {"name": "not-the-requested-name", "visibility": "private"}
    )
    firestore_client.collection("polls").document("meet-1").set(
        {"date": "2026-11-20", "completed": False}
    )
    monkeypatch.setattr(migrate_meets, "_get_db", lambda: firestore_client)

    result = CliRunner().invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert result.exit_code != 0
    assert "already contains 1 Group(s); no writes made" in result.output
    assert len(list(firestore_client.collection("groups").stream())) == 1
    assert firestore_client.collection("polls").document("meet-1").get().to_dict() == {
        "date": "2026-11-20",
        "completed": False,
    }


@pytest.mark.integration
def test_partial_failure_is_reported_and_rerun_refuses(firestore_client, monkeypatch):
    firestore_client.collection("polls").document("a-meet").set({"date": "2026-11-20"})
    firestore_client.collection("polls").document("b-meet").set({"date": "2026-11-21"})

    class FailingDocument:
        def __init__(self, snapshot):
            self.reference = snapshot.reference

        def update(self, values):
            if self.reference.id == "b-meet":
                raise RuntimeError("simulated write failure")
            self.reference.update(values)

    class FailingPollsCollection:
        def stream(self):
            return (
                FailingDocument(snapshot)
                for snapshot in firestore_client.collection("polls").stream()
            )

    class FailingDb:
        def collection(self, name):
            if name == "polls":
                return FailingPollsCollection()
            return firestore_client.collection(name)

    monkeypatch.setattr(migrate_meets, "_get_db", lambda: FailingDb())
    runner = CliRunner()
    failed_result = runner.invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert failed_result.exit_code != 0
    assert "1 of 2 Meets migrated" in failed_result.output
    assert "failed to update polls/b-meet" in failed_result.output
    assert "simulated write failure" in failed_result.output
    groups = list(firestore_client.collection("groups").stream())
    assert len(groups) == 1
    group_id = groups[0].id
    assert firestore_client.collection("polls").document("a-meet").get().to_dict() == {
        "date": "2026-11-20",
        "groupId": group_id,
    }
    assert firestore_client.collection("polls").document("b-meet").get().to_dict() == {
        "date": "2026-11-21",
    }

    monkeypatch.setattr(migrate_meets, "_get_db", lambda: firestore_client)
    rerun_result = runner.invoke(migrate_meets.main, ["--name", "ampubnight"])

    assert rerun_result.exit_code != 0
    assert "already contains 1 Group(s); no writes made" in rerun_result.output
