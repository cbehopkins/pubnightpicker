import os
import socket
from collections.abc import Generator

import firebase_admin
import google.oauth2.credentials
import pytest
from firebase_admin import firestore
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.document import DocumentReference

TEST_PROJECT_ID = "demo-firebase-sub-integration"


def pytest_configure(config) -> None:
    # Fallback marker registration in case pytest.ini is not discovered
    # (for example, some IDE/test-runner invocation contexts).
    config.addinivalue_line(
        "markers", "integration: marks tests that require Firebase emulator integration"
    )


class _EmulatorCredentials(firebase_admin.credentials.Base):
    """Stub credential for emulator-only (demo project) usage.

    Avoids ADC look-up so tests can be skipped cleanly rather than
    erroring when the emulator isn't running.
    """

    def get_credential(self) -> google.oauth2.credentials.Credentials:
        return google.oauth2.credentials.Credentials(token="owner")


def _delete_doc_tree(doc_ref: DocumentReference) -> None:
    for subcollection in doc_ref.collections():
        for subdoc in subcollection.stream():
            _delete_doc_tree(subdoc.reference)
    doc_ref.delete()


def _clear_firestore(client: Client) -> None:
    for collection in client.collections():
        for doc in collection.stream():
            _delete_doc_tree(doc.reference)


@pytest.fixture(scope="session")
def firestore_emulator_host() -> str:
    host = os.getenv("FIRESTORE_EMULATOR_HOST")
    if not host:
        pytest.skip(
            "FIRESTORE_EMULATOR_HOST is not set. Run integration tests via tox integration env or set emulator host explicitly."
        )

    # Skip gracefully if the emulator process isn't actually listening yet.
    hostname, _, port_str = host.rpartition(":")
    try:
        with socket.create_connection(
            (hostname or "127.0.0.1", int(port_str)), timeout=1.0
        ):
            pass
    except OSError:
        pytest.skip(
            f"Firestore emulator at {host} is not reachable. Start the emulator before running integration tests."
        )

    return host


@pytest.fixture(scope="session")
def firebase_test_app(firestore_emulator_host: str):
    os.environ.setdefault("GOOGLE_CLOUD_PROJECT", TEST_PROJECT_ID)

    created_default_app = False
    try:
        app = firebase_admin.get_app()
    except ValueError:
        app = firebase_admin.initialize_app(
            credential=_EmulatorCredentials(),
            options={"projectId": TEST_PROJECT_ID},
        )
        created_default_app = True

    yield app

    if created_default_app:
        firebase_admin.delete_app(app)


@pytest.fixture(scope="session")
def firestore_client(firebase_test_app) -> Client:
    return firestore.client(app=firebase_test_app)


@pytest.fixture(autouse=True)
def clean_firestore(firestore_client: Client) -> Generator[None, None, None]:
    _clear_firestore(firestore_client)
    yield
    _clear_firestore(firestore_client)
