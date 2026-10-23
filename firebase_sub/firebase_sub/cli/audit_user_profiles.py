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
PROFILE_FIELDS = ("name", "photoUrl", "votesVisible", "customPhotoUrl")
LEGACY_PROFILE_FIELDS = ("uid", *PROFILE_FIELDS)
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
    return _render_boolean_field(data, "votesVisible")


def _render_boolean_field(data: dict[str, Any], field: str) -> str:
    if field not in data:
        return "missing"
    value = data[field]
    if isinstance(value, bool):
        return str(value).lower()
    if value is None:
        return "null"
    return f"unexpected-type:{type(value).__name__}"


def _render_string_presence(data: dict[str, Any], field: str) -> str:
    if field not in data:
        return "missing"
    value = data[field]
    if isinstance(value, str):
        return "set" if value else "empty"
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
    custom_photo_details: dict[str, str] = {}

    def add_issue(uid: str, issue: str) -> None:
        issues.setdefault(uid, []).append(issue)

    for uid in sorted(users_by_id.keys() | public_by_id.keys()):
        private_data = users_by_id.get(uid)
        public_data = public_by_id.get(uid)

        if private_data is None:
            add_issue(uid, "orphan_user_public")
            continue

        if "uid" in private_data and private_data["uid"] != uid:
            add_issue(uid, "users_uid_missing_or_mismatched")

        if public_data is None:
            add_issue(uid, "missing_user_public_document")
            public_data = {}
        elif "uid" in public_data and public_data["uid"] != uid:
            add_issue(uid, "user_public_uid_missing_or_mismatched")

        for field in PROFILE_FIELDS:
            if (
                field == "customPhotoUrl"
                and private_data.get(field) is False
                and (public_data.get(field) in (None, "") or field not in public_data)
            ):
                continue
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
        if any(
            issue
            in {
                "missing_public_field:customPhotoUrl",
                "value_differs:customPhotoUrl",
            }
            for issue in user_issues
        ):
            custom_photo_details[uid] = (
                f"users={_render_string_presence(private_data, 'customPhotoUrl')}, "
                f"user-public={_render_string_presence(public_data, 'customPhotoUrl')}"
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
        "custom_photo_details": custom_photo_details,
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
                    "reason": "fill missing or empty public field",
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


def _plan_custom_photo_migration(
    users: Iterable[Any], public_users: Iterable[Any]
) -> tuple[list[dict[str, Any]], list[dict[str, str]]]:
    users_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in users}
    public_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in public_users}
    operations: list[dict[str, Any]] = []
    skipped: list[dict[str, str]] = []

    for uid, private_data in users_by_id.items():
        if "customPhotoUrl" not in private_data:
            continue

        private_value = private_data["customPhotoUrl"]
        public_data = public_by_id.get(uid, {})
        public_value = public_data.get("customPhotoUrl")
        if not isinstance(private_value, str):
            if private_value is not None:
                skipped.append(
                    {"uid": uid, "reason": "private customPhotoUrl is not a string"}
                )
            continue

        if "customPhotoUrl" not in public_data or public_value == "":
            if not private_value:
                continue
            operations.append(
                {
                    "uid": uid,
                    "value": private_value,
                    "reason": "copy private value to missing public field",
                    "write": True,
                    "create_document": uid not in public_by_id,
                }
            )
        elif not isinstance(public_value, str):
            skipped.append(
                {
                    "uid": uid,
                    "reason": f"public customPhotoUrl is not a string: {type(public_value).__name__}",
                }
            )
        elif private_value != public_value:
            operations.append(
                {
                    "uid": uid,
                    "value": public_value,
                    "reason": "preserve existing public value",
                    "write": False,
                    "create_document": False,
                }
            )

    return operations, skipped


def _is_valid_public_value(field: str, value: Any) -> bool:
    if field in {"name", "photoUrl"}:
        return isinstance(value, str) or value is None
    if field == "votesVisible":
        return isinstance(value, bool) or value is None
    if field == "customPhotoUrl":
        return isinstance(value, str)
    return False


def _has_migration_value(field: str, value: Any) -> bool:
    if field == "votesVisible":
        return isinstance(value, bool)
    return isinstance(value, str) and value != ""


