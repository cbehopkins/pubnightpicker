import hashlib
import json
import logging
import os
from collections import Counter
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import click
import firebase_admin
from firebase_admin import credentials

from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.common.output_file import OutputFile
from firebase_sub.database.handlers import DbHandler

_log = logging.getLogger("bob")

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
_DB_HANDLER: DbHandler | None = None


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


def _get_db_handler() -> DbHandler:
    global _DB_HANDLER
    if _DB_HANDLER is None:
        _ensure_firebase_app()
        _DB_HANDLER = DbHandler()
    return _DB_HANDLER


def _iter_document_snapshots(collection_ref):
    for doc in collection_ref.stream():
        yield doc
        for subcollection in doc.reference.collections():
            yield from _iter_document_snapshots(subcollection)


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        while chunk := f.read(65536):
            digest.update(chunk)
    return digest.hexdigest()


@click.command()
@click.option(
    "--loglevel",
    default="INFO",
    help="Set the log level (numeric or name)",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
)
@click.option("--logfile", type=click.Path(path_type=Path), help="Log file path")
@click.option(
    "--outfile",
    type=click.Path(path_type=Path, writable=True),
    required=True,
    help="Output file path for JSON data",
)
@click.option(
    "--manifest-out",
    type=click.Path(path_type=Path, writable=True),
    help="Optional manifest output path. Defaults to '<outfile>.manifest.json'",
)
def main(
    loglevel: int, logfile: Path | None, outfile: Path, manifest_out: Path | None
) -> None:
    configure_logging(loglevel, logfile)
    db_handler = _get_db_handler()

    started_at = datetime.now(UTC)
    per_collection_counts: Counter[str] = Counter()
    total_documents = 0

    with OutputFile(outfile) as out:

        for collection_ref in db_handler.db.collections():
            for document in _iter_document_snapshots(collection_ref):
                dct: dict[str, Any] | None = document.to_dict()
                if dct is None:
                    _log.warning(
                        "Skipping missing Firestore snapshot for %s",
                        document.reference.path,
                    )
                    continue
                out.write_document(
                    path=document.reference.path,
                    data=dct,
                    collection=document.reference.parent.id,
                    doc_id=document.id,
                    create_time=document.create_time,
                    update_time=document.update_time,
                )
                total_documents += 1
                root_collection = document.reference.path.split("/", 1)[0]
                per_collection_counts[root_collection] += 1

    completed_at = datetime.now(UTC)
    data_sha256 = _sha256_file(outfile)
    manifest_path = manifest_out or outfile.with_name(f"{outfile.name}.manifest.json")
    manifest = {
        "schema_version": 2,
        "data_file": str(outfile.name),
        "started_at": started_at.isoformat(),
        "completed_at": completed_at.isoformat(),
        "project_id": getattr(db_handler.db, "project", None),
        "total_documents": total_documents,
        "per_collection_counts": dict(per_collection_counts),
        "sha256": data_sha256,
    }
    with open(manifest_path, "w", encoding="utf-8") as mf:
        json.dump(manifest, mf, indent=2, sort_keys=True)
        mf.write("\n")

    _log.info("Backup completed, data written to %s", outfile)
    _log.info("Backup manifest written to %s", manifest_path)


if __name__ == "__main__":
    main()
