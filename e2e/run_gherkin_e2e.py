#!/usr/bin/env python3
"""Run browser journeys inside fresh Firebase emulators."""

import os
import platform
import signal
import json
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
E2E_DIR = REPO_ROOT / "e2e"
FIREBASE_SUB_DIR = REPO_ROOT / "firebase_sub"
REACT_DIR = REPO_ROOT / "react"
EMULATOR_PROJECT = "demo-pubnightpicker-journey"
ADMIN_UID = "journey-admin"
ADMIN_EMAIL = "journey-admin@test.local"
ADMIN_PASSWORD = "journey-only-password-2026"
BASE_URL = "http://127.0.0.1:3000"


def _tool(name: str) -> str:
    candidates = (f"{name}.cmd", name) if platform.system() == "Windows" else (name,)
    for candidate in candidates:
        resolved = shutil.which(candidate)
        if resolved:
            return resolved
    raise FileNotFoundError(f"{name} was not found on PATH")


def _find_firebase() -> list[str]:
    for candidate in (("firebase.cmd", "firebase") if platform.system() == "Windows" else ("firebase",)):
        resolved = shutil.which(candidate)
        if resolved:
            return [resolved]
    return [_tool("npx"), "firebase-tools"]


def _require_free_emulator_ports() -> None:
    for port in (8080, 9099, 4400, 9150):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
            try:
                probe.bind(("127.0.0.1", port))
            except OSError as error:
                raise RuntimeError(
                    f"Port {port} is unavailable; stop the existing service before "
                    "running this fresh-database journey"
                ) from error


def _remove_stale_hub_locator() -> None:
    locator = Path(tempfile.gettempdir()) / f"hub-{EMULATOR_PROJECT}.json"
    if not locator.exists():
        return
    try:
        metadata = json.loads(locator.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return
    origins = metadata.get("origins", [])
    if not any(origin.endswith(":4400") for origin in origins):
        return
    try:
        with socket.create_connection(("127.0.0.1", 4400), timeout=0.5):
            return
    except OSError:
        locator.unlink(missing_ok=True)
        print("[Journey] Removed stale Firebase emulator hub locator", flush=True)


def _wait_for_emulators(process: subprocess.Popen) -> None:
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"Firebase emulators exited during startup with code {process.returncode}")
        listening = []
        for port in (8080, 9099):
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                    listening.append(port)
            except OSError:
                pass
        if len(listening) == 2:
            return
        time.sleep(0.25)
    raise TimeoutError("Auth and Firestore emulators did not become ready within 90 seconds")


def _stop_emulators(process: subprocess.Popen) -> None:
    # Send an interrupt to the Firebase CLI process group so its own shutdown
    # handler can stop the emulators it launched.
    if process.poll() is None:
        try:
            if platform.system() == "Windows":
                process.send_signal(signal.CTRL_BREAK_EVENT)
            else:
                os.killpg(process.pid, signal.SIGINT)
            process.wait(timeout=30)
        except subprocess.TimeoutExpired:
            print("[Journey] Firebase CLI did not stop after interrupt; terminating it", file=sys.stderr)
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        except OSError as error:
            print(f"[Journey] Could not interrupt Firebase CLI: {error}", file=sys.stderr)
            process.terminate()
            process.wait()

    if platform.system() == "Windows":
        _stop_orphaned_emulator_listeners()


