from collections.abc import Sequence

from google.cloud.firestore_v1.client import Client

from firebase_sub.database.admin_delete_requests import AdminDeleteRequestHandler
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.database.housekeeping_tasks import (
    delete_inactive_push_endpoints,
    delete_notification_diagnostics,
    delete_notification_docs_for_past_polls,
    delete_stale_poll_action_audit_entries,
    delete_stale_push_diagnostic_entries,
    maintain_event_recurrence_polls,
)
from firebase_sub.event import Event
from firebase_sub.plugins.admin_delete_request import AdminDeleteRequestListenerPlugin
from firebase_sub.plugins.chat_message import ChatMessageListenerPlugin
from firebase_sub.plugins.complete_poll import CompletePollListenerPlugin
from firebase_sub.plugins.housekeeping import HousekeepingCallablePlugin
from firebase_sub.plugins.new_poll import NewPollListenerPlugin
from firebase_sub.plugins.notification_request import NotificationRequestListenerPlugin
from firebase_sub.action_track import ActionMan
from firebase_sub.plugins.protocols import HousekeepingPlugin, ListenerPlugin
from firebase_sub.runtime.config import RuntimeConfig
from firebase_sub.runtime.job_queue import JobQueue


def build_listener_plugins(
    *,
    db_handler: DbHandler,
    event_queue: JobQueue[Event],
    open_action_manager: ActionMan,
    complete_action_manager: ActionMan,
    runtime_config: RuntimeConfig,
    poll_min_date: str | None,
    comp_poll_max_retries: int,
    comp_poll_retry_delay_seconds: float,
) -> Sequence[ListenerPlugin]:
    """Explicit in-code listener plugin configuration."""
    notification_mirror = NotificationAckMirrorHandler(db_handler.db)
    notification_push_test = NotificationPushTestHandler(
        db_handler.db,
        db_handler.query_active_push_endpoints_for_user,
        dummy_push=runtime_config.dummy_push,
    )
    admin_delete_handler = AdminDeleteRequestHandler(
        db_handler.db,
        enabled=runtime_config.admin_delete_enabled,
        dry_run=not runtime_config.enable_real_auth_delete,
        enable_real_auth_delete=runtime_config.enable_real_auth_delete,
    )
    return [
        NewPollListenerPlugin(
            db_handler=db_handler,
            event_queue=event_queue,
            action_manager=open_action_manager,
            min_date=poll_min_date,
        ),
        CompletePollListenerPlugin(
            db_handler=db_handler,
            event_queue=event_queue,
            action_manager=complete_action_manager,
            min_date=poll_min_date,
            max_retries=comp_poll_max_retries,
            retry_delay_seconds=comp_poll_retry_delay_seconds,
        ),
        NotificationRequestListenerPlugin(
            query_notification_requests=db_handler.query_notification_requests,
            event_queue=event_queue,
            notification_mirror=notification_mirror,
            notification_push_test=notification_push_test,
        ),
        AdminDeleteRequestListenerPlugin(
            query_admin_delete_requests=db_handler.query_admin_delete_requests,
            event_queue=event_queue,
            handler=admin_delete_handler,
        ),
        ChatMessageListenerPlugin(
            query_messages=db_handler.query_messages,
            db_handler=db_handler,
            event_queue=event_queue,
            dummy_run=runtime_config.dummy_push,
        ),
    ]


def build_housekeeping_plugins(*, db: Client) -> Sequence[HousekeepingPlugin]:
    """Build explicit in-code housekeeping plugin registrations."""
    return [
        HousekeepingCallablePlugin(
            name="delete_notification_diagnostics",
            callback=lambda: delete_notification_diagnostics(db),
        ),
        HousekeepingCallablePlugin(
            name="delete_notification_docs_for_past_polls",
            callback=lambda: delete_notification_docs_for_past_polls(db),
        ),
        HousekeepingCallablePlugin(
            name="delete_inactive_push_endpoints",
            callback=lambda: delete_inactive_push_endpoints(db),
        ),
        HousekeepingCallablePlugin(
            name="delete_stale_push_diagnostic_entries",
            callback=lambda: delete_stale_push_diagnostic_entries(db),
        ),
        HousekeepingCallablePlugin(
            name="delete_stale_poll_action_audit_entries",
            callback=lambda: delete_stale_poll_action_audit_entries(db),
        ),
        HousekeepingCallablePlugin(
            name="maintain_event_recurrence_polls",
            callback=lambda: maintain_event_recurrence_polls(db),
        ),
    ]
