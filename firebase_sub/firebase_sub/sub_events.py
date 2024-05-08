import argparse
from functools import partial
import logging
from pathlib import Path
import queue
import time
import firebase_admin
from firebase_admin import credentials, firestore
from google.cloud.firestore_v1.base_query import FieldFilter

from firebase_sub.action_track import ActionMan, ActionType
from firebase_sub.masto import toot_for_me
from firebase_sub.poll_manager import PollManager
from firebase_sub.pubs_list import PubsList
from firebase_sub.send_email import send_ampub_email, send_poll_open_email

_log = logging.getLogger(__name__)
# Based on https://firebase.google.com/docs/firestore/query-data/listen#python_5

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent / "cred.json"
cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)
db = firestore.client()


def query_personal_emails():
    docs_query = db.collection("users").where(
        filter=FieldFilter("notificationEmailEnabled", "==", True)
    )
    _log.info("Generating personal email addresses")
    for doc in docs_query.stream():
        record = doc.to_dict()
        pemail = record["notificationEmail"]
        _log.debug(f"{pemail}")
        yield pemail, record["uid"]


def query_open_emails():
    docs_query = db.collection("users").where(
        filter=FieldFilter("openPollEmailEnabled", "==", True)
    )
    _log.info("Generating personal email addresses")
    for doc in docs_query.stream():
        record = doc.to_dict()
        pemail = record["notificationEmail"]
        _log.debug(f"{pemail}")
        yield pemail, record["uid"]


def new_poll_event_handler(am: ActionMan, poll_id):
    action_document = db.collection("open_actions").document(poll_id)
    new_action_dict = am.action_event(
        action_dict=action_document.get().to_dict(),
        action_key=poll_id,
    )
    if new_action_dict:
        action_document.set(new_action_dict, merge=True)


def complete_poll_event_handler(pubs_list, am: ActionMan, poll_id):
    polls_document = db.collection("polls").document(poll_id)
    action_document = db.collection("comp_actions").document(poll_id)

    poll_dict = polls_document.get().to_dict()
    pub_id = poll_dict["selected"]
    assert (
        pub_id in pubs_list
    ), f"Someone selected a pub that's not in the pub dict, {pub_id=}"
    new_action_dict = am.action_event(
        action_dict=action_document.get().to_dict(),
        action_key=pub_id,
        poll_dict=poll_dict,
        pub_dict=pubs_list,
    )
    if new_action_dict:
        action_document.set(new_action_dict, merge=True)


def poll_open_actions(dummy_run):
    send_poll_open_email_i = partial(send_poll_open_email, emails_src=query_open_emails)
    open_am = ActionMan(dummy_run)
    open_am.bind(ActionType.EMAIL, send_poll_open_email_i)
    return open_am


def poll_complete_actions(dummy_run):
    send_personal_email = partial(send_ampub_email, emails_src=query_personal_emails)
    complete_am = ActionMan(dummy_run)
    complete_am.bind(ActionType.TOOT, toot_for_me)
    complete_am.bind(ActionType.EMAIL, send_ampub_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    return complete_am


def log_level_to_int(level):
    try:
        level = int(level)
    except ValueError:
        level = logging.getLevelName(level)
    return level


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


if __name__ == "__main__":
    args = arg_parser_setup(log_level_to_int)
    configure_logging(args.loglevel, args.logfile)

    dummy_run = args.dummy
    q = queue.Queue()

    def open_poll_event_callback(document):
        q.put(
            {
                "type": "new_poll",
                "doc": document,
            }
        )

    def comp_poll_event_callback(document):
        q.put(
            {
                "type": "comp_poll",
                "doc": document,
            }
        )

    with PollManager(
        db.collection("polls").where(filter=FieldFilter("completed", "==", False)),
        add=open_poll_event_callback,
    ), PollManager(
        db.collection("polls").where(filter=FieldFilter("completed", "==", True)),
        add=comp_poll_event_callback,
        modify=comp_poll_event_callback,
    ), PubsList(
        db
    ) as pubs_list:
        open_am = poll_open_actions(dummy_run)
        complete_am = poll_complete_actions(dummy_run)

        while True:
            event = q.get()
            _log.debug(f"New Event: {event}")
            if event["type"] == "new_poll":
                new_poll_event_handler(open_am, poll_id=event["doc"].id)
            if event["type"] == "comp_poll":
                complete_poll_event_handler(
                    pubs_list, complete_am, poll_id=event["doc"].id
                )

            time.sleep(1)
