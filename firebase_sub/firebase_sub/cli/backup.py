import json
import logging
import threading
from pathlib import Path
from typing import Any

import click
import firebase_admin
from firebase_admin import credentials
from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.common.output_file import OutputFile
from firebase_sub.database.handlers import DbHandler

_log = logging.getLogger(__name__)

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent.parent / "cred.json"
cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)

DB_HANDLER = DbHandler()


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
    "--timeout",
    type=int,
    default=5,
    show_default=True,
    help="Timeout in seconds to wait for new items before exiting.",
)
def main(loglevel: int, logfile: Path | None, outfile: Path, timeout: int) -> None:
    configure_logging(loglevel, logfile)

    stop_event = threading.Event()
    timer = None

    def on_timeout():
        _log.info(f"No new items for {timeout} seconds. Exiting.")
        stop_event.set()

    def reset_timer():
        nonlocal timer
        if timer:
            timer.cancel()
        timer = threading.Timer(timeout, on_timeout)
        timer.start()

    with OutputFile(outfile) as out:

        def backup_item(collection_name, document: DocumentSnapshot) -> None:
            dct: dict[str, Any] | None = document.to_dict()
            assert dct
            out.write_dict(collection_name, document.id, dct)
            reset_timer()

        reset_timer()
        DB_HANDLER.all_events_except_users(backup_item)
        # Wait for timeout after last item
        stop_event.wait()
        if timer:
            timer.cancel()
        _log.info(f"Backup completed, data written to {outfile}")


if __name__ == "__main__":
    main()
