import logging
import os
from collections.abc import Iterable
from pathlib import Path
from typing import Any

import click
import firebase_admin
from firebase_admin import credentials, firestore

from firebase_sub.common.logging import configure_logging, log_level_to_int

_log = logging.getLogger(__name__)
PROFILE_FIELDS = ("name", "photoUrl", "votesVisible")
LEGACY_PROFILE_FIELDS = PROFILE_FIELDS
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


CRED_PATH = _resolve_cred_path()
_FIREBASE_APP_INITIALIZED = False
_DB = None


def _ensure_firebase_app() -> None:
    global _FIREBASE_APP_INITIALIZED
    if _FIREBASE_APP_INITIALIZED:
        return
    try:
        firebase_admin.get_app()
    except ValueError:
        firebase_admin.initialize_app(credentials.Certificate(CRED_PATH))
    _FIREBASE_APP_INITIALIZED = True


def _get_db():
    global _DB
    if _DB is None:
        _ensure_firebase_app()
        _DB = firestore.client()
    return _DB


def _has_value(data: dict[str, Any], field: str) -> bool:
    value = data.get(field)
    return value is not None and value != ""


def _render_votes_visible(data: dict[str, Any]) -> str:
    if "votesVisible" not in data:
        return "missing"
    value = data["votesVisible"]
    if isinstance(value, bool):
        return str(value).lower()
    if value is None:
        return "null"
    return f"unexpected-type:{type(value).__name__}"


def _audit_user_profiles(
    users: Iterable[Any], public_users: Iterable[Any]
) -> dict[str, Any]:
    users_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in users}
    public_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in public_users}
    issues: dict[str, list[str]] = {}
    votes_visible_details: dict[str, str] = {}

    def add_issue(uid: str, issue: str) -> None:
        issues.setdefault(uid, []).append(issue)

    for uid in sorted(users_by_id.keys() | public_by_id.keys()):
        private_data = users_by_id.get(uid)
        public_data = public_by_id.get(uid)

        if private_data is None:
            add_issue(uid, "orphan_user_public")
            continue

        if private_data.get("uid") != uid:
            add_issue(uid, "users_uid_missing_or_mismatched")

        if public_data is None:
            add_issue(uid, "missing_user_public_document")
            public_data = {}
        elif public_data.get("uid") != uid:
            add_issue(uid, "user_public_uid_missing_or_mismatched")

        for field in PROFILE_FIELDS:
            if _has_value(private_data, field) and not _has_value(public_data, field):
                add_issue(uid, f"missing_public_field:{field}")
            elif _has_value(private_data, field) and _has_value(public_data, field):
                if private_data[field] != public_data[field]:
                    add_issue(uid, f"value_differs:{field}")

        user_issues = issues.get(uid, [])
        if any(
            issue
            in {
                "missing_public_field:votesVisible",
                "value_differs:votesVisible",
            }
            for issue in user_issues
        ):
            votes_visible_details[uid] = (
                f"users={_render_votes_visible(private_data)}, "
                f"user-public={_render_votes_visible(public_data)}"
            )

    counts = {
        "users_scanned": len(users_by_id),
        "user_public_documents_scanned": len(public_by_id),
        "missing_user_public_documents": 0,
        "orphan_user_public_documents": 0,
        "users_uid_missing_or_mismatched": 0,
        "user_public_uid_missing_or_mismatched": 0,
        "missing_public_fields": {field: 0 for field in PROFILE_FIELDS},
        "differing_values": {field: 0 for field in PROFILE_FIELDS},
    }
    for user_issues in issues.values():
        for issue in user_issues:
            if issue == "missing_user_public_document":
                counts["missing_user_public_documents"] += 1
            elif issue == "orphan_user_public":
                counts["orphan_user_public_documents"] += 1
            elif issue == "users_uid_missing_or_mismatched":
                counts["users_uid_missing_or_mismatched"] += 1
            elif issue == "user_public_uid_missing_or_mismatched":
                counts["user_public_uid_missing_or_mismatched"] += 1
            elif issue.startswith("missing_public_field:"):
                field = issue.removeprefix("missing_public_field:")
                counts["missing_public_fields"][field] += 1
            elif issue.startswith("value_differs:"):
                field = issue.removeprefix("value_differs:")
                counts["differing_values"][field] += 1

    return {
        "counts": counts,
        "issues": issues,
        "votes_visible_details": votes_visible_details,
    }


