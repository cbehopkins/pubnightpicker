from pathlib import Path

from click.testing import CliRunner


class _FakeSnapshot:
    def __init__(self, data):
        self._data = None if data is None else dict(data)

    def to_dict(self):
        return None if self._data is None else dict(self._data)


class _FakeDocRef:
    """In-memory document reference. Supports nested sub-collections via compound keys."""

    def __init__(self, backing_store: dict[tuple, dict], path: tuple[str, ...]):
        self._store = backing_store
        self._path = (
            path  # e.g. ("users", "uid") or ("users", "uid", "push_endpoints", "ep-id")
        )

    def get(self):
        return _FakeSnapshot(self._store.get(self._path))

    def set(self, payload: dict, merge: bool = False):
        existing = dict(self._store.get(self._path, {}))
        if merge:
            existing.update(payload)
            self._store[self._path] = existing
        else:
            self._store[self._path] = dict(payload)

    def collection(self, sub_name: str):
        return _FakeCollection(self._store, (*self._path, sub_name))


class _FakeCollection:
    def __init__(self, backing_store: dict[tuple, dict], path: tuple[str, ...]):
        self._store = backing_store
        self._path = path  # e.g. ("users",) or ("users", "uid", "push_endpoints")

    def document(self, doc_id: str):
        return _FakeDocRef(self._store, (*self._path, doc_id))


class _FakeDb:
    def __init__(self):
        self.store: dict[tuple, dict] = {}

    def collection(self, name: str):
        return _FakeCollection(self.store, (name,))


def test_resolve_emulator_project_id_uses_cred_project_id(monkeypatch, tmp_path):
    import firebase_sub.cli.bootstrap as module

    monkeypatch.delenv("GOOGLE_CLOUD_PROJECT", raising=False)
    monkeypatch.delenv("GCLOUD_PROJECT", raising=False)
    monkeypatch.delenv("FIREBASE_PROJECT", raising=False)
    monkeypatch.delenv("REACT_APP_FIREBASE_PROJECT_ID", raising=False)

    cred_path = tmp_path / "cred.json"
    cred_path.write_text('{"project_id": "from-cred"}', encoding="utf-8")
    monkeypatch.setattr(module, "CRED_PATH", Path(cred_path))

    assert module._resolve_emulator_project_id() == "from-cred"


def test_resolve_emulator_project_id_uses_default_when_cred_missing(
    monkeypatch, tmp_path
):
    import firebase_sub.cli.bootstrap as module

    monkeypatch.delenv("GOOGLE_CLOUD_PROJECT", raising=False)
    monkeypatch.delenv("GCLOUD_PROJECT", raising=False)
    monkeypatch.delenv("FIREBASE_PROJECT", raising=False)
    monkeypatch.delenv("REACT_APP_FIREBASE_PROJECT_ID", raising=False)
    monkeypatch.setattr(module, "CRED_PATH", tmp_path / "missing-cred.json")

    assert module._resolve_emulator_project_id() == "demo-firebase-sub-integration"


def test_resolve_emulator_project_id_prefers_google_cloud_project(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    monkeypatch.setenv("GOOGLE_CLOUD_PROJECT", "from-google-cloud")
    monkeypatch.setenv("GCLOUD_PROJECT", "from-gcloud")
    monkeypatch.setenv("FIREBASE_PROJECT", "from-firebase")
    monkeypatch.setenv("REACT_APP_FIREBASE_PROJECT_ID", "from-react")

    assert module._resolve_emulator_project_id() == "from-google-cloud"


def test_create_admin_seeds_user_docs_and_roles(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    fake_db = _FakeDb()
    monkeypatch.setattr(module, "_get_db", lambda: fake_db)

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        [
            "create-admin",
            "--uid",
            "admin-user",
            "--name",
            "Admin Name",
            "--email",
            "admin@example.com",
        ],
    )

    assert result.exit_code == 0

    private_doc = fake_db.store[("users", "admin-user")]
    public_doc = fake_db.store[("user-public", "admin-user")]

    assert private_doc["uid"] == "admin-user"
    assert private_doc["name"] == "Admin Name"
    assert private_doc["notificationEmail"] == "admin@example.com"
    assert private_doc["webPushEnabled"] is False
    assert private_doc["pushPreferences"]["globalChat"] is False

    assert public_doc["uid"] == "admin-user"
    assert public_doc["name"] == "Admin Name"

    assert fake_db.store[("roles", "admin")]["admin-user"] is True
    assert fake_db.store[("roles", "canChat")]["admin-user"] is True
    assert fake_db.store[("roles", "canCompletePoll")]["admin-user"] is True


def test_create_admin_keeps_existing_values(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    fake_db = _FakeDb()
    fake_db.store[("users", "u1")] = {
        "uid": "u1",
        "name": "Existing Name",
        "notificationEmail": "old@example.com",
        "notificationEmailEnabled": True,
    }
    fake_db.store[("user-public", "u1")] = {
        "uid": "u1",
        "name": "Existing Public",
    }

    monkeypatch.setattr(module, "_get_db", lambda: fake_db)

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        [
            "create-admin",
            "--uid",
            "u1",
            "--name",
            "New Name",
            "--email",
            "new@example.com",
        ],
    )

    assert result.exit_code == 0
    assert fake_db.store[("users", "u1")]["name"] == "Existing Name"
    assert fake_db.store[("users", "u1")]["notificationEmail"] == "old@example.com"
    assert fake_db.store[("user-public", "u1")]["name"] == "Existing Public"


def test_create_admin_generates_uid_when_missing(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    fake_db = _FakeDb()
    monkeypatch.setattr(module, "_get_db", lambda: fake_db)

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        [
            "create-admin",
            "--name",
            "Generated Admin",
            "--email",
            "generated@example.com",
        ],
    )

    assert result.exit_code == 0
    assert "generated uid: admin-" in result.output

    user_doc_keys = [
        key for key in fake_db.store if key[0] == "users" and len(key) == 2
    ]
    assert len(user_doc_keys) == 1
    generated_uid = user_doc_keys[0][1]

    assert generated_uid.startswith("admin-")
    assert fake_db.store[("users", generated_uid)]["uid"] == generated_uid
    assert fake_db.store[("user-public", generated_uid)]["uid"] == generated_uid
    assert fake_db.store[("roles", "admin")][generated_uid] is True


def test_create_admin_password_requires_auth_emulator(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    fake_db = _FakeDb()
    monkeypatch.setattr(module, "_get_db", lambda: fake_db)
    monkeypatch.delenv("FIREBASE_AUTH_EMULATOR_HOST", raising=False)

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        [
            "create-admin",
            "--uid",
            "u-auth",
            "--name",
            "Admin Name",
            "--email",
            "admin@example.com",
            "--password",
            "password123",
        ],
    )

    assert result.exit_code != 0
    assert "FIREBASE_AUTH_EMULATOR_HOST" in result.output


def test_create_admin_password_creates_auth_user_in_emulator(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    fake_db = _FakeDb()
    monkeypatch.setattr(module, "_get_db", lambda: fake_db)
    monkeypatch.setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")

    created_payload = {}

    def _fake_create_user(**kwargs):
        created_payload.update(kwargs)
        return object()

    monkeypatch.setattr(module.firebase_auth, "create_user", _fake_create_user)

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        [
            "create-admin",
            "--uid",
            "u-auth",
            "--name",
            "Admin Name",
            "--email",
            "admin@example.com",
            "--password",
            "password123",
        ],
    )

    assert result.exit_code == 0
    assert "auth/u-auth: created email/password user" in result.output
    assert created_payload["uid"] == "u-auth"
    assert created_payload["email"] == "admin@example.com"
    assert created_payload["password"] == "password123"
    assert created_payload["display_name"] == "Admin Name"


def test_grant_role_dry_run_does_not_write(monkeypatch):
    import firebase_sub.cli.bootstrap as module

    fake_db = _FakeDb()
    monkeypatch.setattr(module, "_get_db", lambda: fake_db)

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        ["grant-role", "--uid", "u2", "--role", "canChat", "--dry-run"],
    )

    assert result.exit_code == 0
    assert ("roles", "canChat") not in fake_db.store


