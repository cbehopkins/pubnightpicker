#!/usr/bin/env python3
"""Single top-level command to run all emulator-backed integration tests.

Usage:
    python run_integration_tests.py [--unit] [--pytest-args ARGS...]

The script:
  1. Starts the Firestore emulator via ``firebase emulators:exec``.
  2. Runs ``pytest -m integration`` inside that emulator context.
  3. Returns the pytest exit code so CI/shell can act on it.

Prerequisites (one-time setup):
  - Node / npm with firebase-tools installed globally:
      npm install -g firebase-tools
  - Java (required by the Firestore emulator):
      https://adoptium.net
  - Python dependencies for the firebase_sub package:
      cd firebase_sub && poetry install

Windows note:
  firebase-tools ships as ``firebase.cmd`` on Windows.  The script detects
  this automatically and does not require any manual PATH setup beyond
  installing firebase-tools.
"""

import argparse
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent
FIREBASE_SUB_DIR = REPO_ROOT / "firebase_sub"
INTEGRATION_CONFIG = "firebase.integration.json"
EMULATOR_PROJECT = "demo-firebase-sub-integration"


def _find_firebase_parts() -> list[str]:
    """Return command parts for invoking firebase-tools.

    Returns either [<path-to-firebase>] or [<path-to-npx>, "firebase-tools"].
    """
    if platform.system() == "Windows":
        # firebase-tools on Windows is usually installed as firebase.cmd
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


def _build_cmd(firebase_parts: list[str], test_command: str) -> list[str]:
    """Build the subprocess argument list for emulators:exec."""
    return [
        *firebase_parts,
        "emulators:exec",
        "--config", INTEGRATION_CONFIG,
        "--only", "firestore,auth",
        "--project", EMULATOR_PROJECT,
        test_command,
    ]


def _build_stop_cmd(firebase_parts: list[str]) -> list[str]:
    """Build a best-effort command to stop any running emulators for this project."""
    return [
        *firebase_parts,
        "emulators:stop",
        "--project", EMULATOR_PROJECT,
    ]


def _kill_lingering_emulator_processes_windows() -> list[int]:
    """Force-kill processes listening on known Firebase emulator ports (Windows only)."""
    if platform.system() != "Windows":
        return []

    emulator_ports = {8180, 9199, 4400, 4401, 4500, 4501, 9151}
    try:
        netstat = subprocess.run(
            ["netstat", "-ano"],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError:
        return []

    if netstat.returncode != 0:
        return []

    pids: set[int] = set()
    for line in netstat.stdout.splitlines():
        parts = line.split()
        if len(parts) < 5:
            continue
        state = parts[3].upper()
        if state != "LISTENING":
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
        if pid != os.getpid():
            pids.add(pid)

    killed: list[int] = []
    for pid in sorted(pids):
        result = subprocess.run(
            ["taskkill", "/PID", str(pid), "/F"],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        if result.returncode == 0:
            killed.append(pid)

    return killed


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Run emulator-backed integration tests for pubnightpicker.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument(
        "--unit",
        action="store_true",
        help="Also run unit tests (no emulator required) before integration tests.",
    )
    parser.add_argument(
        "pytest_args",
        nargs=argparse.REMAINDER,
        metavar="PYTEST_ARG",
        help=(
            "Extra arguments forwarded to pytest (e.g. -k smoke -v "
            "tests/integration/test_x.py)."
        ),
    )
    args = parser.parse_args(argv)

    if args.unit:
        print("=== Running unit tests ===")
        unit_result = subprocess.run(
            ["poetry", "run", "pytest", "-m", "not integration", "-v", "--tb=short"],
            cwd=str(FIREBASE_SUB_DIR),
            check=False,
        )
        if unit_result.returncode != 0:
            print(f"\nUnit tests FAILED (exit code {unit_result.returncode})")
            return unit_result.returncode
        print("\nUnit tests PASSED.\n")

    firebase_parts = _find_firebase_parts()

    forwarded_args = list(args.pytest_args)
    extra = " ".join(forwarded_args) if forwarded_args else ""
    test_command = " ".join(
        filter(None, ["poetry", "run", "pytest", "-m", "integration", "-v", "--tb=short", extra])
    )

    cmd = _build_cmd(firebase_parts, test_command)

    print("=== Running integration tests via Firebase emulator ===")
    print(f"Working directory : {FIREBASE_SUB_DIR}")
    print(f"Firebase command  : {' '.join(firebase_parts)}")
    print(f"Test command      : {test_command}")
    print()

    result = subprocess.CompletedProcess(cmd, returncode=1)
    try:
        result = subprocess.run(
            cmd,
            cwd=str(FIREBASE_SUB_DIR),
            check=False,
            shell=False,
        )
    finally:
        # emulators:exec should stop emulators itself, but this extra stop avoids
        # lingering processes after interrupted/failed runs.
        stop_cmd = _build_stop_cmd(firebase_parts)
        stop_result = subprocess.run(
            stop_cmd,
            cwd=str(FIREBASE_SUB_DIR),
            check=False,
            shell=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        if stop_result.returncode == 0:
            print("Stopped any lingering Firebase emulators.")

        killed_pids = _kill_lingering_emulator_processes_windows()
        if killed_pids:
            print(f"Force-stopped lingering emulator PIDs: {', '.join(map(str, killed_pids))}")

    if result.returncode == 0:
        print("\nAll integration tests PASSED.")
    else:
        print(f"\nIntegration tests FAILED (exit code {result.returncode})")

    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
