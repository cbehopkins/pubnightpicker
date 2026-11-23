#!/usr/bin/env python3
"""Run isolated frontend and Last Orders journeys against demo emulators."""

import argparse
import os
import platform
import signal
import json
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path
from contextlib import ExitStack, contextmanager
from collections.abc import Iterator
from datetime import UTC, datetime

from process_owner import ProcessTree

REPO_ROOT = Path(__file__).resolve().parent.parent
E2E_DIR = REPO_ROOT / "e2e"
FIREBASE_SUB_DIR = REPO_ROOT / "firebase_sub"
REACT_DIR = REPO_ROOT / "react"
LAST_ORDERS_DIR = REPO_ROOT / "last_orders"
EMULATOR_PROJECT = "demo-pubnightpicker-journey"
ADMIN_UID = "journey-admin"
ADMIN_EMAIL = "journey-admin@test.local"
ADMIN_PASSWORD = "journey-only-password-2026"
BASE_URL = "http://127.0.0.1:3000"
BACKEND_URL = "http://127.0.0.1:8081"
CLOCK_CONTROL_ADDR = "127.0.0.1:8082"
CLOCK_CONTROL_URL = f"http://{CLOCK_CONTROL_ADDR}"
EMULATOR_PORTS = (8080, 9099, 4400, 4500, 9150)
GROUP_TAGS = {
    "frontend": "not @last_orders",
    "last-orders": "@last_orders",
    "account-deletion": "@last_orders and @account_deletion",
}
BACKEND_GROUPS = frozenset({"last-orders", "account-deletion"})
DEFAULT_GROUPS = ("frontend", "last-orders")
VITE_ENTRY = REACT_DIR / "node_modules" / "vite" / "bin" / "vite.js"
CUCUMBER_ENTRY = (
    E2E_DIR / "node_modules" / "@cucumber" / "cucumber" / "bin" / "cucumber.js"
)


def _environment(application_time: bool = False) -> dict[str, str]:
    env = _base_environment()
    if application_time:
        initial_time = datetime.now(UTC).replace(
            hour=12, minute=0, second=0, microsecond=0
        )
        env.update(
            {
                "LAST_ORDERS_E2E_INITIAL_TIME": initial_time.isoformat(
                    timespec="seconds"
                ).replace("+00:00", "Z"),
                "LAST_ORDERS_E2E_CLOCK_ADDR": CLOCK_CONTROL_ADDR,
                "E2E_CLOCK_URL": CLOCK_CONTROL_URL,
            }
        )
    return env


def _base_environment() -> dict[str, str]:
    retained = {
        "PATH",
        "PATHEXT",
        "SYSTEMROOT",
        "WINDIR",
        "COMSPEC",
        "TEMP",
        "TMP",
        "HOME",
        "USERPROFILE",
        "APPDATA",
        "LOCALAPPDATA",
        "PROGRAMFILES",
        "PROGRAMFILES(X86)",
        "JAVA_HOME",
        "GOPATH",
        "GOTOOLCHAIN",
        "GOCACHE",
        "GOMODCACHE",
        "POETRY_HOME",
        "POETRY_CACHE_DIR",
        "VIRTUAL_ENV",
        "PLAYWRIGHT_BROWSERS_PATH",
        "CI",
    }
    env = {key: value for key, value in os.environ.items() if key.upper() in retained}
    env.update(
        {
            "GOOGLE_CLOUD_PROJECT": EMULATOR_PROJECT,
            "FIREBASE_AUTH_PROJECT_ID": EMULATOR_PROJECT,
            "FIRESTORE_EMULATOR_HOST": "127.0.0.1:8080",
            "FIREBASE_AUTH_EMULATOR_HOST": "127.0.0.1:9099",
            "GOFLAGS": "",
            "GOENV": "off",
            "E2E_ADMIN_EMAIL": ADMIN_EMAIL,
            "E2E_ADMIN_PASSWORD": ADMIN_PASSWORD,
            "E2E_BASE_URL": BASE_URL,
            "ENABLE_ADMIN_DELETE_REQUESTS": "false",
            "ADMIN_DELETE_DRY_RUN": "true",
            "PUBNIGHTPICKER_WEB_BASE_URL": BASE_URL,
            "LAST_ORDERS_ALLOWED_ORIGINS": BASE_URL,
            "VITE_LAST_ORDERS_API_BASE_URL": BACKEND_URL,
            "VITE_ENABLE_WEB_PUSH": "true",
            "VITE_WEB_PUSH_PUBLIC_KEY": "BCxCmBs4i-LWqTvWNT5gQRfJ8fM6fhOVPk0Fy7QbsCj-e_1LUaG8D9aM4Na2ff9WM1kDQy78wwH6a4uJ3y8tITo",
            "VITE_FIREBASE_API_KEY": "journey-emulator-key",
            "VITE_FIREBASE_AUTH_DOMAIN": f"{EMULATOR_PROJECT}.firebaseapp.com",
            "VITE_FIREBASE_PROJECT_ID": EMULATOR_PROJECT,
            "VITE_FIREBASE_STORAGE_BUCKET": f"{EMULATOR_PROJECT}.appspot.com",
            "VITE_FIREBASE_MESSAGING_SENDER_ID": "1234567890",
            "VITE_FIREBASE_APP_ID": "1:1234567890:web:journey",
            "VITE_USE_FIREBASE_EMULATORS": "true",
            "FIREBASE_CLI_DISABLE_UPDATE_CHECK": "true",
        }
    )
    return env


