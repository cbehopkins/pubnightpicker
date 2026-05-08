#!/usr/bin/env python3
"""
E2E Test Orchestrator for Chat Push Notifications

Coordinates:
1. Seed smoke users via firebase-admin
2. Spawn Node frontend client (posts chat message)
3. Spawn Python notifier subprocess (processes chat push)
4. Assert on notifier logs and Firestore side effects
"""

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from threading import Thread
from typing import TypedDict

# Add parent directory to path so we can import firebase_sub modules
FIREBASE_SUB_DIR = Path(__file__).parent.parent / "firebase_sub"
sys.path.insert(0, str(FIREBASE_SUB_DIR))

import firebase_admin
import google.oauth2.credentials
from firebase_admin import firestore
from firebase_sub.cli.bootstrap import seed_smoke_data
from google.cloud.firestore_v1.client import Client

# Emulator configuration
FIRESTORE_EMULATOR_HOST = "127.0.0.1:8180"
AUTH_EMULATOR_HOST = "127.0.0.1:9199"
PROJECT_ID = "demo-firebase-sub-integration"

# Timeouts and limits
NOTIFIER_START_TIMEOUT_SEC = 15  # Wait for notifier subprocess to start
CHAT_PUSH_TIMEOUT_SEC = 10  # Wait for chat_push_actions to appear or log to be recorded
POLL_INTERVAL_SEC = 0.2  # Poll frequency for checking Firestore or logs


class FrontendClientResult(TypedDict, total=False):
    """Shape of the JSON object returned by the Node frontend client script."""

    success: bool
    messageId: str
    error: str


class EmulatorCredentials(firebase_admin.credentials.Base):
    """Stub credential for emulator-only (demo project) usage.

    Mirrors ``firebase_sub.tests.integration._emulator_helpers.EmulatorCredentials``.
    Kept local to avoid a cross-package import from the tests subtree.
    """

    def get_credential(self) -> google.oauth2.credentials.Credentials:
        return google.oauth2.credentials.Credentials(token="owner")


def init_firebase() -> Client:
    """Initialize Firebase Admin SDK for emulator access."""
    os.environ["FIRESTORE_EMULATOR_HOST"] = FIRESTORE_EMULATOR_HOST
    os.environ["FIREBASE_AUTH_EMULATOR_HOST"] = AUTH_EMULATOR_HOST

    try:
        firebase_admin.get_app()
    except ValueError:
        firebase_admin.initialize_app(
            credential=EmulatorCredentials(),
            options={"projectId": PROJECT_ID},
        )

    return firestore.client()


def seed_users(db: Client) -> None:
    """Seed smoke users into Firestore."""
    print("[Orchestrator] Seeding smoke users...")
    result = seed_smoke_data(db, dry_run=False)
    print(f"[Orchestrator] Seeded {len(result.wrote_docs)} docs")


def run_frontend_client(e2e_dir: Path) -> str | None:
    """
    Run the Node frontend client and extract the messageId.

    Returns the messageId if successful, None otherwise.
    """
    print("[Orchestrator] Starting Node frontend client...")
    client_script = e2e_dir / "frontend-client.js"

    try:
        result = subprocess.run(
            ["node", str(client_script)],
            capture_output=True,
            text=True,
            timeout=10,
        )

        if result.returncode != 0:
            print(f"[Orchestrator] Frontend client failed: {result.stderr}")
            return None

        # Parse JSON output from frontend
        output_lines = result.stdout.strip().split("\n")
        # Last non-empty line should be JSON
        json_line = [line for line in output_lines if line.strip()][-1]
        data: FrontendClientResult = json.loads(json_line)

        if data.get("success"):
            message_id = data.get("messageId")
            print(f"[Orchestrator] Frontend client posted message: {message_id}")
            return message_id
        else:
            print(f"[Orchestrator] Frontend client error: {data.get('error')}")
            return None

    except subprocess.TimeoutExpired:
        print("[Orchestrator] Frontend client timeout")
        return None
    except Exception as e:
        print(f"[Orchestrator] Frontend client exception: {e}")
        return None


class NotifierLogCapture:
    """Capture and parse notifier subprocess logs."""

    def __init__(self):
        self.logs: list[str] = []

    def add_line(self, line: str):
        """Add a log line."""
        self.logs.append(line)

    def has_chat_push_delivery(self, delivered_count: int = 1) -> bool:
        """Check if 'Chat push delivery' log appears with expected delivery count."""
        for log in self.logs:
            if "Chat push delivery:" in log and f"delivered={delivered_count}" in log:
                return True
        return False

    def has_no_eligible_recipients(self) -> bool:
        """Check if 'No eligible recipients' log appears."""
        for log in self.logs:
            if "No eligible recipients for chat push" in log:
                return True
        return False


def run_notifier_subprocess(firebase_sub_dir: Path) -> subprocess.Popen:
    """
    Start notifier subprocess in dummy mode and return process.

    The notifier will run indefinitely until the process is terminated.
    """
    print("[Orchestrator] Starting notifier subprocess in dummy mode...")

    # Build command to run notifier via poetry
    cmd = [
        "poetry",
        "run",
        "python",
        "-m",
        "firebase_sub.cli.sub_events",
        "--dummy-push",
        "--loglevel",
        "info",
    ]

    env = os.environ.copy()
    env.setdefault("ENABLE_WEB_PUSH", "true")
    env.setdefault("FIRESTORE_EMULATOR_HOST", FIRESTORE_EMULATOR_HOST)
    env.setdefault("FIREBASE_AUTH_EMULATOR_HOST", AUTH_EMULATOR_HOST)
    env.setdefault("GOOGLE_CLOUD_PROJECT", PROJECT_ID)

    # Change to firebase_sub directory to run poetry command
    try:
        process = subprocess.Popen(
            cmd,
            cwd=str(firebase_sub_dir),
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,  # Line-buffered
        )
        print(
            "[Orchestrator] Notifier subprocess started (PID: {})".format(process.pid)
        )
        return process
    except Exception as e:
        print(f"[Orchestrator] Failed to start notifier: {e}")
        raise


