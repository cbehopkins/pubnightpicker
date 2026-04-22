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
from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.common.retry import retry
from firebase_sub.database.handlers import DbHandler, RetryablePollDataNotReadyError
from firebase_sub.database.housekeeping import (
    CroniterTrigger,
    HousekeepingRunner,
    IntervalSchedule,
    PeriodicTrigger,
)
from firebase_sub.database.housekeeping_tasks import build_housekeeping_tasks
from firebase_sub.database.canary import CanaryWatcher
from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
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
_FIREBASE_APP_INITIALIZED = False
_DB_HANDLER: DbHandler | None = None
_COMP_POLL_MAX_RETRIES = 10
_COMP_POLL_RETRY_DELAY_SECONDS = 1.0

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


def poll_open_actions(
    dummy_email: bool, dummy_push: bool, db_handler: DbHandler | None = None
) -> ActionMan:
    db_handler = db_handler or _get_db_handler()
    send_poll_open_email_i = cast(
        ActionCallbackProtocol,
        partial(send_poll_open_email, emails_src=db_handler.query_open_emails),
    )
    open_am = ActionMan(dummy_email)
    open_am.bind(ActionType.EMAIL, send_poll_open_email_i)
    if web_push_enabled():
        send_poll_open_push_i = cast(
            ActionCallbackProtocol,
            partial(
                send_poll_open_push,
                endpoints_src=db_handler.query_active_push_endpoints,
            ),
        )
        open_am.bind(ActionType.PUSH, send_poll_open_push_i, dummy_run=dummy_push)
    return open_am