def _plan_public_profile_migration(
    users: Iterable[Any], public_users: Iterable[Any]
) -> tuple[list[dict[str, Any]], list[dict[str, str]]]:
    users_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in users}
    public_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in public_users}
    operations: list[dict[str, Any]] = []
    skipped: list[dict[str, str]] = []

    for uid, private_data in users_by_id.items():
        public_data = public_by_id.get(uid)
        changes: dict[str, Any] = {}

        for field in PROFILE_FIELDS:
            private_has_field = field in private_data
            public_has_field = public_data is not None and field in public_data
            private_value = private_data.get(field)
            public_value = public_data.get(field) if public_data is not None else None

            if field == "customPhotoUrl":
                if public_has_field and isinstance(public_value, str) and public_value:
                    continue

                legacy_custom_flag = (
                    private_has_field
                    and isinstance(private_value, bool)
                    and private_value
                ) or (
                    public_has_field and isinstance(public_value, bool) and public_value
                )
                if legacy_custom_flag:
                    legacy_url = public_data.get("photoUrl") if public_data else None
                    if isinstance(legacy_url, str) and legacy_url:
                        changes[field] = legacy_url
                    else:
                        skipped.append(
                            {
                                "uid": uid,
                                "reason": (
                                    "legacy customPhotoUrl is true but "
                                    "user-public.photoUrl has no usable URL"
                                ),
                            }
                        )
                    continue

                has_legacy_false_flag = (
                    private_has_field and private_value is False
                ) or (public_has_field and public_value is False)
                if has_legacy_false_flag and (
                    not public_has_field
                    or isinstance(public_value, bool)
                    or not public_value
                ):
                    changes[field] = ""
                    continue

                if public_has_field and not _is_valid_public_value(field, public_value):
                    skipped.append(
                        {
                            "uid": uid,
                            "reason": (
                                "public customPhotoUrl has unexpected type "
                                f"{type(public_value).__name__}"
                            ),
                        }
                    )
                    continue

                if private_has_field and not _is_valid_public_value(
                    field, private_value
                ):
                    if private_value is not None:
                        skipped.append(
                            {
                                "uid": uid,
                                "reason": (
                                    "private customPhotoUrl has unexpected type "
                                    f"{type(private_value).__name__}"
                                ),
                            }
                        )
                    continue

                public_is_missing = not public_has_field or public_value in (None, "")
                if (
                    private_has_field
                    and public_is_missing
                    and _has_migration_value(field, private_value)
                ):
                    changes[field] = private_value
                continue

            if public_has_field and not _is_valid_public_value(field, public_value):
                skipped.append(
                    {
                        "uid": uid,
                        "reason": (
                            f"public {field} has unexpected type "
                            f"{type(public_value).__name__}"
                        ),
                    }
                )
                continue

            if private_has_field and not _is_valid_public_value(field, private_value):
                if not public_has_field or public_value in (None, ""):
                    skipped.append(
                        {
                            "uid": uid,
                            "reason": (
                                f"private {field} has unexpected type "
                                f"{type(private_value).__name__}"
                            ),
                        }
                    )
                continue

            public_is_missing = not public_has_field or public_value in (None, "")
            if (
                private_has_field
                and public_is_missing
                and _has_migration_value(field, private_value)
            ):
                changes[field] = private_value
            elif (
                field == "votesVisible"
                and isinstance(private_value, bool)
                and isinstance(public_value, bool)
                and private_value != public_value
                and private_value
            ):
                changes[field] = True

        if changes:
            operations.append({"uid": uid, "data": changes})

    return operations, skipped


def _print_public_profile_migration_plan(
    operations: list[dict[str, Any]],
    skipped: list[dict[str, str]],
    *,
    details: bool,
) -> None:
    click.echo("public profile migration plan (dry-run unless --apply is supplied)")
    click.echo(f"  documents to update: {len(operations)}")
    field_counts = {field: 0 for field in PROFILE_FIELDS}
    for operation in operations:
        for field in operation["data"]:
            field_counts[field] += 1
    for field, count in field_counts.items():
        click.echo(f"    {field}: {count}")
    click.echo(f"  skipped for review: {len(skipped)}")

    if details:
        for operation in operations:
            click.echo(f"  {operation['uid']}: set {', '.join(operation['data'])}")
        for item in skipped:
            click.echo(f"  {item['uid']}: skipped ({item['reason']})")


