"""E2E integration test: frontend SDK -> Firestore -> notifier worker.

This test intentionally avoids direct calls to chat_message_push_handler and instead:
1) Uses Firebase Web SDK (Node script) to sign in and write chat messages.
2) Runs the notifier CLI worker process (sub_events) as an external process.
3) Asserts outcomes via chat_push_actions documents in Firestore.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import time
from collections.abc import Generator
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


@pytest.fixture(scope="module")
def notifier_worker() -> Generator[subprocess.Popen, None, None]:
    env = os.environ.copy()
    env.setdefault("ENABLE_WEB_PUSH", "true")
    env.setdefault("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8180")
    env.setdefault("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9199")
    env.setdefault("GOOGLE_CLOUD_PROJECT", "demo-firebase-sub-integration")

    # sub_events blocks and listens forever, so run as a child process.
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
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )

    try:
        # Allow worker startup; tests fail fast if process exits unexpectedly.
        time.sleep(1.0)
        if proc.poll() is not None:
            output = proc.stdout.read() if proc.stdout else ""
            raise AssertionError(
                f"sub_events exited early: code={proc.returncode}\n{output}"
            )
        yield proc
    finally:
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)


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