def _stop_orphaned_emulator_listeners() -> None:
    """Clean up emulator children left behind by the Windows CLI process."""
    result = subprocess.run(
        ["netstat", "-ano", "-p", "tcp"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        print("[Journey] Could not inspect emulator ports during cleanup", file=sys.stderr)
        return

    leaked: dict[int, set[int]] = {}
    for line in result.stdout.splitlines():
        parts = line.split()
        if len(parts) < 5 or parts[0].upper() != "TCP" or parts[3].upper() != "LISTENING":
            continue
        try:
            port = int(parts[1].rsplit(":", 1)[1])
            pid = int(parts[4])
        except (IndexError, ValueError):
            continue
        if port in {8080, 9099, 4400, 9150} and pid > 0:
            leaked.setdefault(pid, set()).add(port)

    for pid, ports in leaked.items():
        process_info = subprocess.run(
            ["tasklist", "/FI", f"PID eq {pid}", "/FO", "CSV", "/NH"],
            capture_output=True,
            text=True,
            check=False,
        )
        image_name = process_info.stdout.split(",", 1)[0].strip('"').lower()
        if image_name not in {"java.exe", "node.exe"}:
            print(
                f"[Journey] Port(s) {sorted(ports)} remain occupied by {image_name or pid}; "
                "leaving the process untouched",
                file=sys.stderr,
            )
            continue
        print(
            f"[Journey] Stopping leftover emulator process {pid} on port(s) {sorted(ports)}",
            file=sys.stderr,
        )
        subprocess.run(
            ["taskkill", "/PID", str(pid), "/T", "/F"],
            capture_output=True,
            text=True,
            check=False,
        )


def _run_emulator_journey() -> int:
    _require_free_emulator_ports()
    _remove_stale_hub_locator()
    firebase = _find_firebase()
    creation_options = {}
    if platform.system() == "Windows":
        creation_options["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        creation_options["start_new_session"] = True
    command = [
        *firebase,
        "emulators:start",
        "--config",
        str(REACT_DIR / "firebase.e2e.json"),
        "--only",
        "auth,firestore",
        "--project",
        EMULATOR_PROJECT,
    ]
    print("[Journey] Starting fresh Auth and Firestore emulators (no import)", flush=True)
    print("[Journey] Project: " + EMULATOR_PROJECT, flush=True)
    process = subprocess.Popen(command, cwd=REACT_DIR, **creation_options)
    try:
        _wait_for_emulators(process)
        return _run_browser_journey()
    finally:
        _stop_emulators(process)


def _wait_for_frontend(process: subprocess.Popen) -> None:
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"Vite exited during startup with code {process.returncode}")
        try:
            with urllib.request.urlopen(BASE_URL, timeout=2) as response:
                if response.status == 200:
                    return
        except (urllib.error.URLError, TimeoutError):
            time.sleep(0.25)
    raise TimeoutError(f"Vite did not become ready at {BASE_URL}")


def _run_browser_journey() -> int:
    env = os.environ.copy()
    env.update(
        {
            "GOOGLE_CLOUD_PROJECT": EMULATOR_PROJECT,
            "FIRESTORE_EMULATOR_HOST": "127.0.0.1:8080",
            "FIREBASE_AUTH_EMULATOR_HOST": "127.0.0.1:9099",
            "E2E_ADMIN_EMAIL": ADMIN_EMAIL,
            "E2E_ADMIN_PASSWORD": ADMIN_PASSWORD,
            "E2E_BASE_URL": BASE_URL,
        }
    )

    bootstrap = [
        _tool("poetry"),
        "run",
        "python",
        "firebase_sub/cli/bootstrap.py",
        "create-admin",
        "--uid",
        ADMIN_UID,
        "--name",
        "Journey Admin",
        "--email",
        ADMIN_EMAIL,
        "--password",
        ADMIN_PASSWORD,
    ]
    print("[Journey] Creating the admin through firebase_sub/cli/bootstrap.py", flush=True)
    result = subprocess.run(bootstrap, cwd=FIREBASE_SUB_DIR, env=env, check=False)
    if result.returncode:
        return result.returncode

    vite_entry = REACT_DIR / "node_modules" / "vite" / "bin" / "vite.js"
    if not vite_entry.exists():
        raise FileNotFoundError("React dependencies are missing; run npm ci in react/")
    env.update(
        {
            "VITE_FIREBASE_API_KEY": "journey-emulator-key",
            "VITE_FIREBASE_AUTH_DOMAIN": f"{EMULATOR_PROJECT}.firebaseapp.com",
            "VITE_FIREBASE_PROJECT_ID": EMULATOR_PROJECT,
            "VITE_FIREBASE_STORAGE_BUCKET": f"{EMULATOR_PROJECT}.appspot.com",
            "VITE_FIREBASE_MESSAGING_SENDER_ID": "1234567890",
            "VITE_FIREBASE_APP_ID": "1:1234567890:web:journey",
            "VITE_USE_FIREBASE_EMULATORS": "true",
        }
    )

    node = _tool("node")
    frontend = subprocess.Popen(
        [node, str(vite_entry), "--host", "127.0.0.1", "--port", "3000", "--strictPort"],
        cwd=REACT_DIR,
        env=env,
    )
    try:
        _wait_for_frontend(frontend)
        print("[Journey] React dev server is ready; running the Gherkin scenarios", flush=True)
        cucumber = E2E_DIR / "node_modules" / "@cucumber" / "cucumber" / "bin" / "cucumber.js"
        if not cucumber.exists():
            raise FileNotFoundError("E2E dependencies are missing; run npm ci in e2e/")
        return subprocess.run(
            [node, str(cucumber), "features/**/*.feature"],
            cwd=E2E_DIR,
            env=env,
            check=False,
        ).returncode
    finally:
        frontend.terminate()
        try:
            frontend.wait(timeout=10)
        except subprocess.TimeoutExpired:
            frontend.kill()
            frontend.wait()


if __name__ == "__main__":
    try:
        sys.exit(_run_emulator_journey())
    except (FileNotFoundError, RuntimeError, TimeoutError) as error:
        print(f"[Journey] ERROR: {error}", file=sys.stderr)
        sys.exit(1)