def _apply_public_profile_migration(db: Any, operations: list[dict[str, Any]]) -> int:
    for start in range(0, len(operations), 500):
        batch = db.batch()
        public_collection = db.collection("user-public")
        for operation in operations[start : start + 500]:
            batch.set(
                public_collection.document(operation["uid"]),
                operation["data"],
                merge=True,
            )
        batch.commit()
    return len(operations)


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


def _print_custom_photo_migration_plan(
    operations: list[dict[str, Any]],
    skipped: list[dict[str, str]],
    *,
    details: bool,
) -> None:
    write_count = sum(operation["write"] for operation in operations)
    click.echo("customPhotoUrl migration plan (dry-run unless --apply is supplied)")
    click.echo(f"  writes planned: {write_count}")
    click.echo(f"  public values preserved: {len(operations) - write_count}")
    click.echo(f"  skipped for review: {len(skipped)}")
    if details:
        for operation in operations:
            action = "write" if operation["write"] else "no change"
            value = "set" if operation["value"] else "empty"
            click.echo(
                f"  {operation['uid']}: {action} customPhotoUrl={value} "
                f"({operation['reason']})"
            )
        for item in skipped:
            click.echo(f"  {item['uid']}: skipped ({item['reason']})")


def _apply_votes_visible_migration(db: Any, operations: list[dict[str, Any]]) -> int:
    return _apply_profile_field_migration(db, operations, "votesVisible")


def _apply_custom_photo_migration(db: Any, operations: list[dict[str, Any]]) -> int:
    return _apply_profile_field_migration(db, operations, "customPhotoUrl")


def _apply_profile_field_migration(
    db: Any, operations: list[dict[str, Any]], field: str
) -> int:
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
        payload = {field: operation["value"]}
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


def _plan_public_uid_cleanup(public_users: Iterable[Any]) -> list[str]:
    return [
        snapshot.id for snapshot in public_users if "uid" in (snapshot.to_dict() or {})
    ]


def _print_public_uid_cleanup_plan(uids: list[str], *, details: bool) -> None:
    click.echo("legacy user-public uid cleanup (dry-run unless --apply is supplied)")
    click.echo(f"  documents with uid field: {len(uids)}")
    if details:
        for uid in uids:
            click.echo(f"  {uid}: delete uid")


def _apply_public_uid_cleanup(db: Any, uids: list[str]) -> int:
    for start in range(0, len(uids), 500):
        batch = db.batch()
        public_collection = db.collection("user-public")
        for uid in uids[start : start + 500]:
            batch.update(
                public_collection.document(uid),
                {"uid": firestore.DELETE_FIELD},
            )
        batch.commit()
    return len(uids)


def _legacy_cleanup_blockers(
    report: dict[str, Any],
    users: Iterable[Any],
    public_users: Iterable[Any],
) -> dict[str, list[str]]:
    blockers: dict[str, list[str]] = {}
    users_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in users}
    public_by_id = {snapshot.id: snapshot.to_dict() or {} for snapshot in public_users}

    def add(uid: str, field: str) -> None:
        blockers.setdefault(uid, []).append(field)

    for uid, issues in report["issues"].items():
        for issue in issues:
            if issue == "missing_user_public_document":
                add(uid, "user-public document")
            elif issue.startswith("missing_public_field:"):
                field = issue.removeprefix("missing_public_field:")
                if field == "customPhotoUrl":
                    private_flag = users_by_id.get(uid, {}).get(field)
                    legacy_photo_url = public_by_id.get(uid, {}).get("photoUrl")
                    if (
                        private_flag is True
                        and isinstance(legacy_photo_url, str)
                        and legacy_photo_url
                    ):
                        add(
                            uid,
                            "customPhotoUrl (legacy true; source URL available in "
                            "user-public.photoUrl; run --migrate-public-profile)",
                        )
                    elif private_flag is True:
                        add(
                            uid,
                            "customPhotoUrl (legacy true; no usable URL in "
                            "user-public.photoUrl)",
                        )
                    else:
                        add(uid, field)
                else:
                    add(uid, field)

    for snapshot in public_users:
        data = snapshot.to_dict() or {}
        uid = snapshot.id
        for field in PROFILE_FIELDS:
            if field in data and not _is_valid_public_value(field, data[field]):
                add(uid, f"invalid user-public.{field}")
    return blockers