def _validate_environment(env: dict[str, str], application_time: bool = False) -> None:
    expected = _base_environment()
    if application_time:
        expected.update(
            {
                "LAST_ORDERS_E2E_CLOCK_ADDR": CLOCK_CONTROL_ADDR,
                "E2E_CLOCK_URL": CLOCK_CONTROL_URL,
            }
        )
    for key, value in expected.items():
        if env.get(key) != value:
            raise RuntimeError(f"Unsafe journey configuration: {key}")
    if application_time:
        initial_time = env.get("LAST_ORDERS_E2E_INITIAL_TIME")
        if not initial_time:
            raise RuntimeError(
                "Unsafe journey configuration: LAST_ORDERS_E2E_INITIAL_TIME"
            )
        try:
            parsed_initial_time = datetime.fromisoformat(
                initial_time.replace("Z", "+00:00")
            )
        except ValueError as error:
            raise RuntimeError(
                "Unsafe journey configuration: LAST_ORDERS_E2E_INITIAL_TIME"
            ) from error
        if parsed_initial_time.tzinfo is None or parsed_initial_time.utcoffset() is None:
            raise RuntimeError(
                "Unsafe journey configuration: LAST_ORDERS_E2E_INITIAL_TIME"
            )
    unexpected = set(env) - set(expected) - {"LAST_ORDERS_CURSOR_HMAC_KEY"}
    if application_time:
        unexpected.discard("LAST_ORDERS_E2E_INITIAL_TIME")
    if unexpected:
        raise RuntimeError(
            "Unexpected child configuration keys: " + ", ".join(sorted(unexpected))
        )


def _tool(name: str) -> str:
    candidates = (f"{name}.cmd", name) if platform.system() == "Windows" else (name,)
    for candidate in candidates:
        resolved = shutil.which(candidate)
        if resolved:
            return resolved
    raise FileNotFoundError(f"{name} was not found on PATH")


def _find_firebase() -> list[str]:
    for candidate in (
        ("firebase.cmd", "firebase") if os.name == "nt" else ("firebase",)
    ):
        resolved = shutil.which(candidate)
        if resolved:
            return [resolved]
    raise FileNotFoundError(
        "Firebase CLI is missing; install firebase-tools before running journeys"
    )


def _require_free_ports(ports: tuple[int, ...]) -> None:
    for port in ports:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.25):
                raise RuntimeError(
                    f"Port {port} is unavailable; no existing service will be reused or stopped"
                )
        except OSError:
            pass
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
            if os.name != "nt":
                probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                probe.bind(("127.0.0.1", port))
            except OSError as error:
                raise RuntimeError(
                    f"Port {port} is unavailable; no existing service will be reused or stopped"
                ) from error


