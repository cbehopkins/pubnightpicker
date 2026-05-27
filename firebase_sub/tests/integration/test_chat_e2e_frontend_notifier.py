"""E2E integration test: frontend SDK -> Firestore -> notifier worker.

This test intentionally avoids direct calls to chat_message_push_handler and instead:
1) Uses Firebase Web SDK (Node script) to sign in and write chat messages.
2) Runs the notifier CLI worker process (sub_events) as an external process.
3) Asserts outcomes via chat_push_actions documents in Firestore.
"""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path
from typing import Any, TypedDict

import pytest
from google.cloud.firestore_v1.client import Client

REPO_ROOT = Path(__file__).resolve().parents[3]
REACT_DIR = REPO_ROOT / "react"
CHAT_CLIENT_SCRIPT = REACT_DIR / "poc" / "e2e_chat_client.mjs"


class FrontendClientResult(TypedDict, total=False):
    """Shape of the JSON object returned by the Node frontend client script."""

    uid: str
    messageId: str
    success: bool
    error: str


def _has_frontend_runtime() -> tuple[bool, str]:
    if shutil.which("node") is None:
        return False, "node is not installed"
    if not CHAT_CLIENT_SCRIPT.exists():
        return False, f"missing script: {CHAT_CLIENT_SCRIPT}"
    if not (REACT_DIR / "node_modules" / "firebase").exists():
        return (
            False,
            "react/node_modules/firebase is missing (run npm install in react/)",
        )
    return True, "ok"