def wait_for_notifier_ready(
    process: subprocess.Popen, timeout_sec: float, log_capture: NotifierLogCapture
) -> bool:
    """Wait for notifier startup logs that indicate listeners are active."""
    required_markers = (
        "Notification request/ack mirror listener started",
        "Poll listeners using recent history",
        "Chat message push listener started",
    )
    seen: set[str] = set()
    start = time.time()

    while time.time() - start < timeout_sec:
        if process.poll() is not None:
            return False

        line = process.stdout.readline()
        if not line:
            continue

        line = line.rstrip("\n")
        if line:
            print(f"[Notifier] {line}")
            log_capture.add_line(line)

            for marker in required_markers:
                if marker in line:
                    seen.add(marker)

            if len(seen) == len(required_markers):
                return True

    return False


def read_notifier_output(
    process: subprocess.Popen,
    timeout_sec: float,
    log_capture: NotifierLogCapture,
) -> None:
    """
    Read notifier output in a background thread for the given timeout.

    Stores logs in log_capture.logs.
    """

    def read_output():
        start = time.time()
        try:
            while time.time() - start < timeout_sec:
                line = process.stdout.readline()
                if not line:
                    break
                line = line.rstrip("\n")
                if line:
                    print(f"[Notifier] {line}")
                    log_capture.add_line(line)
        except Exception as e:
            print(f"[Orchestrator] Error reading notifier output: {e}")

    thread = Thread(target=read_output, daemon=True)
    thread.start()
    thread.join(timeout=timeout_sec)


def wait_for_chat_push_actions(db: Client, message_id: str, timeout_sec: float) -> bool:
    """Wait for chat_push_actions document to be written."""
    start = time.time()
    while time.time() - start < timeout_sec:
        doc = db.collection("chat_push_actions").document(message_id).get()
        if doc.exists:
            data = doc.to_dict()
            print(f"[Orchestrator] Found chat_push_actions: {data}")
            return True
        time.sleep(POLL_INTERVAL_SEC)
    return False


def test_chat_push_e2e_flow() -> None:
    """Run E2E chat push flow and assert end-to-end delivery evidence."""
    e2e_dir = Path(__file__).parent
    firebase_sub_dir = e2e_dir.parent / "firebase_sub"

    print("\n=== PubNightPicker E2E Chat Push Test ===\n")

    # Initialize Firebase
    db = init_firebase()
    print("[Orchestrator] Firebase initialized (emulator mode)")

    # Seed users
    seed_users(db)

    # Spawn notifier subprocess
    notifier_process = None
    log_capture = NotifierLogCapture()

    try:
        notifier_process = run_notifier_subprocess(firebase_sub_dir)

        # Wait until notifier listeners are confirmed ready.
        ready = wait_for_notifier_ready(
            notifier_process, NOTIFIER_START_TIMEOUT_SEC, log_capture
        )
        assert ready, "notifier did not become ready in time"

        # Spawn frontend client after notifier is listening so the create event
        # is observed by the worker.
        message_id = run_frontend_client(e2e_dir)
        assert message_id is not None, "frontend client did not return a message ID"

        # Trigger the chat push handler by waiting for listener to pick up the message
        # (it should already have the message from frontend client)
        # The notifier should process it within a few seconds.

        print(
            f"[Orchestrator] Waiting for chat push processing (timeout: {CHAT_PUSH_TIMEOUT_SEC}s)..."
        )

        # Start reading notifier output in background
        read_notifier_output(notifier_process, CHAT_PUSH_TIMEOUT_SEC, log_capture)

        # Check for success: either chat_push_actions is written OR expected log appears
        actions_written = wait_for_chat_push_actions(
            db, message_id, CHAT_PUSH_TIMEOUT_SEC
        )

        # Check notifier logs
        has_delivery_log = log_capture.has_chat_push_delivery(delivered_count=1)
        has_no_recipients_log = log_capture.has_no_eligible_recipients()

        print("\n=== E2E Test Results ===")
        print(f"Message ID: {message_id}")
        print(f"chat_push_actions written: {actions_written}")
        print(f"Notifier logs show delivery: {has_delivery_log}")
        print(f"Notifier logs show no recipients: {has_no_recipients_log}")

        # Success criteria: either we saw the delivery log OR chat_push_actions was written
        success = actions_written or has_delivery_log
        if not success:
            print("\nNotifier logs captured:")
            for log in log_capture.logs:
                print(f"  {log}")

        assert success, "no evidence of chat push processing"

    finally:
        # Cleanup: terminate notifier subprocess
        if notifier_process and notifier_process.poll() is None:
            print("\n[Orchestrator] Terminating notifier subprocess...")
            notifier_process.terminate()
            try:
                notifier_process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                print("[Orchestrator] Notifier did not terminate; killing...")
                notifier_process.kill()
                notifier_process.wait()