def _remove_stale_hub_locator() -> None:
    locator = Path(tempfile.gettempdir()) / f"hub-{EMULATOR_PROJECT}.json"
    if not locator.exists():
        return
    try:
        metadata = json.loads(locator.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return
    if any(origin.endswith(":4400") for origin in metadata.get("origins", [])):
        locator.unlink(missing_ok=True)


def _assert_alive(services: dict[str, ProcessTree]) -> None:
    for name, service in services.items():
        if service.process.poll() is not None:
            raise RuntimeError(
                f"{name} exited unexpectedly with code {service.process.returncode}"
            )


def _wait_for_emulators(services: dict[str, ProcessTree]) -> None:
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        _assert_alive(services)
        try:
            for port in (8080, 9099):
                with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                    pass
            return
        except OSError:
            time.sleep(0.25)
    raise TimeoutError("Auth and Firestore emulators were not ready within 90 seconds")


def _wait_for_url(
    url: str, services: dict[str, ProcessTree], timeout: float = 60
) -> None:
    deadline = time.monotonic() + timeout
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    while time.monotonic() < deadline:
        _assert_alive(services)
        try:
            with opener.open(url, timeout=1) as response:
                if response.status == 200:
                    return
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            pass
        time.sleep(0.25)
    raise TimeoutError(f"Service was not ready at {url} within {timeout} seconds")


def _run_command(
    command: list[str],
    cwd: Path,
    env: dict[str, str],
    services: dict[str, ProcessTree] | None = None,
    timeout: float = 900,
) -> int:
    owner = ProcessTree(command, cwd=cwd, env=env)
    try:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            _assert_alive(services or {})
            result = owner.process.poll()
            if result is not None:
                return result
            time.sleep(0.1)
        raise TimeoutError(
            f"{Path(command[0]).name} did not finish within {timeout} seconds"
        )
    finally:
        owner.stop()


def _preflight(groups: tuple[str, ...], env: dict[str, str]) -> None:
    for name in ("node", "poetry", "java"):
        _tool(name)
    _find_firebase()
    for entry in (VITE_ENTRY, CUCUMBER_ENTRY):
        if not entry.is_file():
            raise FileNotFoundError(
                f"Missing {entry}; install React and E2E npm dependencies first"
            )
    if BACKEND_GROUPS.intersection(groups):
        _tool("go")
    check = "const {chromium}=require('playwright');if(!require('node:fs').existsSync(chromium.executablePath()))process.exit(1)"
    if _run_command([_tool("node"), "-e", check], E2E_DIR, env):
        raise RuntimeError(
            "Playwright Chromium is missing; run npm run install:browsers in e2e/"
        )
    if _run_command(
        [_tool("poetry"), "run", "python", "-c", "import firebase_admin, click"],
        FIREBASE_SUB_DIR,
        env,
    ):
        raise RuntimeError(
            "Bootstrap dependencies are missing; run poetry install in firebase_sub/"
        )


def _discover(
    group: str,
    env: dict[str, str],
    artifacts: Path,
    name: str | None = None,
    application_time: bool = False,
) -> tuple[str, ...]:
    report = artifacts / "discovery.json"
    result = _run_command(
        [
            _tool("node"),
            str(CUCUMBER_ENTRY),
            "features/**/*.feature",
            "--tags",
            _scenario_tags(group, application_time),
            "--dry-run",
            "--format",
            f'"json":"{report.as_posix()}"',
            *(["--name", name] if name else []),
        ],
        E2E_DIR,
        env,
    )
    if result:
        raise RuntimeError(f"Cucumber discovery failed for {group}")
    features = json.loads(report.read_text(encoding="utf-8"))
    scenarios = []
    for feature in features:
        feature_path = feature["uri"].replace("\\", "/")
        scenarios.extend(
            f"{feature_path}:{element['line']}"
            for element in feature.get("elements", [])
            if element.get("keyword") in ("Scenario", "Scenario Outline")
        )
    if not scenarios:
        raise RuntimeError(f"No scenarios selected for mandatory group {group}")
    print(f"[Journey] {group}: discovered {len(scenarios)} scenarios", flush=True)
    return tuple(scenarios)


def _scenario_tags(group: str, application_time: bool) -> str:
    tags = GROUP_TAGS[group]
    if application_time:
        return f"({tags}) and @application_time"
    if group == "last-orders":
        return f"({tags}) and not @application_time and not @account_deletion"
    return tags


def _build_backend(
    scratch: Path, env: dict[str, str], application_time: bool = False
) -> Path:
    build_dir = scratch / "bin"
    build_dir.mkdir()
    binary = build_dir / ("last-orders.exe" if os.name == "nt" else "last-orders")
    print("[Journey] Building Last Orders", flush=True)
    if _run_command(
        [
            _tool("go"),
            "build",
            *(["-tags=e2e"] if application_time else []),
            "-o",
            str(binary),
            "./cmd/last-orders",
        ],
        LAST_ORDERS_DIR,
        env,
    ):
        raise RuntimeError("Last Orders build failed")
    return binary


def _polls_since(env: dict[str, str], application_time: bool = False) -> str:
    if not application_time:
        return datetime.now(UTC).date().isoformat()

    initial_time = env.get("LAST_ORDERS_E2E_INITIAL_TIME")
    if not initial_time:
        raise RuntimeError("Application-time journeys require an initial instant")
    try:
        parsed_initial_time = datetime.fromisoformat(
            initial_time.replace("Z", "+00:00")
        )
    except ValueError as error:
        raise RuntimeError("Application-time initial instant is invalid") from error
    if parsed_initial_time.tzinfo is None:
        raise RuntimeError("Application-time initial instant requires an offset")

    script = (
        'import { londonDate, shiftDate } from "./features/support/calendar.js"; '
        'console.log(shiftDate(londonDate(new Date(process.argv[1])), -1));'
    )
    result = subprocess.run(
        [
            _tool("node"),
            "--input-type=module",
            "-e",
            script,
            initial_time,
        ],
        cwd=E2E_DIR,
        env=env,
        capture_output=True,
        check=True,
        text=True,
    )
    polls_since = result.stdout.strip()
    try:
        datetime.strptime(polls_since, "%Y-%m-%d")
    except ValueError as error:
        raise RuntimeError("London processing floor is not a valid date") from error
    return polls_since


def _verify_backend_startup(
    path: Path,
    application_time: bool = False,
    initial_time: str | None = None,
    polls_since: str | None = None,
) -> None:
    lines = path.read_text(encoding="utf-8").splitlines(keepends=True)
    records = [
        json.loads(line) for line in lines if line.endswith("\n") and line.strip()
    ]
    expected = (
        {
            "msg": "firestore emulator enabled",
            "project_id": EMULATOR_PROJECT,
            "firestore_emulator_host": "127.0.0.1:8080",
        },
        {
            "msg": "dummy email client enabled; no Mailtrap or Sweego requests will be made"
        },
        {"msg": "push client selected", "client": "dummy"},
    )
    if application_time:
        expected += (
            {
                "msg": "e2e application clock enabled",
                "initial_instant": initial_time,
                "control_addr": CLOCK_CONTROL_ADDR,
            },
            {"msg": "listener scope resolved", "polls_since": polls_since},
        )
    for evidence in expected:
        if not any(
            all(record.get(key) == value for key, value in evidence.items())
            for record in records
        ):
            raise RuntimeError("Missing backend safety evidence: " + str(evidence))


def _stop_services(services: dict[str, ProcessTree], ports: tuple[int, ...]) -> None:
    errors = []
    for name, service in reversed(list(services.items())):
        try:
            service.stop()
        except (OSError, subprocess.TimeoutExpired) as error:
            errors.append(f"{name}: {error}")
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        try:
            _require_free_ports(ports)
            break
        except RuntimeError:
            time.sleep(0.1)
    else:
        errors.append(
            "owned service ports were not released; no unrelated process was terminated"
        )
    if errors:
        raise RuntimeError("Cleanup failed: " + "; ".join(errors))


def _print_backend_diagnostics(artifacts: Path) -> None:
    for filename in ("last-orders.stderr.log", "last-orders.jsonl"):
        path = artifacts / filename
        if path.is_file():
            print(f"[Journey] Last records from {path}:", file=sys.stderr)
            for line in path.read_text(encoding="utf-8", errors="replace").splitlines()[
                -10:
            ]:
                print(line[:2000], file=sys.stderr)


def _remove_scratch(path: Path) -> None:
    deadline = time.monotonic() + 15
    while True:
        try:
            shutil.rmtree(path)
            return
        except FileNotFoundError:
            return
        except PermissionError:
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.1)


