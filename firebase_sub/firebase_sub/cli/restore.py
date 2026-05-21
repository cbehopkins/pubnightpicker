import hashlib
import json
import logging
import os
from pathlib import Path
from typing import Any

import click
import firebase_admin
from firebase_admin import credentials, firestore

from firebase_sub.common.logging import configure_logging
from firebase_sub.common.serialization import restore_datetimes

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

    # Prefer a predictable default for packaged runtime environments.
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


def _record_path(record: dict[str, Any]) -> str:
    path = record.get("path")
    if isinstance(path, str) and path.strip():
        normalized = "/".join(part for part in path.split("/") if part)
        if normalized and len(normalized.split("/")) % 2 == 0:
            return normalized
        raise ValueError(f"Invalid document path in record: {path}")

    collection = record.get("collection")
    doc_id = record.get("id")
    if isinstance(collection, str) and isinstance(doc_id, str):
        return f"{collection}/{doc_id}"
    raise ValueError("Record must include either path or collection+id")


def _record_uid(path: str) -> str | None:
    parts = path.split("/")
    if len(parts) < 2:
        return None
    if parts[0] in {"users", "user-public"}:
        return parts[1]
    if parts[0] == "roles":
        return None
    return None


def _root_collection(path: str) -> str:
    parts = path.split("/")
    if not parts or not parts[0]:
        raise ValueError(f"Invalid path for collection extraction: {path}")
    return parts[0]


def _collection_allowed(
    path: str,
    allow_collections: set[str],
    deny_collections: set[str],
) -> bool:
    root = _root_collection(path)
    if root in deny_collections:
        return False
    if allow_collections and root not in allow_collections:
        return False
    return True


def _validate_restore_intent(
    *,
    dry_run: bool,
    uid: str | None,
    allow_collections: set[str],
    confirm_non_dry_run: bool,
) -> None:
    if dry_run:
        return
    if confirm_non_dry_run:
        return
    if uid is not None:
        return
    if allow_collections:
        return
    raise ValueError(
        "Refusing broad non-dry-run restore without explicit confirmation. "
        "Use --confirm-non-dry-run or scope with --uid/--allow-collection."
    )


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        while chunk := f.read(65536):
            digest.update(chunk)
    return digest.hexdigest()


def _verify_manifest(infile: Path, manifest_file: Path) -> None:
    with open(manifest_file, "r", encoding="utf-8") as f:
        manifest = json.load(f)
    if not isinstance(manifest, dict):
        raise ValueError("Manifest payload must be a JSON object")

    expected_sha = manifest.get("sha256")
    if not isinstance(expected_sha, str) or not expected_sha:
        raise ValueError("Manifest is missing a valid 'sha256' field")

    actual_sha = _sha256_file(infile)
    if expected_sha != actual_sha:
        raise ValueError(
            f"Manifest checksum mismatch for {infile}: expected {expected_sha}, got {actual_sha}"
        )

    expected_data_file = manifest.get("data_file")
    if isinstance(expected_data_file, str) and expected_data_file:
        if expected_data_file != infile.name:
            _log.warning(
                "Manifest data_file differs from restore input (manifest=%s input=%s)",
                expected_data_file,
                infile.name,
            )


@click.command()
@click.option(
    "--loglevel",
    default="INFO",
    help="Set the log level (numeric or name)",
    show_default=True,
)
@click.option("--logfile", type=click.Path(path_type=Path), help="Log file path")
@click.option(
    "--infile",
    type=click.Path(path_type=Path, exists=True, readable=True),
    required=True,
    help="Input file path for JSON data",
)
@click.option(
    "--dry-run",
    is_flag=True,
    default=False,
    help="Print planned restores without writing to Firestore.",
)
@click.option(
    "--uid",
    type=str,
    help="Only restore records tied to this user uid (users/user-public paths).",
)
@click.option(
    "--manifest",
    "manifest_file",
    type=click.Path(path_type=Path, exists=True, readable=True),
    help="Optional manifest file to verify backup checksum before restore.",
)
@click.option(
    "--allow-collection",
    "allow_collections",
    multiple=True,
    help="Restrict restore to one or more root collections (repeat option).",
)
@click.option(
    "--deny-collection",
    "deny_collections",
    multiple=True,
    help="Exclude one or more root collections from restore (repeat option).",
)
@click.option(
    "--confirm-non-dry-run",
    is_flag=True,
    default=False,
    help="Required to run an unscoped non-dry-run restore.",
)
def main(
    loglevel: str,
    logfile: Path | None,
    infile: Path,
    dry_run: bool,
    uid: str | None,
    manifest_file: Path | None,
    allow_collections: tuple[str, ...],
    deny_collections: tuple[str, ...],
    confirm_non_dry_run: bool,
) -> None:
    configure_logging(loglevel, logfile)
    db = _get_db()

    allow_set = {c.strip() for c in allow_collections if c and c.strip()}
    deny_set = {c.strip() for c in deny_collections if c and c.strip()}
    overlap = allow_set & deny_set
    if overlap:
        overlap_csv = ", ".join(sorted(overlap))
        raise ValueError(
            f"Collections cannot be in both allow and deny lists: {overlap_csv}"
        )
    _validate_restore_intent(
        dry_run=dry_run,
        uid=uid,
        allow_collections=allow_set,
        confirm_non_dry_run=confirm_non_dry_run,
    )

    if manifest_file is not None:
        _verify_manifest(infile, manifest_file)
        _log.info("Verified backup manifest checksum from %s", manifest_file)

    with open(infile, "r", encoding="utf-8") as f:
        try:
            records = json.load(f)
        except Exception as e:
            raise ValueError(f"Failed to load JSON from {infile}: {e}") from e

        if not isinstance(records, list):
            raise ValueError(
                f"Backup payload must be a list of records: {type(records)}"
            )

        for record in records:
            try:
                if not isinstance(record, dict):
                    raise ValueError(f"Backup record must be an object: {type(record)}")
                path = _record_path(record)
                if not _collection_allowed(path, allow_set, deny_set):
                    continue
                if uid is not None:
                    record_uid = _record_uid(path)
                    if record_uid != uid:
                        continue
                data = restore_datetimes(record["data"])
                if not isinstance(data, dict):
                    raise ValueError(
                        f"Document data for path {path} is not a dictionary: {type(data)}"
                    )
                if dry_run:
                    _log.info("Dry-run restore would write %s", path)
                    continue
                db.document(path).set(data)
                _log.info("Restored document %s", path)
            except Exception as e:
                _log.exception(f"Failed to restore record: {record}", exc_info=e)


if __name__ == "__main__":
    main()
