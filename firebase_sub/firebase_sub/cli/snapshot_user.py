import json
import logging
import os
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import click
import firebase_admin
from firebase_admin import auth as firebase_auth
from firebase_admin import credentials, firestore

from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.common.serialization import convert_datetimes

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
        cred = credentials.Certificate(CRED_PATH)
        firebase_admin.initialize_app(cred)
    _FIREBASE_APP_INITIALIZED = True


def _get_db():
    global _DB
    if _DB is None:
        _ensure_firebase_app()
        _DB = firestore.client()
    return _DB


def _document_record(
    path: str, data: dict[str, Any], **metadata: Any
) -> dict[str, Any]:
    parts = [part for part in path.split("/") if part]
    if len(parts) < 2 or len(parts) % 2 != 0:
        raise ValueError(f"Document path must have even segments: {path}")
    record = {
        "schema_version": 2,
        "path": "/".join(parts),
        "collection": parts[-2],
        "id": parts[-1],
        "data": data,
    }
    record.update(metadata)
    return convert_datetimes(record)


def _serialize_auth_user(user: Any) -> dict[str, Any]:
    providers = []
    for provider in getattr(user, "provider_data", []):
        providers.append(
            {
                "provider_id": getattr(provider, "provider_id", None),
                "uid": getattr(provider, "uid", None),
                "email": getattr(provider, "email", None),
                "display_name": getattr(provider, "display_name", None),
                "phone_number": getattr(provider, "phone_number", None),
            }
        )

    return convert_datetimes(
        {
            "uid": getattr(user, "uid", None),
            "email": getattr(user, "email", None),
            "display_name": getattr(user, "display_name", None),
            "phone_number": getattr(user, "phone_number", None),
            "photo_url": getattr(user, "photo_url", None),
            "disabled": getattr(user, "disabled", None),
            "email_verified": getattr(user, "email_verified", None),
            "custom_claims": getattr(user, "custom_claims", None),
            "providers": providers,
            "tokens_valid_after_timestamp": getattr(
                user, "tokens_valid_after_timestamp", None
            ),
            "user_metadata": {
                "creation_timestamp": getattr(
                    getattr(user, "user_metadata", None), "creation_timestamp", None
                ),
                "last_sign_in_timestamp": getattr(
                    getattr(user, "user_metadata", None), "last_sign_in_timestamp", None
                ),
                "last_refresh_timestamp": getattr(
                    getattr(user, "user_metadata", None), "last_refresh_timestamp", None
                ),
            },
        }
    )


def _snapshot_firestore_user_data(db, uid: str) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []

    for top_path in (f"users/{uid}", f"user-public/{uid}"):
        snap = db.document(top_path).get()
        data = snap.to_dict()
        if data is None:
            continue
        records.append(
            _document_record(
                top_path,
                data,
                create_time=snap.create_time,
                update_time=snap.update_time,
            )
        )

    push_endpoint_stream = (
        db.collection("users").document(uid).collection("push_endpoints").stream()
    )
    for snap in push_endpoint_stream:
        data = snap.to_dict()
        if data is None:
            continue
        records.append(
            _document_record(
                snap.reference.path,
                data,
                create_time=snap.create_time,
                update_time=snap.update_time,
            )
        )

    for role_doc in db.collection("roles").stream():
        data = role_doc.to_dict() or {}
        if uid in data:
            records.append(
                _document_record(
                    role_doc.reference.path,
                    data,
                    create_time=role_doc.create_time,
                    update_time=role_doc.update_time,
                )
            )

    records.sort(key=lambda item: str(item.get("path", "")))
    return records


@click.command()
@click.option(
    "--loglevel",
    default="INFO",
    help="Set the log level (numeric or name)",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
)
@click.option("--logfile", type=click.Path(path_type=Path), help="Log file path")
@click.option("--uid", required=True, help="Auth uid to snapshot")
@click.option(
    "--outfile",
    type=click.Path(path_type=Path, writable=True),
    required=True,
    help="Output file path for user snapshot JSON",
)
@click.option(
    "--allow-missing-auth",
    is_flag=True,
    default=False,
    help="Allow snapshot to proceed when Auth user does not exist.",
)
def main(
    loglevel: int,
    logfile: Path | None,
    uid: str,
    outfile: Path,
    allow_missing_auth: bool,
) -> None:
    configure_logging(loglevel, logfile)
    db = _get_db()

    auth_payload: dict[str, Any] | None
    try:
        auth_user = firebase_auth.get_user(uid)
    except firebase_auth.UserNotFoundError:
        if not allow_missing_auth:
            raise click.ClickException(
                f"Auth user {uid} not found. Use --allow-missing-auth to continue."
            )
        _log.warning("Auth user %s not found; writing snapshot with auth=null", uid)
        auth_payload = None
    else:
        auth_payload = _serialize_auth_user(auth_user)

    firestore_records = _snapshot_firestore_user_data(db, uid)

    snapshot = {
        "schema_version": 1,
        "generated_at": datetime.now(UTC).isoformat(),
        "uid": uid,
        "auth": auth_payload,
        "firestore": firestore_records,
        "summary": {
            "firestore_record_count": len(firestore_records),
            "has_auth_record": auth_payload is not None,
        },
    }

    with open(outfile, "w", encoding="utf-8") as f:
        json.dump(snapshot, f, indent=2, sort_keys=True)
        f.write("\n")

    _log.info("Wrote user snapshot for uid=%s to %s", uid, outfile)


if __name__ == "__main__":
    main()
