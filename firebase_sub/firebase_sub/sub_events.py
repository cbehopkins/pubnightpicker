import argparse
import enum
from functools import partial
import logging
from pathlib import Path
import queue
import time
from typing import cast
import firebase_admin
from firebase_admin import credentials
from dataclasses import dataclass

from firebase_sub.action_track import ActionCallbackProtocol, ActionMan, ActionType
from firebase_sub.handlers import DbHandler
from firebase_sub.poll_manager import PollManager
from firebase_sub.pubs_list import PubsList
from firebase_sub.send_email import send_ampub_email, send_poll_open_email
# from google.cloud.firestore_v1.watch import DocumentChange
from google.cloud.firestore_v1.base_document import DocumentSnapshot

_log = logging.getLogger(__name__)
# Based on https://firebase.google.com/docs/firestore/query-data/listen#python_5

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent / "cred.json"
cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)

DB_HANDLER = DbHandler()


def poll_open_actions(dummy_run: bool) -> ActionMan:
    send_poll_open_email_i  = cast(ActionCallbackProtocol, partial(send_poll_open_email, emails_src=DB_HANDLER.query_open_emails))
    open_am = ActionMan(dummy_run)
    open_am.bind(ActionType.EMAIL, send_poll_open_email_i)
    return open_am


def poll_complete_actions(dummy_run: bool) -> ActionMan:
    send_personal_email  = cast(ActionCallbackProtocol,partial(send_ampub_email, emails_src=DB_HANDLER.query_personal_emails))
    complete_am = ActionMan(dummy_run)
    complete_am.bind(ActionType.EMAIL, send_ampub_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    return complete_am


def log_level_to_int(level: str | int) -> int:
    try:
        return int(level)
    except ValueError:
        return int(logging.getLevelName(level))


def arg_parser_setup(log_level_to_int):
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

class EventType(enum.Enum):
    NEW_POLL = "new_poll"
    COMP_POLL = "comp_poll"

@dataclass
class Event:
    type: EventType
    doc: DocumentSnapshot

    def handle_queue_item(self, DB_HANDLER: DbHandler, pubs_list: PubsList, open_am: ActionMan, complete_am: ActionMan):
        match self.type:
            case EventType.NEW_POLL:
                DB_HANDLER.new_poll_event_handler(open_am, poll_id=self.doc.id)
            case EventType.COMP_POLL:
                DB_HANDLER.complete_poll_event_handler(
                    pubs_list, complete_am, poll_id=self.doc.id
                )

if __name__ == "__main__":
    args = arg_parser_setup(log_level_to_int)
    configure_logging(args.loglevel, args.logfile)

    dummy_run = args.dummy
    q = queue.Queue()

    def open_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.NEW_POLL, doc=document))

    def comp_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.COMP_POLL, doc=document))

    with PollManager(
        DB_HANDLER.query_completed_false,
        add=open_poll_event_callback,
    ), PollManager(
        DB_HANDLER.query_completed_true,
        add=comp_poll_event_callback,
        modify=comp_poll_event_callback,
    ), PubsList(
        DB_HANDLER.pub_collection,
    ) as pubs_list:
        open_am = poll_open_actions(dummy_run)
        complete_am = poll_complete_actions(dummy_run)

        while True:
            event: Event = q.get()
            doc = event.doc.to_dict()
            assert doc
            date = doc["date"]
            completed: bool = doc.get("completed", False)
            _log.info(f"New Event: Type:{event.type}, Date:{date}, Completed:{completed}")
            event.handle_queue_item(DB_HANDLER, pubs_list, open_am, complete_am)
            _log.info(f"Completed Event: Type:{event.type}, Date:{date}, Completed:{completed}")

            time.sleep(1)
