import logging
from collections.abc import Callable
from pathlib import Path
from typing import TypeGuard

import click

from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.database.canary import CanaryWatcher
from firebase_sub.database.housekeeping import CroniterTrigger, PeriodicTrigger
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event
from firebase_sub.plugins.complete_poll import CompletePollListenerPlugin
from firebase_sub.plugins.plugin_config import (
    build_event_producer,
    build_event_registry,
    build_housekeeping_plugins,
    build_listener_plugins,
    build_scheduled_housekeeping_plugins,
)
from firebase_sub.plugins.protocols import EventPlugin, ListenerPlugin
from firebase_sub.plugins.runtime import PluginRuntime
from firebase_sub.plugins.scheduled_housekeeping import ScheduledHousekeepingRunner
from firebase_sub.runtime.action_policies import (
    poll_complete_actions,
    poll_open_actions,
)
from firebase_sub.runtime.config import RuntimeConfig
from firebase_sub.runtime.job_queue import JobQueue
from firebase_sub.runtime.queue_runner import QueueRunner
from firebase_sub.runtime.sub_events_bootstrap import get_db_handler

_log = logging.getLogger(__name__)


def _is_event_plugin(plugin: ListenerPlugin) -> TypeGuard[EventPlugin]:
    return (
        hasattr(plugin, "filter")
        and hasattr(plugin, "handle")
        and hasattr(plugin, "mark_done")
    )


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
    del restart_interval  # Retained as CLI compatibility flag.

    configure_logging(loglevel, logfile)

    runtime_config = RuntimeConfig.from_legacy_options(
        dummy_email=dummy_email,
        dummy_push=dummy_push,
        housekeeping_interval_seconds=housekeeping_interval_seconds,
        housekeeping_cron=housekeeping_cron,
        all_history=all_history,
        poll_lookback_days=poll_lookback_days,
        enable_real_auth_delete=False,
        admin_delete_enabled=True,
    )

    db_handler = get_db_handler()
    poll_min_date = runtime_config.poll_history.min_date()
    event_queue: JobQueue[Event] = JobQueue()
    notification_push_test = NotificationPushTestHandler(
        db_handler.db,
        db_handler.query_active_push_endpoints_for_user,
        dummy_push=runtime_config.dummy_push,
    )
    event_producer = build_event_producer(
        db_handler=db_handler,
        event_queue=event_queue,
        notification_push_test=notification_push_test,
        poll_min_date=poll_min_date,
    )

    open_action_manager = poll_open_actions(
        runtime_config.dummy_email,
        runtime_config.dummy_push,
        db_handler,
    )
    complete_action_manager = poll_complete_actions(
        runtime_config.dummy_email,
        runtime_config.dummy_push,
        db_handler,
    )

    listener_plugins = build_listener_plugins(
        db_handler=db_handler,
        open_action_manager=open_action_manager,
        complete_action_manager=complete_action_manager,
        runtime_config=runtime_config,
        comp_poll_max_retries=runtime_config.comp_poll_max_retries,
        comp_poll_retry_delay_seconds=runtime_config.comp_poll_retry_delay_seconds,
        notification_push_test=notification_push_test,
    )
    housekeeping_plugins = build_housekeeping_plugins(db=db_handler.db)
    scheduled_housekeeping_plugins = build_scheduled_housekeeping_plugins(
        db=db_handler.db
    )
    scheduled_runner = ScheduledHousekeepingRunner(list(scheduled_housekeeping_plugins))

    event_plugins: list[EventPlugin] = [
        plugin for plugin in listener_plugins if _is_event_plugin(plugin)
    ]
    event_registry = build_event_registry(event_plugins=event_plugins)

    with (
        event_producer.build_chat_message_manager(),
        event_producer.build_notification_request_manager(),
        event_producer.build_admin_delete_request_manager(),
        event_producer.build_new_poll_manager(),
        event_producer.build_complete_poll_manager(),
        PluginRuntime(
            listener_plugins=listener_plugins,
            housekeeping_plugins=housekeeping_plugins,
        ) as runtime,
        (
            CroniterTrigger(
                cron_expression=housekeeping_cron,
                callback=runtime.run_housekeeping,
            )
            if housekeeping_cron
            else PeriodicTrigger(
                interval_seconds=housekeeping_interval_seconds,
                callback=runtime.run_housekeeping,
            )
        ),
        CanaryWatcher(db_handler.db) as canary,
        PubsList(db_handler.pub_collection) as pubs_list,
    ):
        for plugin in event_plugins:
            if isinstance(plugin, CompletePollListenerPlugin):
                plugin.set_pubs_list(pubs_list)

        healthchecks: list[Callable[[], str | None]] = [
            lambda: None if db_handler.okay else "Exiting due to db is not okay",
            lambda: (
                "Exiting due to stale Firestore listener canary"
                if canary.is_stale()
                else None
            ),
        ]
        runner = QueueRunner(
            event_queue=event_queue,
            healthcheck_interval_seconds=runtime_config.healthcheck_interval_seconds,
            healthchecks=healthchecks,
            registry=event_registry,
            scheduled_runner=scheduled_runner,
        )
        _log.info("sub_events runtime started")
        runner.run_forever()


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
