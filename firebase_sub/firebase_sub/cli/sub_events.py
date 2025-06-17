import argparse
import logging
import queue
import threading
import time
from functools import partial
from pathlib import Path
from typing import cast

import firebase_admin
from firebase_admin import credentials
from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionCallbackProtocol, ActionMan, ActionType
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType
from firebase_sub.send_email import send_ampub_email, send_poll_open_email

_log = logging.getLogger(__name__)
# Based on https://firebase.google.com/docs/firestore/query-data/listen#python_5

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent.parent / "cred.json"
cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)

DB_HANDLER = DbHandler()


def poll_open_actions(dummy_run: bool) -> ActionMan:
    send_poll_open_email_i = cast(
        ActionCallbackProtocol,
        partial(send_poll_open_email, emails_src=DB_HANDLER.query_open_emails),
    )
    open_am = ActionMan(dummy_run)
    open_am.bind(ActionType.EMAIL, send_poll_open_email_i)
    return open_am


def poll_complete_actions(dummy_run: bool) -> ActionMan:
    send_personal_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email, emails_src=DB_HANDLER.query_personal_emails),
    )
    complete_am = ActionMan(dummy_run)
    complete_am.bind(ActionType.EMAIL, send_ampub_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    return complete_am


def log_level_to_int(level: str | int) -> int:
    try:
        return int(level)
    except ValueError:
        return int(logging.getLevelName(level))


def arg_parser_setup():
    parser = argparse.ArgumentParser(description="ampubnight notification server")
    parser.add_argument("--dummy", action=argparse.BooleanOptionalAction)
    parser.add_argument(
        "--loglevel",
        default=logging.INFO,
        help="set the log level",
        type=log_level_to_int,
    )
    parser.add_argument("--logfile", type=Path)

    args = parser.parse_args()
    return args


def configure_logging(log_level, logfile):
    logging_config = {"level": log_level}
    if logfile:
        print(f"Logging to {logfile}")
        logging_config["filename"] = logfile
        logging_config["encoding"] = "utf-8"

    logging.basicConfig(**logging_config)
    logging.getLogger("google.api_core.bidi").setLevel(logging.WARNING)

def heartbeat_publisher(queue):
    while True:
        event = Event(type=EventType.HEARTBEAT, doc=None)
        queue.put(event)
        time.sleep(10)

if __name__ == "__main__":
    args = arg_parser_setup()
    configure_logging(args.loglevel, args.logfile)

    dummy_run = args.dummy
    q = queue.Queue()
    heartbeat_thread = threading.Thread(target=heartbeat_publisher, args=(q,), daemon=True)
    heartbeat_thread.start()

    def open_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.NEW_POLL, doc=document))

    def comp_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.COMP_POLL, doc=document))

    with (
        PollManager(
            DB_HANDLER.query_completed_false,
            add=open_poll_event_callback,
        ),
        PollManager(
            DB_HANDLER.query_completed_true,
            add=comp_poll_event_callback,
            modify=comp_poll_event_callback,
        ),
        PubsList(
            DB_HANDLER.pub_collection,
        ) as pubs_list,
    ):  # FIXME can we integrate pubs_list into the DB_Handler?
        open_am = poll_open_actions(dummy_run)
        complete_am = poll_complete_actions(dummy_run)

        while True:
            event: Event = q.get()
            if event.doc:
                doc = event.doc.to_dict()
                assert doc
                date = doc["date"]
                completed: bool = doc.get("completed", False)
                _log.info(
                    f"New Event: Type:{event.type}, Date:{date}, Completed:{completed}"
                )
            else:
                _log.info(f"New Event: Type:{event.type}, No Document")
                date = None
                completed = False

            event.handle_queue_item(DB_HANDLER, pubs_list, open_am, complete_am)
            _log.info(
                f"Completed Event: Type:{event.type}, Date:{date}, Completed:{completed}"
            )

            time.sleep(1)