def _plan_votes_visible_migration(
    users: Iterable[Any], public_users: Iterable[Any]
) -> tuple[list[dict[str, Any]], list[dict[str, str]]]:
    users_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in users}
    public_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in public_users}
    operations: list[dict[str, Any]] = []
    skipped: list[dict[str, str]] = []

    for uid, private_data in users_by_id.items():
        private_value = private_data.get("votesVisible")
        if not isinstance(private_value, bool):
            continue

        public_data = public_by_id.get(uid, {})
        public_value = public_data.get("votesVisible")
        if not _has_value(public_data, "votesVisible"):
            operations.append(
                {
                    "uid": uid,
                    "value": private_value,
                    "reason": "copy private value to missing public field",
                    "write": True,
                    "create_document": uid not in public_by_id,
                }
            )
        elif not isinstance(public_value, bool):
            skipped.append(
                {
                    "uid": uid,
                    "reason": f"unexpected public type: {type(public_value).__name__}",
                }
            )
        elif private_value != public_value:
            target_value = private_value or public_value
            operations.append(
                {
                    "uid": uid,
                    "value": target_value,
                    "reason": "true wins conflict",
                    "write": target_value != public_value,
                    "create_document": False,
                }
            )

    return operations, skipped


def _print_migration_plan(
    operations: list[dict[str, Any]],
    skipped: list[dict[str, str]],
    *,
    details: bool,
) -> None:
    write_count = sum(operation["write"] for operation in operations)
    click.echo("votesVisible migration plan (dry-run unless --apply is supplied)")
    click.echo(f"  writes planned: {write_count}")
    click.echo(f"  already matches policy: {len(operations) - write_count}")
    click.echo(f"  skipped for review: {len(skipped)}")

    if details:
        for operation in operations:
            action = "write" if operation["write"] else "no change"
            value = str(operation["value"]).lower()
            click.echo(
                f"  {operation['uid']}: {action} votesVisible={value} "
                f"({operation['reason']})"
            )
        for item in skipped:
            click.echo(f"  {item['uid']}: skipped ({item['reason']})")


def _apply_votes_visible_migration(db: Any, operations: list[dict[str, Any]]) -> int:
    writes = [operation for operation in operations if operation["write"]]
    if not writes:
        return 0
    if len(writes) > 500:
        raise click.ClickException(
            f"Refusing to apply {len(writes)} writes in one batch; maximum is 500."
        )

    batch = db.batch()
    public_collection = db.collection("user-public")
    for operation in writes:
        payload = {"votesVisible": operation["value"]}
        if operation["create_document"]:
            payload["uid"] = operation["uid"]
        batch.set(
            public_collection.document(operation["uid"]),
            payload,
            merge=True,
        )
    batch.commit()
    return len(writes)


def _plan_legacy_profile_cleanup(
    users: Iterable[Any],
) -> list[dict[str, Any]]:
    operations = []
    for snapshot in users:
        data = snapshot.to_dict() or {}
        fields = [field for field in LEGACY_PROFILE_FIELDS if field in data]
        if fields:
            operations.append({"uid": snapshot.id, "fields": fields})
    return operations


def _print_legacy_profile_cleanup_plan(
    operations: list[dict[str, Any]], *, details: bool
) -> None:
    field_count = sum(len(operation["fields"]) for operation in operations)
    click.echo(
        "legacy users profile-field cleanup (dry-run unless --apply is supplied)"
    )
    click.echo(f"  documents with fields to delete: {len(operations)}")
    click.echo(f"  fields to delete: {field_count}")
    for field in LEGACY_PROFILE_FIELDS:
        count = sum(field in operation["fields"] for operation in operations)
        click.echo(f"    {field}: {count}")

    if details:
        for operation in operations:
            click.echo(f"  {operation['uid']}: delete {', '.join(operation['fields'])}")


def _apply_legacy_profile_cleanup(db: Any, operations: list[dict[str, Any]]) -> int:
    for start in range(0, len(operations), 500):
        batch = db.batch()
        users_collection = db.collection("users")
        for operation in operations[start : start + 500]:
            batch.update(
                users_collection.document(operation["uid"]),
                {field: firestore.DELETE_FIELD for field in operation["fields"]},
            )
        batch.commit()
    return sum(len(operation["fields"]) for operation in operations)


