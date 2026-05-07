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
import platform
import shutil
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent
FIREBASE_SUB_DIR = REPO_ROOT / "firebase_sub"
INTEGRATION_CONFIG = "firebase.integration.json"
EMULATOR_PROJECT = "demo-firebase-sub-integration"


def _find_firebase() -> str:
    """Return the firebase command name available in PATH."""
    if platform.system() == "Windows":
        # firebase-tools on Windows is installed as firebase.cmd
        for candidate in ("firebase.cmd", "firebase"):
            if shutil.which(candidate):
                return candidate
        # Last resort: rely on npx
        return "npx firebase-tools"
    return "firebase"


def _build_cmd(firebase: str, test_command: str) -> list[str]:
    """Build the subprocess argument list for emulators:exec."""
    if firebase.startswith("npx"):
        # Split "npx firebase-tools" → ["npx", "firebase-tools"]
        parts = firebase.split()
    else:
        parts = [firebase]

    return [
        *parts,
        "emulators:exec",
        "--config", INTEGRATION_CONFIG,
        "--only", "firestore",
        "--project", EMULATOR_PROJECT,
        test_command,
    ]


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
        nargs="*",
        metavar="PYTEST_ARG",
        help="Extra arguments forwarded to pytest (e.g. -k smoke --tb=short).",
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

    firebase = _find_firebase()

    extra = " ".join(args.pytest_args) if args.pytest_args else ""
    test_command = " ".join(
        filter(None, ["poetry", "run", "pytest", "-m", "integration", "-v", "--tb=short", extra])
    )

    cmd = _build_cmd(firebase, test_command)

    print("=== Running integration tests via Firebase emulator ===")
    print(f"Working directory : {FIREBASE_SUB_DIR}")
    print(f"Firebase command  : {firebase}")
    print(f"Test command      : {test_command}")
    print()

    result = subprocess.run(
        cmd,
        cwd=str(FIREBASE_SUB_DIR),
        check=False,
        # On Windows, firebase.cmd requires shell=True when invoked via
        # subprocess without the full .cmd extension in the PATH resolution.
        shell=(platform.system() == "Windows"),
    )

    if result.returncode == 0:
        print("\nAll integration tests PASSED.")
    else:
        print(f"\nIntegration tests FAILED (exit code {result.returncode})")

    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
