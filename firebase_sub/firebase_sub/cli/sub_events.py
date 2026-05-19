import contextlib
import logging
import os
from pathlib import Path
from typing import cast

import click

from firebase_sub.common.logging import configure_logging, log_level_to_int
from firebase_sub.database.housekeeping import (
    CroniterTrigger,
    PeriodicTrigger,
)
from firebase_sub.database.canary import CanaryWatcher
from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event, EventType
from firebase_sub.plugins.plugin_config import (
    build_housekeeping_plugins,
    build_listener_plugins,
)
from firebase_sub.plugins.runtime import PluginRuntime
from firebase_sub.runtime.action_policies import (
    poll_complete_actions,
    poll_open_actions,
)
from firebase_sub.runtime.sub_events_bootstrap import get_db_handler
from firebase_sub.runtime.job_queue import JobQueue
from firebase_sub.runtime.queue_runner import QueueRunner
from firebase_sub.runtime.config import RuntimeConfig

_log = logging.getLogger(__name__)


def _env_flag(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def sub_events(
    dummy_email: bool,
    dummy_push: bool,
    loglevel: int,
    logfile: Path | None,
    housekeeping_interval_seconds: int,
    housekeeping_cron: str | None,
    all_history: bool,
    poll_lookback_days: int,
    canary_interval_seconds: int = 300,
    enable_real_auth_delete: bool = False,
) -> None:
    configure_logging(loglevel, logfile)

    runtime_config = RuntimeConfig.from_legacy_options(
        dummy_email=dummy_email,
        dummy_push=dummy_push,
        housekeeping_interval_seconds=housekeeping_interval_seconds,
        housekeeping_cron=housekeeping_cron,
        all_history=all_history,
        poll_lookback_days=poll_lookback_days,
        enable_real_auth_delete=enable_real_auth_delete,
        admin_delete_enabled=_env_flag("ENABLE_ADMIN_DELETE_REQUESTS", default=False),
    )
    db_handler = get_db_handler()
    q: JobQueue[Event] = JobQueue()

    open_am = poll_open_actions(
        runtime_config.dummy_email, runtime_config.dummy_push, db_handler
    )

    complete_am = poll_complete_actions(
        runtime_config.dummy_email,
        runtime_config.dummy_push,
        db_handler,
    )

    if runtime_config.admin_delete_enabled:
        _log.info(
            "Admin delete request listener enabled (dry_run=%s)",
            not runtime_config.enable_real_auth_delete,
        )
    else:
        _log.info(
            "Admin delete request listener disabled (set ENABLE_ADMIN_DELETE_REQUESTS=true to enable)"
        )
    if (
        runtime_config.enable_real_auth_delete
        and not runtime_config.admin_delete_enabled
    ):
        _log.warning(
            "--enable-real-auth-delete was set but ENABLE_ADMIN_DELETE_REQUESTS is false; admin delete processing remains disabled"
        )

    poll_min_date = runtime_config.poll_history.min_date()
    if poll_min_date is None:
        _log.info("Poll listeners using full history")
    else:
        _log.info(
            "Poll listeners using recent history (min_date=%s lookback_days=%s)",
            poll_min_date,
            runtime_config.poll_history.lookback_days,
        )

    listener_plugins = build_listener_plugins(
        db_handler=db_handler,
        event_queue=q,
        open_action_manager=open_am,
        complete_action_manager=complete_am,
        runtime_config=runtime_config,
        poll_min_date=poll_min_date,
        comp_poll_max_retries=runtime_config.comp_poll_max_retries,
        comp_poll_retry_delay_seconds=runtime_config.comp_poll_retry_delay_seconds,
    )
    housekeeping_plugins = build_housekeeping_plugins(db=db_handler.db)
    plugin_runtime = PluginRuntime(
        listener_plugins=listener_plugins,
        housekeeping_plugins=housekeeping_plugins,
    )

    def enqueue_housekeeping_tick() -> None:
        q.put(
            Event(
                type=EventType.TICK,
                doc=None,
                callback=lambda doc=None, pubs_list=None: plugin_runtime.run_housekeeping(),
            )
        )

    if runtime_config.housekeeping.uses_cron:
        _log.info(
            "Housekeeping runner started (cron=%s)",
            runtime_config.housekeeping.cron_expression,
        )
        housekeeping_trigger = CroniterTrigger(
            cron_expression=cast(str, runtime_config.housekeeping.cron_expression),
            callback=enqueue_housekeeping_tick,
        )
    else:
        _log.info(
            "Housekeeping runner started (interval=%ss)",
            runtime_config.housekeeping.interval_seconds,
        )
        housekeeping_trigger = PeriodicTrigger(
            interval_seconds=runtime_config.housekeeping.interval_seconds,
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

    with contextlib.ExitStack() as stack:
        stack.enter_context(plugin_runtime)
        stack.enter_context(housekeeping_trigger)
        stack.enter_context(canary)
        stack.enter_context(canary_trigger)
        pubs_list = stack.enter_context(PubsList(db_handler.pub_collection))

        QueueRunner(
            event_queue=q,
            pubs_list=pubs_list,
            healthcheck_interval_seconds=runtime_config.healthcheck_interval_seconds,
            healthchecks=[
                lambda: None if db_handler.okay else "Exiting due to db is not okay",
                lambda: (
                    "Exiting due to stale Firestore listener (canary not observed)"
                    if canary.is_stale()
                    else None
                ),
            ],
        ).run_forever()


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
@click.option(
    "--enable-real-auth-delete/--no-enable-real-auth-delete",
    default=False,
    show_default=True,
    help="Allow real Firebase Auth deletion for validated requests (requires ENABLE_ADMIN_DELETE_REQUESTS=true)",
)
def cli(
    dummy_email: bool,
    dummy_push: bool,
    loglevel: int,
    logfile: Path | None,
    housekeeping_interval_seconds: int,
    housekeeping_cron: str | None,
    all_history: bool,
    poll_lookback_days: int,
    canary_interval_seconds: int,
    enable_real_auth_delete: bool,
) -> None:
    sub_events(
        dummy_email,
        dummy_push,
        loglevel,
        logfile,
        housekeeping_interval_seconds,
        housekeeping_cron,
        all_history,
        poll_lookback_days,
        canary_interval_seconds,
        enable_real_auth_delete,
    )


if __name__ == "__main__":
    cli()
