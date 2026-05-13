import os
import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import click
import firebase_admin
import google.oauth2.credentials
from firebase_admin import credentials, firestore

from firebase_sub.common.logging import configure_logging, log_level_to_int

CWD = Path(__file__).resolve().parent
CRED_PATH = Path(os.getenv("FIREBASE_CRED_PATH", Path.cwd() / "cred.json"))
_DEFAULT_EMULATOR_PROJECT_ID = "demo-firebase-sub-integration"
_FIREBASE_APP_INITIALIZED = False
_DB = None


def _env_flag(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def _project_id_from_cred_file() -> str | None:
    try:
        payload = json.loads(CRED_PATH.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    value = str(payload.get("project_id", "")).strip()
    return value or None


def _resolve_project_id() -> str:
    for env_key in (
        "GOOGLE_CLOUD_PROJECT",
        "GCLOUD_PROJECT",
        "FIREBASE_PROJECT",
        "REACT_APP_FIREBASE_PROJECT_ID",
    ):
        value = os.getenv(env_key, "").strip()
        if value:
            return value
    return _project_id_from_cred_file() or _DEFAULT_EMULATOR_PROJECT_ID


def _in_emulator_mode() -> bool:
    return bool(os.getenv("FIRESTORE_EMULATOR_HOST"))


def _ensure_firebase_app() -> None:
    global _FIREBASE_APP_INITIALIZED
    if _FIREBASE_APP_INITIALIZED:
        return

    try:
        firebase_admin.get_app()
    except ValueError:
        if _in_emulator_mode():
            project_id = _resolve_project_id()
            firebase_admin.initialize_app(
                credential=google.oauth2.credentials.Credentials(token="owner"),
                options={"projectId": project_id},
            )
        else:
            firebase_admin.initialize_app(credentials.Certificate(str(CRED_PATH)))

    _FIREBASE_APP_INITIALIZED = True


def _get_db():
    global _DB
    if _DB is None:
        _ensure_firebase_app()
        _DB = firestore.client()
    return _DB


def _format_outcomes(outcomes: dict[str, Any] | None, *, include_zeroes: bool) -> list[str]:
    if not outcomes:
        return []
    rendered: list[str] = []
    for key in sorted(outcomes):
        value = outcomes.get(key)
        if value in (None, 0) and not include_zeroes:
            continue
        rendered.append(f"  - {key}: {value}")
    return rendered


def _print_metric_doc(label: str, payload: dict[str, Any] | None, *, include_zeroes: bool) -> None:
    click.echo(label)
    if not payload:
        click.echo("  (missing)")
        return

    click.echo(f"  total: {payload.get('total', 0)}")
    click.echo(f"  lastOutcome: {payload.get('lastOutcome', '-')}")
    click.echo(f"  lastRequestId: {payload.get('lastRequestId', '-')}")
    click.echo(f"  updatedAt: {payload.get('updatedAt', '-')}")

    outcome_lines = _format_outcomes(
        payload.get("outcomes") if isinstance(payload.get("outcomes"), dict) else None,
        include_zeroes=include_zeroes,
    )
    click.echo("  outcomes:")
    if outcome_lines:
        for line in outcome_lines:
            click.echo(line)
    else:
        click.echo("  (none)")


def _metric_count(payload: dict[str, Any] | None, key: str) -> int:
    if not payload:
        return 0
    outcomes = payload.get("outcomes")
    if not isinstance(outcomes, dict):
        return 0
    value = outcomes.get(key)
    if isinstance(value, (int, float)):
        return int(value)
    return 0


def _print_preflight(
    *,
    db,
    day: str,
    enable_real_auth_delete: bool,
    global_doc: dict[str, Any] | None,
    daily_doc: dict[str, Any] | None,
) -> None:
    env_gate_enabled = _env_flag("ENABLE_ADMIN_DELETE_REQUESTS", default=False)
    effective_real_delete = env_gate_enabled and enable_real_auth_delete

    kill_switch = db.collection("system_config").document("admin_delete").get().to_dict() or {}
    paused = bool(kill_switch.get("paused", False))
    pause_reason = kill_switch.get("reason", "")

    click.echo("Admin delete preflight")
    click.echo(f"  env gate ENABLE_ADMIN_DELETE_REQUESTS: {'on' if env_gate_enabled else 'off'}")
    click.echo(f"  cli gate --enable-real-auth-delete: {'on' if enable_real_auth_delete else 'off'}")
    click.echo(f"  effective real auth delete: {'on' if effective_real_delete else 'off'}")
    click.echo(f"  kill-switch paused: {'yes' if paused else 'no'}")
    if pause_reason:
        click.echo(f"  kill-switch reason: {pause_reason}")

    click.echo("  outcome counters:")
    click.echo(
        "    global auth_delete_failed/auth_delete_blocked: "
        f"{_metric_count(global_doc, 'auth_delete_failed')}/{_metric_count(global_doc, 'auth_delete_blocked')}"
    )
    click.echo(
        f"    daily-{day} auth_delete_failed/auth_delete_blocked: "
        f"{_metric_count(daily_doc, 'auth_delete_failed')}/{_metric_count(daily_doc, 'auth_delete_blocked')}"
    )


def _preflight_exit_code(*, db, enable_real_auth_delete: bool) -> int:
    env_gate_enabled = _env_flag("ENABLE_ADMIN_DELETE_REQUESTS", default=False)
    effective_real_delete = env_gate_enabled and enable_real_auth_delete

    kill_switch = db.collection("system_config").document("admin_delete").get().to_dict() or {}
    paused = bool(kill_switch.get("paused", False))

    if not effective_real_delete:
        return 2
    if paused:
        return 3
    return 0


@click.command()
@click.option("--day", type=str, default=None, help="Day in YYYY-MM-DD (defaults to today UTC)")
@click.option(
    "--include-zeroes/--no-include-zeroes",
    default=False,
    show_default=True,
    help="Include outcomes with zero values in output",
)
@click.option(
    "--preflight",
    is_flag=True,
    default=False,
    help="Print dual-gate, kill-switch, and failure/block counters for on-call readiness",
)
@click.option(
    "--enable-real-auth-delete/--no-enable-real-auth-delete",
    default=False,
    show_default=True,
    help="Evaluate preflight as if sub_events were started with this CLI gate",
)
@click.option("--loglevel", default="INFO", type=log_level_to_int, help="Set the log level")
def cli(
    day: str | None,
    include_zeroes: bool,
    preflight: bool,
    enable_real_auth_delete: bool,
    loglevel: int,
) -> None:
    """Print admin delete metrics from Firestore for global and daily docs."""
    configure_logging(loglevel, None)

    if day is None:
        day = datetime.now(UTC).date().isoformat()

    db = _get_db()
    metrics = db.collection("admin_delete_request_metrics")

    global_doc = metrics.document("global").get().to_dict()
    daily_doc = metrics.document(f"daily-{day}").get().to_dict()

    if preflight:
        _print_preflight(
            db=db,
            day=day,
            enable_real_auth_delete=enable_real_auth_delete,
            global_doc=global_doc,
            daily_doc=daily_doc,
        )
        exit_code = _preflight_exit_code(
            db=db,
            enable_real_auth_delete=enable_real_auth_delete,
        )
        if exit_code != 0:
            raise SystemExit(exit_code)
        click.echo("")

    click.echo(f"Admin delete metrics (day={day})")
    _print_metric_doc("global", global_doc, include_zeroes=include_zeroes)
    _print_metric_doc(f"daily-{day}", daily_doc, include_zeroes=include_zeroes)


if __name__ == "__main__":
    cli()
