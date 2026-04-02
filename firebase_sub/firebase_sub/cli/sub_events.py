import logging
import queue
from functools import partial
from pathlib import Path
from typing import cast

import click
import firebase_admin
from firebase_admin import credentials
from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.action_track import ActionCallbackProtocol, ActionMan, ActionType
from firebase_sub.common.logging import log_level_to_int
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
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
    send_mail_list_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email),  # defaults to Google Groups mailing list
    )
    send_personal_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email, emails_src=DB_HANDLER.query_personal_emails),
    )
    complete_am = ActionMan(dummy_run)
    complete_am.bind(ActionType.EMAIL, send_mail_list_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    return complete_am


def configure_logging(log_level: int, logfile: Path | None) -> None:
    if logfile:
        print(f"Logging to {logfile}")
        logging.basicConfig(level=log_level, filename=str(logfile), encoding="utf-8")
    else:
        logging.basicConfig(level=log_level)
    logging.getLogger("google.api_core.bidi").setLevel(logging.WARNING)


def sub_events(
    dummy: bool, loglevel: int, logfile: Path | None, restart_interval: int
) -> None:
    configure_logging(loglevel, logfile)
    dummy_run = dummy
    q: queue.Queue[Event] = queue.Queue()
    healthcheck_interval_seconds = 10.0
    notification_mirror = NotificationAckMirrorHandler(DB_HANDLER.db)

    def open_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.NEW_POLL, doc=document))

    def comp_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.COMP_POLL, doc=document))

    def notification_request_callback(document: DocumentSnapshot) -> None:
        notification_mirror.mirror_request_document(document)

    _log.info("Notification request/ack mirror listener started")

    with (
        PollManager(
            DB_HANDLER.query_completed_false,
            add=open_poll_event_callback,
        ).start_periodic_restart(restart_interval),
        PollManager(
            DB_HANDLER.query_completed_true,
            add=comp_poll_event_callback,
            modify=comp_poll_event_callback,
        ).start_periodic_restart(restart_interval),
        PollManager(
            DB_HANDLER.query_notification_requests,
            add=notification_request_callback,
            modify=notification_request_callback,
        ).start_periodic_restart(restart_interval),
        PubsList(
            DB_HANDLER.pub_collection,
        ) as pubs_list,
    ):
        pubs_list.start_periodic_restart(restart_interval)
        open_am = poll_open_actions(dummy_run)
        complete_am = poll_complete_actions(dummy_run)

        while True:
            try:
                event = q.get(timeout=healthcheck_interval_seconds)
            except queue.Empty:
                if not DB_HANDLER.okay:
                    raise SystemExit("Exiting due to db is not okay")
                continue

            if event.doc:
                doc = event.doc.to_dict()
                if doc is None:
                    _log.warning(
                        "Received event %s for doc %s with no payload",
                        event.type,
                        event.doc.id,
                    )
                    date = None
                    completed = False
                else:
                    date = doc.get("date")
                    completed = doc.get("completed", False)
                    _log.info(
                        "New Event: Type:%s, Date:%s, Completed:%s",
                        event.type,
                        date,
                        completed,
                    )
            else:
                date = None
                completed = False

            event.handle_queue_item(DB_HANDLER, pubs_list, open_am, complete_am)
            _log.info(
                f"Completed Event: Type:{event.type}, Date:{date}, Completed:{completed}"
            )


@click.command()
@click.option("--dummy/--no-dummy", default=False, help="Run in dummy mode")
@click.option(
    "--loglevel", default=logging.INFO, type=log_level_to_int, help="Set the log level"
)
@click.option(
    "--logfile", type=click.Path(path_type=Path), default=None, help="Log file path"
)
@click.option(
    "--restart-interval",
    type=int,
    default=60 * 24,
    help="Restart interval in minutes (default: 1 day)",
)
def cli(
    dummy: bool, loglevel: int, logfile: Path | None, restart_interval: int
) -> None:
    sub_events(dummy, loglevel, logfile, restart_interval)


if __name__ == "__main__":
    cli()