def _print_report(report: dict[str, Any], *, details: bool) -> None:
    counts = report["counts"]
    click.echo("User profile audit (read-only)")
    click.echo(f"  users scanned: {counts['users_scanned']}")
    click.echo(
        "  user-public documents scanned: " f"{counts['user_public_documents_scanned']}"
    )
    click.echo(
        "  missing user-public documents: " f"{counts['missing_user_public_documents']}"
    )
    click.echo(
        "  orphan user-public documents: " f"{counts['orphan_user_public_documents']}"
    )
    click.echo(
        "  uid field missing/mismatched: "
        f"users={counts['users_uid_missing_or_mismatched']}, "
        f"user-public={counts['user_public_uid_missing_or_mismatched']}"
    )
    click.echo("  private values missing from user-public:")
    for field, count in counts["missing_public_fields"].items():
        click.echo(f"    {field}: {count}")
    click.echo("  differing private/public values (review; may be intentional):")
    for field, count in counts["differing_values"].items():
        click.echo(f"    {field}: {count}")

    if details:
        click.echo("  document findings:")
        for uid, user_issues in sorted(report["issues"].items()):
            click.echo(f"    {uid}: {', '.join(sorted(user_issues))}")
            votes_detail = report["votes_visible_details"].get(uid)
            if votes_detail:
                click.echo(f"      votesVisible: {votes_detail}")


@click.command()
@click.option(
    "--details",
    is_flag=True,
    help="Include document IDs and issue labels in the report.",
)
@click.option(
    "--migrate-votes-visible",
    is_flag=True,
    help="Plan to backfill missing votesVisible and make true win conflicts.",
)
@click.option(
    "--delete-legacy-profile-fields",
    is_flag=True,
    help="Plan to delete name, photoUrl, and votesVisible from users.",
)
@click.option(
    "--apply",
    is_flag=True,
    help="Commit the selected migration or cleanup plan.",
)
@click.option(
    "--loglevel",
    default="INFO",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
    help="Set the log level (numeric or name)",
)
def main(
    details: bool,
    migrate_votes_visible: bool,
    delete_legacy_profile_fields: bool,
    apply: bool,
    loglevel: int,
) -> None:
    """Audit profile overlap and optionally migrate votesVisible."""
    if migrate_votes_visible and delete_legacy_profile_fields:
        raise click.UsageError(
            "Choose either --migrate-votes-visible or --delete-legacy-profile-fields."
        )
    if apply and not (migrate_votes_visible or delete_legacy_profile_fields):
        raise click.UsageError("--apply requires a migration or cleanup option.")

    configure_logging(loglevel, None)
    db = _get_db()
    user_snapshots = list(db.collection("users").stream())
    public_snapshots = list(db.collection("user-public").stream())
    report = _audit_user_profiles(
        user_snapshots,
        public_snapshots,
    )
    _print_report(report, details=details)

    if migrate_votes_visible:
        operations, skipped = _plan_votes_visible_migration(
            user_snapshots,
            public_snapshots,
        )
        _print_migration_plan(operations, skipped, details=details)
        if apply:
            if skipped:
                raise click.ClickException(
                    "Migration contains values needing review; no writes were applied."
                )
            if report["counts"]["user_public_uid_missing_or_mismatched"]:
                raise click.ClickException(
                    "user-public uid fields are inconsistent; no writes were applied."
                )
            written = _apply_votes_visible_migration(db, operations)
            click.echo(f"  writes applied: {written}")

    if delete_legacy_profile_fields:
        operations = _plan_legacy_profile_cleanup(user_snapshots)
        _print_legacy_profile_cleanup_plan(operations, details=details)
        if apply:
            if report["counts"]["missing_user_public_documents"]:
                raise click.ClickException(
                    "Some users lack user-public documents; no fields were deleted."
                )
            missing_public_fields = report["counts"]["missing_public_fields"]
            if any(missing_public_fields.values()):
                raise click.ClickException(
                    "Some private profile values are not present in user-public; "
                    "no fields were deleted."
                )
            deleted = _apply_legacy_profile_cleanup(db, operations)
            click.echo(f"  fields deleted: {deleted}")

    _log.info("Completed user profile audit")


if __name__ == "__main__":
    main()
