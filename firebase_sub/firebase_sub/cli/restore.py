import json
import logging
import os
from pathlib import Path

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
def main(loglevel: str, logfile: Path | None, infile: Path) -> None:
    configure_logging(loglevel, logfile)
    db = _get_db()

    with open(infile, "r", encoding="utf-8") as f:
        try:
            records = json.load(f)
        except Exception as e:
            raise ValueError(f"Failed to load JSON from {infile}: {e}") from e

        for record in records:
            try:
                collection = record["collection"]
                doc_id = record["id"]
                data = restore_datetimes(record["data"])
                if not isinstance(data, dict):
                    raise ValueError(
                        f"Document data for id {doc_id} in collection {collection} is not a dictionary: {type(data)}"
                    )
                db.collection(collection).document(doc_id).set(data)
                _log.info(f"Restored document {doc_id} to collection {collection}")
            except Exception as e:
                _log.exception(f"Failed to restore record: {record}", exc_info=e)


if __name__ == "__main__":
    main()
