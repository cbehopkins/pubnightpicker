import json
import logging
from pathlib import Path

import click
import firebase_admin
from firebase_admin import credentials, firestore

from firebase_sub.common.logging import configure_logging
from firebase_sub.common.serialization import restore_datetimes

_log = logging.getLogger(__name__)

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent.parent / "cred.json"

cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)

db = firestore.client()


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
