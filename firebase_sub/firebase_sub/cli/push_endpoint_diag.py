"""Diagnose why a user's push endpoints are visible to some push paths but not others.

Compares the collection-group query used by poll open/complete pushes against the
per-user subcollection scan used by the diagnostic push, and reports any endpoint
that one path sees and the other does not.
"""

import logging
import os
from pathlib import Path
from typing import Any

import click
import firebase_admin
from firebase_admin import credentials, firestore
from google.cloud.firestore_v1.base_query import FieldFilter

from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.push_contract import PUSH_PREFERENCE_DEFAULTS

_log = logging.getLogger(__name__)

CWD = Path(__file__).resolve().parent


def _resolve_cred_path() -> Path:
    env_path = os.getenv("FIREBASE_CRED_PATH")
    if env_path:
        return Path(env_path)
    cwd_path = Path.cwd() / "cred.json"
    if cwd_path.exists():
        return cwd_path
    source_tree_path = CWD.parent.parent / "cred.json"
    if source_tree_path.exists():
        return source_tree_path
    return cwd_path


def _get_db() -> Any:
    try:
        firebase_admin.get_app()
    except ValueError:
        firebase_admin.initialize_app(credentials.Certificate(_resolve_cred_path()))
    return firestore.client()


def _endpoint_label(endpoint: str) -> str:
    if len(endpoint) <= 72:
        return endpoint
    return f"{endpoint[:60]}...{endpoint[-12:]}"


@click.command()
@click.option(
    "--loglevel",
    default="INFO",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
)
@click.option("--logfile", type=click.Path(path_type=Path), help="Log file path")
@click.option("--uid", required=True, help="Auth uid to diagnose")
def main(loglevel: int, logfile: Path | None, uid: str) -> None:
    configure_logging(loglevel, logfile)
    db = _get_db()

    user_payload = db.collection("users").document(uid).get().to_dict() or {}
    push_prefs = user_payload.get("pushPreferences") or {}
    click.echo(f"uid: {uid}")
    click.echo(
        "webPushEnabled: "
        f"{user_payload.get('webPushEnabled')!r} "
        f"(present={'webPushEnabled' in user_payload})"
    )
    click.echo(f"pushPreferences: {push_prefs!r}")
    for field, default in PUSH_PREFERENCE_DEFAULTS.items():
        click.echo(
            f"  effective {field}: {bool(push_prefs.get(field, default))} "
            f"(raw={push_prefs.get(field)!r})"
        )

    click.echo("\n--- Per-user subcollection scan (diagnostic push path) ---")
    subcollection_active: set[str] = set()
    for snap in (
        db.collection("users").document(uid).collection("push_endpoints").stream()
    ):
        payload = snap.to_dict() or {}
        raw_active = payload.get("active")
        is_active = bool(raw_active)
        if is_active:
            subcollection_active.add(snap.reference.path)
        click.echo(
            f"{snap.reference.path}\n"
            f"    active={raw_active!r} (type={type(raw_active).__name__}, "
            f"python_truthy={is_active})\n"
            f"    endpoint={_endpoint_label(str(payload.get('endpoint', '')))}\n"
            f"    keys_present=p256dh:{payload.get('p256dh') is not None} "
            f"auth:{payload.get('auth') is not None}\n"
            f"    createdAt={payload.get('createdAt')!r} "
            f"lastSeenAt={payload.get('lastSeenAt')!r}"
        )

    click.echo("\n--- Collection-group query active == True (poll push path) ---")
    group_active: set[str] = set()
    all_group_paths: list[str] = []
    query = db.collection_group("push_endpoints").where(
        filter=FieldFilter("active", "==", True)
    )
    for snap in query.stream():
        all_group_paths.append(snap.reference.path)
        parts = snap.reference.path.split("/")
        if len(parts) >= 4 and parts[-3] == uid:
            group_active.add(snap.reference.path)
    click.echo(f"matched {len(group_active)} endpoint(s) for this uid")
    for path in sorted(group_active):
        click.echo(f"    {path}")

    missing = subcollection_active - group_active
    extra = group_active - subcollection_active
    click.echo("\n--- Divergence for this uid ---")
    if not missing and not extra:
        click.echo("None: both paths agree. Problem lies elsewhere (payload/client).")
    for path in sorted(missing):
        click.echo(
            f"NOT VISIBLE to poll pushes (indexed as active!=True or unindexed): {path}"
        )
    for path in sorted(extra):
        click.echo(f"Visible only to the collection-group query: {path}")

    click.echo("\n--- System-wide accounting (explains the delivered=N figure) ---")
    unfiltered_total = sum(1 for _ in db.collection_group("push_endpoints").stream())
    click.echo(f"all push_endpoints docs (no filter):   {unfiltered_total}")
    click.echo(f"collection-group active == True:       {len(all_group_paths)}")
    if unfiltered_total != len(all_group_paths):
        click.echo(
            "  ^ difference = inactive endpoints, OR endpoints whose 'active' is "
            "true in the document but not matched by the index."
        )

    per_user: dict[str, list[str]] = {}
    for path in all_group_paths:
        parts = path.split("/")
        if len(parts) >= 4 and parts[-2] == "push_endpoints":
            per_user.setdefault(parts[-3], []).append(path)

    click.echo("\nper-user breakdown of indexed-active endpoints:")
    gated_out = 0
    for owner_uid, paths in sorted(per_user.items()):
        owner = db.collection("users").document(owner_uid).get().to_dict() or {}
        prefs = owner.get("pushPreferences") or {}
        has_master = "webPushEnabled" in owner
        master_ok = has_master and bool(owner.get("webPushEnabled"))
        opens = bool(prefs.get("pollOpens", PUSH_PREFERENCE_DEFAULTS["pollOpens"]))
        completes = bool(
            prefs.get("pollCompletes", PUSH_PREFERENCE_DEFAULTS["pollCompletes"])
        )
        verdict = "SENT" if master_ok and completes else "SKIPPED"
        if verdict == "SKIPPED":
            gated_out += len(paths)
        reason = ""
        if not has_master:
            reason = " (webPushEnabled missing)"
        elif not master_ok:
            reason = " (webPushEnabled false)"
        elif not completes:
            reason = " (pushPreferences.pollCompletes false)"
        click.echo(
            f"  {owner_uid}: endpoints={len(paths)} pollOpens={opens} "
            f"pollCompletes={completes} -> {verdict}{reason}"
        )

    click.echo(
        f"\nexpected poll-complete delivered count: "
        f"{len(all_group_paths) - gated_out}"
    )


if __name__ == "__main__":
    main()