def _format_cleanup_blockers(blockers: dict[str, list[str]]) -> str:
    return "; ".join(
        f"{uid}: {', '.join(sorted(set(fields)))}"
        for uid, fields in sorted(blockers.items())
    )


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
        "  legacy uid field mismatch: "
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
            custom_photo_detail = report["custom_photo_details"].get(uid)
            if custom_photo_detail:
                click.echo(f"      customPhotoUrl: {custom_photo_detail}")


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
    "--migrate-custom-photo",
    is_flag=True,
    help="Backfill customPhotoUrl when public is missing; preserve existing public values.",
)
@click.option(
    "--migrate-public-profile",
    is_flag=True,
    help="Backfill all missing public profile fields from users; preserve public values.",
)
@click.option(
    "--delete-legacy-profile-fields",
    is_flag=True,
    help="Plan to delete uid and duplicated profile fields from users.",
)
@click.option(
    "--delete-public-uid",
    is_flag=True,
    help="Plan to delete the redundant uid field from user-public documents.",
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
    migrate_custom_photo: bool,
    migrate_public_profile: bool,
    delete_legacy_profile_fields: bool,
    delete_public_uid: bool,
    apply: bool,
    loglevel: int,
) -> None:
    """Audit, migrate, or clean up duplicated user profile fields."""
    selected_operations = sum(
        (
            migrate_votes_visible,
            migrate_custom_photo,
            migrate_public_profile,
            delete_legacy_profile_fields,
            delete_public_uid,
        )
    )
    if selected_operations > 1:
        raise click.UsageError(
            "Choose only one migration or cleanup option per invocation."
        )
    if apply and selected_operations == 0:
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
            written = _apply_votes_visible_migration(db, operations)
            click.echo(f"  writes applied: {written}")

    if migrate_custom_photo:
        operations, skipped = _plan_custom_photo_migration(
            user_snapshots,
            public_snapshots,
        )
        _print_custom_photo_migration_plan(operations, skipped, details=details)
        if apply:
            if skipped:
                raise click.ClickException(
                    "Migration contains values needing review; no writes were applied."
                )
            written = _apply_custom_photo_migration(db, operations)
            click.echo(f"  writes applied: {written}")

    if migrate_public_profile:
        operations, skipped = _plan_public_profile_migration(
            user_snapshots,
            public_snapshots,
        )
        _print_public_profile_migration_plan(operations, skipped, details=details)
        if apply:
            if skipped:
                skipped_details = "; ".join(
                    f"{item['uid']}: {item['reason']}" for item in skipped
                )
                raise click.ClickException(
                    "Public profile migration needs review; no writes were applied: "
                    f"{skipped_details}"
                )
            written = _apply_public_profile_migration(db, operations)
            click.echo(f"  documents updated: {written}")

    if delete_legacy_profile_fields:
        operations = _plan_legacy_profile_cleanup(user_snapshots)
        _print_legacy_profile_cleanup_plan(operations, details=details)
        if apply:
            blockers = _legacy_cleanup_blockers(
                report,
                user_snapshots,
                public_snapshots,
            )
            if blockers:
                raise click.ClickException(
                    "Cleanup blocked because public profile data is incomplete or "
                    "invalid; no fields were deleted. "
                    f"Affected documents: {_format_cleanup_blockers(blockers)}. "
                    "Run --migrate-public-profile --details, review the plan, then "
                    "rerun with --apply."
                )
            deleted = _apply_legacy_profile_cleanup(db, operations)
            click.echo(f"  fields deleted: {deleted}")

    if delete_public_uid:
        uids = _plan_public_uid_cleanup(public_snapshots)
        _print_public_uid_cleanup_plan(uids, details=details)
        if apply:
            deleted = _apply_public_uid_cleanup(db, uids)
            click.echo(f"  uid fields deleted: {deleted}")

    _log.info("Completed user profile audit")


if __name__ == "__main__":
    main()
