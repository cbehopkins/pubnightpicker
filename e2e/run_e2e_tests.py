#!/usr/bin/env python3
"""E2E Test Runner

Starts Firebase emulators via ``firebase emulators:exec`` and runs the E2E
pytest suite as the inner command. Follows the same pattern as
``run_integration_tests.py`` which is proven to work on Windows.

Usage:
    python run_e2e_tests.py
"""

import platform
import shutil
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
FIREBASE_SUB_DIR = REPO_ROOT / "firebase_sub"
E2E_DIR = REPO_ROOT / "e2e"
INTEGRATION_CONFIG = "firebase.integration.json"
EMULATOR_PROJECT = "demo-firebase-sub-integration"


def _find_firebase_parts() -> list[str]:
    """Return command parts for invoking firebase-tools (Windows-aware)."""
    if platform.system() == "Windows":
        for candidate in ("firebase.cmd", "firebase"):
            resolved = shutil.which(candidate)
            if resolved:
                return [resolved]
        npx_resolved = shutil.which("npx.cmd") or shutil.which("npx")
        if npx_resolved:
            return [npx_resolved, "firebase-tools"]
        return ["npx", "firebase-tools"]

    resolved = shutil.which("firebase")
    if resolved:
        return [resolved]
    return ["firebase"]


def _kill_lingering_emulator_processes_windows() -> None:
    """Force-kill processes listening on known Firebase emulator ports (Windows only)."""
    if platform.system() != "Windows":
        return

    emulator_ports = {8180, 9199, 4400, 4401, 4500, 4501, 9151}
    try:
        netstat = subprocess.run(
            ["netstat", "-ano"],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError:
        return

    if netstat.returncode != 0:
        return

    pids: set[int] = set()
    for line in netstat.stdout.splitlines():
        parts = line.split()
        if len(parts) < 5:
            continue
        if parts[3].upper() != "LISTENING":
            continue
        local_addr = parts[1]
        try:
            port = int(local_addr.rsplit(":", 1)[1])
        except (IndexError, ValueError):
            continue
        if port not in emulator_ports:
            continue
        try:
            pid = int(parts[4])
        except ValueError:
            continue
        if pid != 0:
            pids.add(pid)

    for pid in sorted(pids):
        subprocess.run(
            ["taskkill", "/PID", str(pid), "/F"],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )


def main() -> int:
    print("\n=== E2E Test Runner ===\n")

    firebase_parts = _find_firebase_parts()
    if not firebase_parts:
        print("[Runner] Error: firebase CLI not found in PATH")
        print("  Install with: npm install -g firebase-tools")
        return 1

    print(f"[Runner] Firebase: {' '.join(firebase_parts)}")

    # Build the inner test command as a single string (required by emulators:exec).
    # Use the absolute path to the pytest module so it works regardless of CWD.
    test_file = str(E2E_DIR / "test_e2e_chat_push.py")
    test_command = f'poetry run pytest -v --tb=short "{test_file}"'

    cmd = [
        *firebase_parts,
        "emulators:exec",
        "--config",
        INTEGRATION_CONFIG,
        "--only",
        "firestore,auth",
        "--project",
        EMULATOR_PROJECT,
        test_command,
    ]

    print(f"[Runner] Test command: {test_command}")
    print(f"[Runner] Working directory: {FIREBASE_SUB_DIR}")
    print()

    try:
        result = subprocess.run(
            cmd,
            cwd=str(FIREBASE_SUB_DIR),
            check=False,
            shell=False,
        )
        return result.returncode
    finally:
        # Belt-and-suspenders cleanup on Windows in case emulators linger.
        _kill_lingering_emulator_processes_windows()


if __name__ == "__main__":
    sys.exit(main())
