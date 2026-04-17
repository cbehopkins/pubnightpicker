import logging
import queue
from datetime import date, timedelta
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
from firebase_sub.database.housekeeping import (
    CroniterTrigger,
    HousekeepingRunner,
    IntervalSchedule,
    PeriodicTrigger,
)
from firebase_sub.database.housekeeping_tasks import build_housekeeping_tasks
from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.poll_manager import PollManager
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType
from firebase_sub.send_email import send_ampub_email, send_poll_open_email
from firebase_sub.send_push import (
    send_poll_complete_push,
    send_poll_open_push,
    web_push_enabled,
)

_log = logging.getLogger(__name__)
# Based on https://firebase.google.com/docs/firestore/query-data/listen#python_5

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent.parent / "cred.json"
cred = credentials.Certificate(CRED_PATH)
app = firebase_admin.initialize_app(cred)

DB_HANDLER = DbHandler()


def poll_open_actions(dummy_email: bool, dummy_push: bool) -> ActionMan:
    send_poll_open_email_i = cast(
        ActionCallbackProtocol,
        partial(send_poll_open_email, emails_src=DB_HANDLER.query_open_emails),
    )
    open_am = ActionMan(dummy_email)
    open_am.bind(ActionType.EMAIL, send_poll_open_email_i)
    if web_push_enabled():
        send_poll_open_push_i = cast(
            ActionCallbackProtocol,
            partial(
                send_poll_open_push,
                endpoints_src=DB_HANDLER.query_active_push_endpoints,
            ),
        )
        open_am.bind(ActionType.PUSH, send_poll_open_push_i, dummy_run=dummy_push)
    return open_am