@contextmanager
def _scratch_directory() -> Iterator[Path]:
    path = Path(tempfile.mkdtemp(prefix="pubnightpicker-journey-"))
    try:
        yield path
    finally:
        _remove_scratch(path)


def _run_group(
    group: str,
    binary: Path | None,
    scratch: Path,
    artifacts: Path,
    name: str | None = None,
    scenario: str | None = None,
    application_time: bool = False,
) -> int:
    backend_ports = (8081, 8082) if application_time else (8081,)
    ports = (*EMULATOR_PORTS, 3000, *(backend_ports if group in BACKEND_GROUPS else ()))
    _require_free_ports(ports)
    _remove_stale_hub_locator()
    env = _environment(application_time)
    _validate_environment(env, application_time)
    polls_since = _polls_since(env, application_time)
    services: dict[str, ProcessTree] = {}
    print(f"[Journey] Starting fresh {group} group ({EMULATOR_PROJECT})", flush=True)
    with ExitStack() as stack:
        files = stack.enter_context(ExitStack())
        stack.callback(_stop_services, services, ports)
        services["Firebase emulators"] = ProcessTree(
            [
                *_find_firebase(),
                "emulators:start",
                "--config",
                str(REACT_DIR / "firebase.e2e.json"),
                "--only",
                "auth,firestore",
                "--project",
                EMULATOR_PROJECT,
            ],
            cwd=REACT_DIR,
            env=env,
        )
        _wait_for_emulators(services)
        if _run_command(
            [
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
            ],
            FIREBASE_SUB_DIR,
            env,
            services,
        ):
            raise RuntimeError("Emulator admin bootstrap failed")
        if group in BACKEND_GROUPS:
            if binary is None:
                raise RuntimeError("Backend group requires a built Last Orders binary")
            backend_env = {**env, "LAST_ORDERS_CURSOR_HMAC_KEY": secrets.token_hex(32)}
            _validate_environment(backend_env, application_time)
            backend_args = [
                str(binary),
                "all",
                f"-db-path={scratch / 'last-orders.db'}",
                "-http-addr=127.0.0.1:8081",
                "-push-client=dummy",
                "-allow-auth-emulator",
                f"-polls-since={polls_since}",
            ]
            if group == "account-deletion":
                backend_env.update(
                    {
                        "ENABLE_ADMIN_DELETE_REQUESTS": "true",
                        "ADMIN_DELETE_DRY_RUN": "false",
                    }
                )
                backend_args.append("-enable-real-auth-delete")
            stdout_path = artifacts / "last-orders.jsonl"
            stdout = files.enter_context(stdout_path.open("w", encoding="utf-8"))
            stderr = files.enter_context(
                (artifacts / "last-orders.stderr.log").open("w", encoding="utf-8")
            )
            services["Last Orders"] = ProcessTree(
                backend_args,
                cwd=LAST_ORDERS_DIR,
                env=backend_env,
                stdout=stdout,
                stderr=stderr,
            )
            _wait_for_url(f"{BACKEND_URL}/healthz", services)
            if application_time:
                _wait_for_url(f"{CLOCK_CONTROL_URL}/clock", services)
            _verify_backend_startup(
                stdout_path,
                application_time,
                env.get("LAST_ORDERS_E2E_INITIAL_TIME"),
                polls_since,
            )
            print(
                "[Journey] Verified demo emulator and dummy delivery selection",
                flush=True,
            )
        services["Vite"] = ProcessTree(
            [
                _tool("node"),
                str(VITE_ENTRY),
                "--config",
                str(REACT_DIR / "vite.e2e.config.js"),
                "--host",
                "127.0.0.1",
                "--port",
                "3000",
                "--strictPort",
            ],
            cwd=REACT_DIR,
            env=env,
        )
        _wait_for_url(BASE_URL, services)
        cucumber_env = env
        if group in BACKEND_GROUPS:
            cucumber_env = {
                **env,
                "E2E_BACKEND_LOG_PATH": str(artifacts / "last-orders.jsonl"),
                **(
                    {"LAST_ORDERS_E2E_POLLS_SINCE": polls_since}
                    if application_time
                    else {}
                ),
            }
        return _run_command(
            [
                _tool("node"),
                str(CUCUMBER_ENTRY),
                scenario or "features/**/*.feature",
                "--tags",
                _scenario_tags(group, application_time),
                "--format",
                f'"json":"{(artifacts / "cucumber.json").as_posix()}"',
                *(["--name", name] if name else []),
            ],
            E2E_DIR,
            cucumber_env,
            services,
            timeout=1200,
        )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--group", choices=("default", "all", *GROUP_TAGS), default="default"
    )
    parser.add_argument(
        "--name", help="Run only Cucumber scenarios matching this name expression"
    )
    parser.add_argument(
        "--lifecycle",
        choices=("group", "scenario"),
        default=None,
        help="Use shared group services or fresh services for each selected backend scenario",
    )
    parser.add_argument(
        "--application-time",
        action="store_true",
        help="Run only @application_time Last Orders scenarios with a tagged backend and fresh state per scenario",
    )
    args = parser.parse_args(argv)
    if args.lifecycle == "scenario" and args.group not in BACKEND_GROUPS:
        parser.error("--lifecycle scenario requires a backend-dependent group")
    if args.application_time and args.group != "last-orders":
        parser.error("--application-time requires --group last-orders")
    lifecycle = args.lifecycle or ("scenario" if args.application_time else "group")
    if args.application_time and lifecycle != "scenario":
        parser.error("--application-time requires the scenario lifecycle")
    if args.group == "default":
        groups = DEFAULT_GROUPS
    elif args.group == "all":
        groups = tuple(GROUP_TAGS)
    else:
        groups = (args.group,)
    env = _environment(args.application_time)
    _validate_environment(env, args.application_time)
    _preflight(groups, env)
    run_id = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ") + "-" + secrets.token_hex(3)
    artifacts = E2E_DIR / "artifacts" / run_id
    artifacts.mkdir(parents=True)
    print(f"[Journey] Artifacts: {artifacts}", flush=True)
    with _scratch_directory() as scratch:
        binary = (
            _build_backend(scratch, env, args.application_time)
            if BACKEND_GROUPS.intersection(groups)
            else None
        )
        discovered = {}
        for group in groups:
            group_artifacts = artifacts / group
            group_artifacts.mkdir()
            discovered[group] = _discover(
                group, env, group_artifacts, args.name, args.application_time
            )
        result = 0
        for group in groups:
            group_scratch = scratch / group
            group_scratch.mkdir()
            if lifecycle == "scenario":
                for index, scenario in enumerate(discovered[group], start=1):
                    scenario_scratch = group_scratch / f"scenario-{index:03d}"
                    scenario_scratch.mkdir()
                    scenario_artifacts = artifacts / group / f"scenario-{index:03d}"
                    scenario_artifacts.mkdir()
                    try:
                        scenario_result = _run_group(
                            group,
                            binary,
                            scenario_scratch,
                            scenario_artifacts,
                            args.name,
                            scenario,
                            args.application_time,
                        )
                    except BaseException:
                        _print_backend_diagnostics(scenario_artifacts)
                        raise
                    finally:
                        _remove_scratch(scenario_scratch)
                    if scenario_result:
                        _print_backend_diagnostics(scenario_artifacts)
                    result = result or scenario_result
            else:
                try:
                    group_result = _run_group(
                        group,
                        binary,
                        group_scratch,
                        artifacts / group,
                        args.name,
                        application_time=args.application_time,
                    )
                except BaseException:
                    _print_backend_diagnostics(artifacts / group)
                    raise
                if group_result:
                    _print_backend_diagnostics(artifacts / group)
                result = result or group_result
        return result


def _interrupt(signal_number: int, frame: object) -> None:
    raise KeyboardInterrupt


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, _interrupt)
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print(
            "[Journey] Interrupted; owned services have been stopped", file=sys.stderr
        )
        sys.exit(130)
    except (
        OSError,
        RuntimeError,
        TimeoutError,
        ValueError,
        subprocess.SubprocessError,
    ) as error:
        print(f"[Journey] ERROR: {error}", file=sys.stderr)
        if error.__context__ is not None:
            print(f"[Journey] Original error: {error.__context__}", file=sys.stderr)
        sys.exit(1)