def _run_frontend_client(*args: str) -> FrontendClientResult:
    env = os.environ.copy()
    completed = subprocess.run(
        ["node", str(CHAT_CLIENT_SCRIPT), *args],
        cwd=str(REACT_DIR),
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        raise AssertionError(
            "frontend client command failed\n"
            f"args={args}\n"
            f"stdout={completed.stdout}\n"
            f"stderr={completed.stderr}"
        )

    lines = [line.strip() for line in completed.stdout.splitlines() if line.strip()]
    assert lines, f"frontend client returned no output for args={args}"
    return json.loads(lines[-1])


def _seed_user_with_preferences(
    firestore_client: Client,
    *,
    uid: str,
    name: str,
    web_push_enabled: bool,
    poll_opens: bool,
    poll_completes: bool,
    global_chat: bool,
    event_chat: bool,
) -> None:
    firestore_client.collection("users").document(uid).set(
        {
            "uid": uid,
            "name": name,
            "webPushEnabled": web_push_enabled,
            "pushPreferences": {
                "pollOpens": poll_opens,
                "pollCompletes": poll_completes,
                "globalChat": global_chat,
                "eventChat": event_chat,
            },
        },
        merge=True,
    )
    firestore_client.collection("users").document(uid).collection(
        "push_endpoints"
    ).document(f"ep-{uid}").set(
        {
            "endpoint": f"https://push.example/{uid}",
            "p256dh": "test-p256dh-key",
            "auth": "test-auth-key",
            "active": True,
        },
        merge=True,
    )


def _await_chat_action(
    firestore_client: Client, message_id: str, timeout_seconds: float = 20.0
) -> dict[str, Any] | None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        doc = (
            firestore_client.collection("chat_push_actions")
            .document(message_id)
            .get()
            .to_dict()
        )
        if doc is not None:
            return doc
        time.sleep(0.1)
    return None


def _notifier_env() -> dict[str, str]:
    env = os.environ.copy()
    env.setdefault("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8180")
    env.setdefault("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9199")
    env.setdefault("GOOGLE_CLOUD_PROJECT", "demo-firebase-sub-integration")
    return env


def _start_notifier_worker() -> subprocess.Popen[str]:
    proc = subprocess.Popen(
        [
            sys.executable,
            "-m",
            "firebase_sub.cli.sub_events",
            "--dummy-email",
            "--dummy-push",
            "--loglevel",
            "INFO",
            "--restart-interval",
            "1",
            "--housekeeping-interval-seconds",
            "3600",
        ],
        cwd=str(REPO_ROOT / "firebase_sub"),
        env=_notifier_env(),
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    time.sleep(1.0)
    if proc.poll() is not None:
        output = proc.stdout.read() if proc.stdout else ""
        raise AssertionError(
            f"sub_events exited early: code={proc.returncode}\\n{output}"
        )
    return proc


def _stop_notifier_worker(proc: subprocess.Popen[str]) -> None:
    if proc.poll() is not None:
        return
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)


@contextmanager
def _running_notifier_worker() -> Generator[subprocess.Popen[str], None, None]:
    proc = _start_notifier_worker()
    try:
        yield proc
    finally:
        _stop_notifier_worker(proc)


def _endpoint_hash_for_user(*, uid: str, endpoint: str) -> str:
    raw = f"{uid}__{endpoint}"
    return hashlib.sha256(raw.encode()).hexdigest()[:32]


@pytest.fixture(scope="module")
def notifier_worker() -> Generator[subprocess.Popen, None, None]:
    with _running_notifier_worker() as proc:
        yield proc


@pytest.mark.e2e
class TestChatE2EFrontendToNotifier:
    def test_global_chat_e2e_matrix(self, firestore_client, notifier_worker):
        ok, reason = _has_frontend_runtime()
        if not ok:
            pytest.skip(f"Skipping frontend E2E test: {reason}")

        # Create frontend-authenticated users and capture UIDs from Auth emulator.
        a = _run_frontend_client("signup", "e2e-a@example.com", "pw12345")
        b = _run_frontend_client("signup", "e2e-b@example.com", "pw12345")
        c = _run_frontend_client("signup", "e2e-c@example.com", "pw12345")

        # Apply required notification preferences in Firestore.
        _seed_user_with_preferences(
            firestore_client,
            uid=a["uid"],
            name="E2E A",
            web_push_enabled=True,
            poll_opens=True,
            poll_completes=True,
            global_chat=True,
            event_chat=True,
        )
        _seed_user_with_preferences(
            firestore_client,
            uid=b["uid"],
            name="E2E B",
            web_push_enabled=True,
            poll_opens=False,
            poll_completes=False,
            global_chat=True,
            event_chat=False,
        )
        _seed_user_with_preferences(
            firestore_client,
            uid=c["uid"],
            name="E2E C",
            web_push_enabled=False,
            poll_opens=False,
            poll_completes=False,
            global_chat=False,
            event_chat=False,
        )

        # Scenario 1: A posts -> only B should be notified.
        msg_a = _run_frontend_client(
            "send-message",
            "e2e-a@example.com",
            "pw12345",
            "E2E A",
            "hello from A",
            "global",
            "main",
        )
        action_a = _await_chat_action(firestore_client, msg_a["messageId"])
        assert action_a is not None, "Expected chat_push_actions for message from A"
        assert set(action_a["notified"]) == {b["uid"]}

        # Scenario 2: C posts -> A and B should be notified.
        msg_c = _run_frontend_client(
            "send-message",
            "e2e-c@example.com",
            "pw12345",
            "E2E C",
            "hello from C",
            "global",
            "main",
        )
        action_c = _await_chat_action(firestore_client, msg_c["messageId"])
        assert action_c is not None, "Expected chat_push_actions for message from C"
        assert set(action_c["notified"]) == {a["uid"], b["uid"]}

        # Keep fixture reference explicit so lint/test readers see the worker dependency.
        assert notifier_worker.poll() is None

    def test_global_chat_not_replayed_after_worker_restart(self, firestore_client):
        ok, reason = _has_frontend_runtime()
        if not ok:
            pytest.skip(f"Skipping frontend E2E test: {reason}")

        # Create frontend-authenticated users and capture UIDs from Auth emulator.
        a = _run_frontend_client("signup", "e2e-replay-a@example.com", "pw12345")
        b = _run_frontend_client("signup", "e2e-replay-b@example.com", "pw12345")

        _seed_user_with_preferences(
            firestore_client,
            uid=a["uid"],
            name="Replay A",
            web_push_enabled=True,
            poll_opens=True,
            poll_completes=True,
            global_chat=True,
            event_chat=True,
        )
        _seed_user_with_preferences(
            firestore_client,
            uid=b["uid"],
            name="Replay B",
            web_push_enabled=True,
            poll_opens=False,
            poll_completes=False,
            global_chat=True,
            event_chat=False,
        )

        # First run: emit one message and let worker process it.
        with _running_notifier_worker():
            msg = _run_frontend_client(
                "send-message",
                "e2e-replay-a@example.com",
                "pw12345",
                "Replay A",
                "hello replay guard",
                "global",
                "main",
            )
            action_doc = _await_chat_action(firestore_client, msg["messageId"])

        assert action_doc is not None, "Expected chat_push_actions for replay test"
        assert action_doc["processed"] is True
        initial_notified = set(action_doc.get("notified", []))
        initial_delivered = set(action_doc.get("delivered_endpoints", []))
        initial_processed_at = action_doc.get("processedAt")

        base_endpoint = f"https://push.example/{b['uid']}"
        expected_base_hash = _endpoint_hash_for_user(
            uid=b["uid"], endpoint=base_endpoint
        )
        assert initial_notified == {b["uid"]}
        assert expected_base_hash in initial_delivered

        # Add a new endpoint after the message was already processed.
        replay_endpoint = f"https://push.example/{b['uid']}-replay"
        firestore_client.collection("users").document(b["uid"]).collection(
            "push_endpoints"
        ).document(f"ep-{b['uid']}-replay").set(
            {
                "endpoint": replay_endpoint,
                "p256dh": "test-p256dh-key",
                "auth": "test-auth-key",
                "active": True,
            },
            merge=True,
        )
        replay_hash = _endpoint_hash_for_user(uid=b["uid"], endpoint=replay_endpoint)

        # Restart worker: startup replay should not deliver for this old message again.
        with _running_notifier_worker():
            time.sleep(2.0)

        action_after = (
            firestore_client.collection("chat_push_actions")
            .document(msg["messageId"])
            .get()
            .to_dict()
        )
        assert action_after is not None
        assert action_after["processed"] is True
        assert set(action_after.get("notified", [])) == initial_notified
        assert set(action_after.get("delivered_endpoints", [])) == initial_delivered
        assert replay_hash not in set(action_after.get("delivered_endpoints", []))
        assert action_after.get("processedAt") == initial_processed_at

    def test_no_eligible_recipients_not_replayed_after_restart(self, firestore_client):
        ok, reason = _has_frontend_runtime()
        if not ok:
            pytest.skip(f"Skipping frontend E2E test: {reason}")

        # Only author exists initially, so no recipients are eligible.
        a = _run_frontend_client("signup", "e2e-noeligible-a@example.com", "pw12345")
        _seed_user_with_preferences(
            firestore_client,
            uid=a["uid"],
            name="NoEligible A",
            web_push_enabled=True,
            poll_opens=True,
            poll_completes=True,
            global_chat=True,
            event_chat=True,
        )

        with _running_notifier_worker():
            msg = _run_frontend_client(
                "send-message",
                "e2e-noeligible-a@example.com",
                "pw12345",
                "NoEligible A",
                "hello no eligible",
                "global",
                "main",
            )
            action_doc = _await_chat_action(firestore_client, msg["messageId"])

        assert action_doc is not None, "Expected chat_push_actions for no-eligible test"
        assert action_doc["processed"] is True
        initial_notified = set(action_doc.get("notified", []))
        initial_delivered = set(action_doc.get("delivered_endpoints", []))
        initial_processed_at = action_doc.get("processedAt")
        assert initial_notified == set()
        assert initial_delivered == set()

        # Add a newly eligible user after the message was already processed.
        b = _run_frontend_client("signup", "e2e-noeligible-b@example.com", "pw12345")
        _seed_user_with_preferences(
            firestore_client,
            uid=b["uid"],
            name="NoEligible B",
            web_push_enabled=True,
            poll_opens=False,
            poll_completes=False,
            global_chat=True,
            event_chat=False,
        )

        replay_endpoint = f"https://push.example/{b['uid']}"
        replay_hash = _endpoint_hash_for_user(uid=b["uid"], endpoint=replay_endpoint)

        with _running_notifier_worker():
            time.sleep(2.0)

        action_after = (
            firestore_client.collection("chat_push_actions")
            .document(msg["messageId"])
            .get()
            .to_dict()
        )
        assert action_after is not None
        assert action_after["processed"] is True
        assert set(action_after.get("notified", [])) == initial_notified
        assert set(action_after.get("delivered_endpoints", [])) == initial_delivered
        assert replay_hash not in set(action_after.get("delivered_endpoints", []))
        assert action_after.get("processedAt") == initial_processed_at