def poll_complete_actions(dummy_email: bool, dummy_push: bool) -> ActionMan:
    send_mail_list_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email),  # defaults to Google Groups mailing list
    )
    send_personal_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email, emails_src=DB_HANDLER.query_personal_emails),
    )
    complete_am = ActionMan(dummy_email)
    complete_am.bind(ActionType.EMAIL, send_mail_list_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    if web_push_enabled():
        send_push_i = cast(
            ActionCallbackProtocol,
            partial(
                send_poll_complete_push,
                endpoints_src=DB_HANDLER.query_active_push_endpoints,
            ),
        )
        complete_am.bind(ActionType.PUSH, send_push_i, dummy_run=dummy_push)
    return complete_am


def configure_logging(log_level: int, logfile: Path | None) -> None:
    if logfile:
        print(f"Logging to {logfile}")
        logging.basicConfig(level=log_level, filename=str(logfile), encoding="utf-8")
    else:
        logging.basicConfig(level=log_level)
    logging.getLogger("google.api_core.bidi").setLevel(logging.WARNING)


def sub_events(
    dummy_email: bool,
    dummy_push: bool,
    loglevel: int,
    logfile: Path | None,
    restart_interval: int,
    housekeeping_interval_seconds: int,
    housekeeping_cron: str | None,
    all_history: bool,
    poll_lookback_days: int,
) -> None:
    configure_logging(loglevel, logfile)
    q: queue.Queue[Event] = queue.Queue()
    healthcheck_interval_seconds = 10.0
    notification_mirror = NotificationAckMirrorHandler(DB_HANDLER.db)
    housekeeping_runner = HousekeepingRunner(
        tasks=build_housekeeping_tasks(DB_HANDLER.db),
        schedule=IntervalSchedule(interval_seconds=housekeeping_interval_seconds),
    )

    def open_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.NEW_POLL, doc=document))

    def comp_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.COMP_POLL, doc=document))

    def notification_request_callback(document: DocumentSnapshot) -> None:
        notification_mirror.mirror_request_document(document)

    def enqueue_housekeeping_tick() -> None:
        q.put(
            Event(
                type=EventType.TICK,
                doc=None,
                callback=lambda: housekeeping_runner.maybe_run(),
            )
        )

    _log.info("Notification request/ack mirror listener started")

    poll_min_date = None
    if all_history:
        _log.info("Poll listeners using full history")
    else:
        poll_min_date = (date.today() - timedelta(days=poll_lookback_days)).isoformat()
        _log.info(
            "Poll listeners using recent history (min_date=%s lookback_days=%s)",
            poll_min_date,
            poll_lookback_days,
        )

    if housekeeping_cron:
        _log.info("Housekeeping runner started (cron=%s)", housekeeping_cron)
    else:
        _log.info(
            "Housekeeping runner started (interval=%ss)",
            housekeeping_interval_seconds,
        )

    with (
        PollManager(
            DB_HANDLER.query_polls_by_status(
                completed=False,
                min_date=poll_min_date,
            ),
            add=open_poll_event_callback,
        ).start_periodic_restart(restart_interval),
        PollManager(
            DB_HANDLER.query_polls_by_status(
                completed=True,
                min_date=poll_min_date,
            ),
            add=comp_poll_event_callback,
            modify=comp_poll_event_callback,
        ).start_periodic_restart(restart_interval),
        PollManager(
            DB_HANDLER.query_notification_requests,
            add=notification_request_callback,
            modify=notification_request_callback,
        ).start_periodic_restart(restart_interval),
        (
            CroniterTrigger(
                cron_expression=housekeeping_cron,
                callback=enqueue_housekeeping_tick,
            )
            if housekeeping_cron
            else PeriodicTrigger(
                interval_seconds=housekeeping_interval_seconds,
                callback=enqueue_housekeeping_tick,
            )
        ),
        PubsList(
            DB_HANDLER.pub_collection,
        ) as pubs_list,
    ):
        pubs_list.start_periodic_restart(restart_interval)
        open_am = poll_open_actions(dummy_email, dummy_push)
        complete_am = poll_complete_actions(dummy_email, dummy_push)

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
                    event_date = None
                    completed = False
                else:
                    event_date = doc.get("date")
                    completed = doc.get("completed", False)
                    _log.info(
                        "New Event: Type:%s, Date:%s, Completed:%s",
                        event.type,
                        event_date,
                        completed,
                    )
            else:
                event_date = None
                completed = False

            event.handle_queue_item(
                DB_HANDLER,
                pubs_list,
                open_am,
                complete_am,
            )
            _log.info(
                f"Completed Event: Type:{event.type}, Date:{event_date}, Completed:{completed}"
            )


@click.command()
@click.option("--dummy-email/--no-dummy-email", default=False, help="Run in dummy mode")
@click.option(
    "--dummy-push/--no-dummy-push",
    default=False,
    help="Run push notifications in dummy mode",
)
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
@click.option(
    "--housekeeping-interval-seconds",
    type=int,
    default=60,
    show_default=True,
    help="Housekeeping trigger interval in seconds (ignored if --housekeeping-cron is set)",
)
@click.option(
    "--housekeeping-cron",
    type=str,
    default=None,
    help="Cron expression for housekeeping trigger (e.g. '0 0 * * 4')",
)
@click.option(
    "--all-history/--recent-history",
    default=False,
    help="Watch all historical polls, not just recent polls",
)
@click.option(
    "--poll-lookback-days",
    type=click.IntRange(min=0),
    default=7,
    show_default=True,
    help="When using recent-history, include polls from this many days ago",
)
def cli(
    dummy_email: bool,
    dummy_push: bool,
    loglevel: int,
    logfile: Path | None,
    restart_interval: int,
    housekeeping_interval_seconds: int,
    housekeeping_cron: str | None,
    all_history: bool,
    poll_lookback_days: int,
) -> None:
    sub_events(
        dummy_email,
        dummy_push,
        loglevel,
        logfile,
        restart_interval,
        housekeeping_interval_seconds,
        housekeeping_cron,
        all_history,
        poll_lookback_days,
    )


if __name__ == "__main__":
    cli()
