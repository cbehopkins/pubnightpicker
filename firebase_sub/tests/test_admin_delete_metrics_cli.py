from click.testing import CliRunner

from firebase_sub.cli import admin_delete_metrics as module


class _FakeSnapshot:
    def __init__(self, data):
        self._data = data

    def to_dict(self):
        return self._data


class _FakeDocRef:
    def __init__(self, payload):
        self._payload = payload

    def get(self):
        return _FakeSnapshot(self._payload)


class _FakeCollection:
    def __init__(self, docs):
        self._docs = docs

    def document(self, doc_id: str):
        return _FakeDocRef(self._docs.get(doc_id))


class _FakeDb:
    def __init__(self, docs):
        self._docs = docs

    def collection(self, name: str):
        if name not in {"admin_delete_request_metrics", "system_config"}:
            raise AssertionError(f"unexpected collection: {name}")
        if name == "admin_delete_request_metrics":
            return _FakeCollection(self._docs)
        return _FakeCollection(
            {"admin_delete": self._docs.get("system_config_admin_delete")}
        )


def test_cli_prints_global_and_daily_metrics(monkeypatch):
    docs = {
        "global": {
            "total": 7,
            "lastOutcome": "auth_deleted",
            "lastRequestId": "req-7",
            "updatedAt": "ts-global",
            "outcomes": {
                "auth_delete_failed": 1,
                "auth_delete_blocked": 2,
                "auth_deleted": 4,
            },
        },
        "daily-2026-05-12": {
            "total": 3,
            "lastOutcome": "auth_delete_failed",
            "lastRequestId": "req-9",
            "updatedAt": "ts-daily",
            "outcomes": {
                "auth_delete_failed": 1,
                "auth_deleted": 2,
            },
        },
    }
    monkeypatch.setattr(module, "_get_db", lambda: _FakeDb(docs))

    runner = CliRunner()
    result = runner.invoke(module.cli, ["--day", "2026-05-12"])

    assert result.exit_code == 0
    assert "Admin delete metrics (day=2026-05-12)" in result.output
    assert "global" in result.output
    assert "daily-2026-05-12" in result.output
    assert "auth_delete_failed: 1" in result.output
    assert "auth_delete_blocked: 2" in result.output


def test_cli_handles_missing_docs(monkeypatch):
    monkeypatch.setattr(module, "_get_db", lambda: _FakeDb({}))

    runner = CliRunner()
    result = runner.invoke(module.cli, ["--day", "2026-05-12"])

    assert result.exit_code == 0
    assert "global" in result.output
    assert "daily-2026-05-12" in result.output
    assert "(missing)" in result.output


def test_cli_preflight_prints_gate_killswitch_and_counters(monkeypatch):
    docs = {
        "global": {
            "outcomes": {
                "auth_delete_failed": 2,
                "auth_delete_blocked": 3,
            }
        },
        "daily-2026-05-12": {
            "outcomes": {
                "auth_delete_failed": 1,
                "auth_delete_blocked": 0,
            }
        },
        "system_config_admin_delete": {
            "paused": False,
            "reason": "",
        },
    }
    monkeypatch.setenv("ENABLE_ADMIN_DELETE_REQUESTS", "true")
    monkeypatch.setattr(module, "_get_db", lambda: _FakeDb(docs))

    runner = CliRunner()
    result = runner.invoke(
        module.cli,
        [
            "--preflight",
            "--enable-real-auth-delete",
            "--day",
            "2026-05-12",
        ],
    )

    assert result.exit_code == 0
    assert "Admin delete preflight" in result.output
    assert "env gate ENABLE_ADMIN_DELETE_REQUESTS: on" in result.output
    assert "cli gate --enable-real-auth-delete: on" in result.output
    assert "effective real auth delete: on" in result.output
    assert "kill-switch paused: no" in result.output
    assert "global auth_delete_failed/auth_delete_blocked: 2/3" in result.output
    assert (
        "daily-2026-05-12 auth_delete_failed/auth_delete_blocked: 1/0" in result.output
    )


def test_cli_preflight_exits_nonzero_when_effective_real_delete_off(monkeypatch):
    docs = {
        "system_config_admin_delete": {
            "paused": False,
        },
    }
    monkeypatch.delenv("ENABLE_ADMIN_DELETE_REQUESTS", raising=False)
    monkeypatch.setattr(module, "_get_db", lambda: _FakeDb(docs))

    runner = CliRunner()
    result = runner.invoke(module.cli, ["--preflight"])

    assert result.exit_code == 2
    assert "effective real auth delete: off" in result.output


def test_cli_preflight_exits_nonzero_when_kill_switch_paused(monkeypatch):
    docs = {
        "system_config_admin_delete": {
            "paused": True,
            "reason": "incident",
        },
    }
    monkeypatch.setenv("ENABLE_ADMIN_DELETE_REQUESTS", "true")
    monkeypatch.setattr(module, "_get_db", lambda: _FakeDb(docs))

    runner = CliRunner()
    result = runner.invoke(module.cli, ["--preflight", "--enable-real-auth-delete"])

    assert result.exit_code == 3
    assert "kill-switch paused: yes" in result.output
    assert "kill-switch reason: incident" in result.output