def test_seed_smoke_creates_expected_documents(monkeypatch):
    """seed_smoke_data() writes user/public/role/endpoint docs for all three smoke users."""
    import firebase_sub.cli.bootstrap as module

    monkeypatch.delenv("FIREBASE_AUTH_EMULATOR_HOST", raising=False)
    fake_db = _FakeDb()
    result = module.seed_smoke_data(fake_db)

    # All three private user docs
    assert (
        fake_db.store[("users", module.SMOKE_ADMIN_UID)]["uid"]
        == module.SMOKE_ADMIN_UID
    )
    assert (
        fake_db.store[("users", module.SMOKE_USER_A_UID)]["uid"]
        == module.SMOKE_USER_A_UID
    )
    assert (
        fake_db.store[("users", module.SMOKE_USER_B_UID)]["uid"]
        == module.SMOKE_USER_B_UID
    )

    # User B has push enabled and global chat opted in
    user_b = fake_db.store[("users", module.SMOKE_USER_B_UID)]
    assert user_b["webPushEnabled"] is True
    assert user_b["pushPreferences"]["globalChat"] is True

    # User B push endpoint exists as a sub-collection document
    ep_key = (
        "users",
        module.SMOKE_USER_B_UID,
        "push_endpoints",
        module.SMOKE_USER_B_ENDPOINT_ID,
    )
    assert ep_key in fake_db.store
    ep = fake_db.store[ep_key]
    assert ep["active"] is True
    assert "endpoint" in ep
    assert "p256dh" in ep
    assert "auth" in ep

    # Admin has the admin role
    assert fake_db.store[("roles", "admin")][module.SMOKE_ADMIN_UID] is True

    # SeedResult accounts for all writes
    assert len(result.wrote_docs) > 0
    assert len(result.skipped_docs) == 0


def test_seed_smoke_is_idempotent(monkeypatch):
    """Calling seed_smoke_data twice does not overwrite any existing docs."""
    import firebase_sub.cli.bootstrap as module

    monkeypatch.delenv("FIREBASE_AUTH_EMULATOR_HOST", raising=False)
    fake_db = _FakeDb()
    # Mutate user B name before first seed so it differs from defaults
    fake_db.store[("users", module.SMOKE_USER_B_UID)] = {
        "uid": module.SMOKE_USER_B_UID,
        "name": "Pre-existing",
    }

    first = module.seed_smoke_data(fake_db)
    second = module.seed_smoke_data(fake_db)

    # Pre-seeded user B doc should survive unchanged
    assert fake_db.store[("users", module.SMOKE_USER_B_UID)]["name"] == "Pre-existing"

    # Second call should have more skips than first
    assert len(second.skipped_docs) > len(first.skipped_docs)


def test_seed_smoke_dry_run_does_not_write(monkeypatch):
    """seed_smoke_data with dry_run=True must not mutate any Firestore document."""
    import firebase_sub.cli.bootstrap as module

    monkeypatch.delenv("FIREBASE_AUTH_EMULATOR_HOST", raising=False)
    fake_db = _FakeDb()
    result = module.seed_smoke_data(fake_db, dry_run=True)

    assert len(fake_db.store) == 0
    assert len(result.wrote_docs) > 0  # reported as "would write" but nothing written
