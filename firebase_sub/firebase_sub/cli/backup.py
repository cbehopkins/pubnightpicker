import datetime
import json
import logging
import time
from pathlib import Path
from typing import Any, Sequence

import click
import firebase_admin
from firebase_admin import credentials
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.database.pubs_list import PubsList

_log = logging.getLogger(__name__)

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent.parent / "cred.json"
cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)

DB_HANDLER = DbHandler()


class OutputFile:
    def __init__(self, path: Path):
        self.path = path
        self.file = open(self.path, "w", encoding="utf-8")
        self.separator = ""

    def write_dict(self, collection: str, doc_id: str, data: dict):
        record = {
            "collection": collection,
            "id": doc_id,
            "data": data,
        }
        json_str = json.dumps(record, ensure_ascii=False)
        self.file.write(self.separator + json_str)
        self.separator = ",\n"
        self.file.flush()

    def close(self):
        self.file.write("\n]\n")
        self.file.close()

    def __enter__(self):
        self.file.write("[\n")
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()


def log_level_to_int(level: str | int) -> int:
    try:
        return int(level)
    except ValueError:
        assert isinstance(level, str), "Level must be a string by now..."
        return int(logging.getLevelNamesMapping().get(level, logging.INFO))


def configure_logging(log_level, logfile):
    logging_config = {"level": log_level}
    if logfile:
        print(f"Logging to {logfile}")
        logging_config["filename"] = logfile
        logging_config["encoding"] = "utf-8"

    logging.basicConfig(**logging_config)
    logging.getLogger("google.api_core.bidi").setLevel(logging.WARNING)


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
def main(loglevel: int, logfile: Path | None, outfile: Path) -> None:
    configure_logging(loglevel, logfile)

    with OutputFile(outfile) as out:

        def backup_item(collection_name, document: DocumentSnapshot) -> None:
            dct: dict[str, Any] | None = document.to_dict()
            assert dct
            out.write_dict(collection_name, document.id, dct)

        DB_HANDLER.all_events_except_users(backup_item)

        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            _log.info("Backup interrupted by user. Closing output file cleanly.")


if __name__ == "__main__":
    main()