def poll_complete_actions(
    dummy_email: bool, dummy_push: bool, db_handler: DbHandler | None = None
) -> ActionMan:
    db_handler = db_handler or _get_db_handler()
    send_mail_list_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email),  # defaults to Google Groups mailing list
    )
    send_personal_email = cast(
        ActionCallbackProtocol,
        partial(send_ampub_email, emails_src=db_handler.query_personal_emails),
    )
    complete_am = ActionMan(dummy_email)
    complete_am.bind(ActionType.EMAIL, send_mail_list_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    if web_push_enabled():
        send_push_i = cast(
            ActionCallbackProtocol,
            partial(
                send_poll_complete_push,
                endpoints_src=db_handler.query_active_push_endpoints,
            ),
        )
        complete_am.bind(ActionType.PUSH, send_push_i, dummy_run=dummy_push)
    return complete_am


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
    canary_interval_seconds: int = 300,
) -> None:
    configure_logging(loglevel, logfile)
    if restart_interval:
        _log.info(
            "Ignoring restart interval of %s minutes; periodic Firestore watch restarts are disabled",
            restart_interval,
        )
    db_handler = _get_db_handler()
    q: queue.Queue[Event] = queue.Queue()
    healthcheck_interval_seconds = 10.0
    notification_mirror = NotificationAckMirrorHandler(db_handler.db)
    notification_push_test = NotificationPushTestHandler(
        db_handler.db,
        db_handler.query_active_push_endpoints_for_user,
        dummy_push=dummy_push,
    )
    housekeeping_runner = HousekeepingRunner(
        tasks=build_housekeeping_tasks(db_handler.db),
        schedule=IntervalSchedule(interval_seconds=housekeeping_interval_seconds),
    )

    open_am = poll_open_actions(dummy_email, dummy_push, db_handler)

    def new_handler(document: DocumentSnapshot | None, pubs_list: PubsList) -> None:
        if document is None:
            raise ValueError(
                f"New Event has no document. This indicates a coding error."
            )
        db_handler.new_poll_event_handler(open_am, poll_id=document.id)

    def open_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(Event(type=EventType.NEW_POLL, doc=document, callback=new_handler))

    complete_am = poll_complete_actions(dummy_email, dummy_push, db_handler)

    @retry(
        retry_errors=(RetryablePollDataNotReadyError,),
        max_retries=_COMP_POLL_MAX_RETRIES,
        delay_seconds=_COMP_POLL_RETRY_DELAY_SECONDS,
        operation_name="completed poll event after pubs not ready",
    )
    def comp_handler(document: DocumentSnapshot | None, pubs_list: PubsList) -> None:
        if document is None:
            raise ValueError(
                "Completed Event has no document. This indicates a coding error."
            )
        db_handler.complete_poll_event_handler(
            pubs_list, complete_am, poll_id=document.id
        )

    def comp_poll_event_callback(document: DocumentSnapshot) -> None:
        q.put(
            Event(
                type=EventType.COMP_POLL,
                doc=document,
                callback=comp_handler,
            )
        )

    def notification_request_callback(document: DocumentSnapshot) -> None:
        if notification_push_test.is_push_test_request(document):
            q.put(
                Event(
                    type=EventType.PUSH_TEST,
                    doc=document,
                    callback=lambda doc, pubs_list: notification_push_test.handle_request_document(doc),
                )
            )
        else:
            q.put(
                Event(
                    type=EventType.PUSH,
                    doc=document,
                    callback=lambda doc, pubs_list: notification_mirror.mirror_request_document(doc),
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

    def enqueue_housekeeping_tick() -> None:
        q.put(
            Event(
                type=EventType.TICK,
                doc=None,
                callback=lambda doc=None, pubs_list=None: housekeeping_runner.maybe_run(),
            )
        )
    if housekeeping_cron:
        _log.info("Housekeeping runner started (cron=%s)", housekeeping_cron)
        housekeeping_trigger = CroniterTrigger(
            cron_expression=housekeeping_cron,
            callback=enqueue_housekeeping_tick,
        )
    else:
        _log.info(
            "Housekeeping runner started (interval=%ss)",
            housekeeping_interval_seconds,
        )
        housekeeping_trigger = PeriodicTrigger(
            interval_seconds=housekeeping_interval_seconds,
            callback=enqueue_housekeeping_tick,
        )

    canary = CanaryWatcher(
        db_handler.db,
        timeout_seconds=canary_interval_seconds * 2,
    )
    canary_trigger = PeriodicTrigger(
        interval_seconds=canary_interval_seconds,
        callback=canary.send_canary,
    )

    with (
        PollManager(
            db_handler.query_polls_by_status(
                completed=False,
                min_date=poll_min_date,
            ),
            add=open_poll_event_callback,
        ),
        PollManager(
            db_handler.query_polls_by_status(
                completed=True,
                min_date=poll_min_date,
            ),
            add=comp_poll_event_callback,
            modify=comp_poll_event_callback,
        ),
        PollManager(
            db_handler.query_notification_requests,
            add=notification_request_callback,
            modify=notification_request_callback,
        ),
        housekeeping_trigger,
        canary,
        canary_trigger,
        PubsList(db_handler.pub_collection) as pubs_list,
    ):
        

        while True:
            try:
                event = q.get(timeout=healthcheck_interval_seconds)
            except queue.Empty:
                if not db_handler.okay:
                    raise SystemExit("Exiting due to db is not okay")
                if canary.is_stale():
                    raise SystemExit("Exiting due to stale Firestore listener (canary not observed)")
                continue

            event.handle_queue_item(
                pubs_list,
            )
            event_date, completed = doc_props(event)
            _log.info(
                f"Completed Event: Type:{event.type}, Date:{event_date}, Completed:{completed}"
            )


def doc_props(event: Event) -> tuple[str | None, bool]:
    if not event.doc:
        return None, False
    doc = event.doc.to_dict()
    if doc is None:
        _log.warning(
            "Received event %s for doc %s with no payload",
            event.type,
            event.doc.id,
        )
        return None, False
    event_date = doc.get("date")
    completed = doc.get("completed", False)
    _log.info(
        "New Event: Type:%s, Date:%s, Completed:%s",
        event.type,
        event_date,
        completed,
    )
    return event_date, completed


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
    help="Deprecated and ignored; periodic watch restarts are disabled",
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
@click.option(
    "--canary-interval-seconds",
    type=click.IntRange(min=10),
    default=300,
    show_default=True,
    help="How often (seconds) to write a canary nonce to verify Firestore listener health",
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
    canary_interval_seconds: int,
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
        canary_interval_seconds,
    )


if __name__ == "__main__":
    cli()
