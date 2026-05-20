from collections.abc import Sequence

from google.cloud.firestore_v1.client import Client

from firebase_sub.action_track import ActionMan
from firebase_sub.database.admin_delete_requests import AdminDeleteRequestHandler
from firebase_sub.database.handlers import DbHandler
from firebase_sub.database.housekeeping_tasks import (
    auto_complete_multi_option_polls_due_today,
    auto_complete_single_event_polls_due_tomorrow,
    delete_inactive_push_endpoints,
    delete_notification_diagnostics,
    delete_notification_docs_for_past_polls,
    delete_stale_poll_action_audit_entries,
    delete_stale_push_diagnostic_entries,
    maintain_event_recurrence_polls,
)
from firebase_sub.database.notification_mirror import NotificationAckMirrorHandler
from firebase_sub.database.notification_push_diag import NotificationPushTestHandler
from firebase_sub.event import Event, EventType
from firebase_sub.plugins.admin_delete_request import AdminDeleteRequestListenerPlugin
from firebase_sub.plugins.chat_message import ChatMessageListenerPlugin
from firebase_sub.plugins.complete_poll import CompletePollListenerPlugin
from firebase_sub.plugins.housekeeping import (
    DailyUtcScheduledCallablePlugin,
    HousekeepingCallablePlugin,
)
from firebase_sub.plugins.new_poll import NewPollListenerPlugin
from firebase_sub.plugins.notification_request import NotificationRequestListenerPlugin
from firebase_sub.plugins.protocols import (
    EventPlugin,
    HousekeepingPlugin,
    ListenerPlugin,
)
from firebase_sub.runtime.config import RuntimeConfig
from firebase_sub.runtime.event_producers import EventProducer
from firebase_sub.runtime.event_registry import EventRegistry
from firebase_sub.runtime.job_queue import JobQueue


def build_listener_plugins(
    *,
    db_handler: DbHandler,
    open_action_manager: ActionMan,
    complete_action_manager: ActionMan,
    runtime_config: RuntimeConfig,
    comp_poll_max_retries: int,
    comp_poll_retry_delay_seconds: float,
    notification_push_test: NotificationPushTestHandler,
) -> Sequence[ListenerPlugin]:
    """Explicit in-code listener plugin configuration.

    Plugins no longer query the database directly. Event producers are responsible
    for querying and creating events, which are then consumed by plugins.
    """
    notification_mirror = NotificationAckMirrorHandler(db_handler.db)
    admin_delete_handler = AdminDeleteRequestHandler(
        db_handler.db,
        enabled=runtime_config.admin_delete_enabled,
        dry_run=not runtime_config.enable_real_auth_delete,
        enable_real_auth_delete=runtime_config.enable_real_auth_delete,
    )
    return [
        NewPollListenerPlugin(
            db_handler=db_handler,
            action_manager=open_action_manager,
        ),
        CompletePollListenerPlugin(
            db_handler=db_handler,
            action_manager=complete_action_manager,
            max_retries=comp_poll_max_retries,
            retry_delay_seconds=comp_poll_retry_delay_seconds,
        ),
        NotificationRequestListenerPlugin(
            notification_mirror=notification_mirror,
            notification_push_test=notification_push_test,
        ),
        AdminDeleteRequestListenerPlugin(
            handler=admin_delete_handler,
        ),
        ChatMessageListenerPlugin(
            db_handler=db_handler,
            dummy_run=runtime_config.dummy_push,
        ),
    ]


def build_event_registry(
    *,
    event_plugins: Sequence[EventPlugin],
) -> EventRegistry:
    """Build event plugin registry for dispatcher-based routing.

    Subscribes EventPlugin instances to their respective EventType handlers.
    """
    registry = EventRegistry()

    for plugin in event_plugins:
        if isinstance(plugin, NewPollListenerPlugin):
            registry.subscribe(EventType.NEW_POLL, plugin)
            continue
        if isinstance(plugin, CompletePollListenerPlugin):
            registry.subscribe(EventType.COMP_POLL, plugin)
            continue
        if isinstance(plugin, NotificationRequestListenerPlugin):
            registry.subscribe(EventType.PUSH, plugin)
            registry.subscribe(EventType.PUSH_TEST, plugin)
            continue
        if isinstance(plugin, ChatMessageListenerPlugin):
            registry.subscribe(EventType.CHAT_MESSAGE, plugin)
            continue
        if isinstance(plugin, AdminDeleteRequestListenerPlugin):
            registry.subscribe(EventType.ADMIN_DELETE_REQUEST, plugin)
            continue

        raise ValueError(
            "Unsupported EventPlugin registration for "
            f"{plugin.__class__.__name__} ({plugin.name()})"
        )

    return registry


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


def build_scheduled_housekeeping_plugins(
    *,
    db: Client,
) -> Sequence[HousekeepingPlugin]:
    """Build explicit in-code scheduled housekeeping registrations."""
    return [
        DailyUtcScheduledCallablePlugin(
            name="auto_complete_single_event_polls_due_tomorrow",
            callback=lambda: auto_complete_single_event_polls_due_tomorrow(db),
            hour=0,
            minute=1,
        ),
        DailyUtcScheduledCallablePlugin(
            name="auto_complete_multi_option_polls_due_today",
            callback=lambda: auto_complete_multi_option_polls_due_today(db),
            hour=16,
            minute=0,
        ),
    ]


def build_event_producer(
    *,
    db_handler: DbHandler,
    event_queue: JobQueue[Event],
    notification_push_test: NotificationPushTestHandler,
    poll_min_date: str | None = None,
) -> EventProducer:
    """Build the single runtime event producer that feeds the queue."""
    return EventProducer(
        db_handler=db_handler,
        event_queue=event_queue,
        notification_push_test=notification_push_test,
        new_poll_db_handler=db_handler,
        complete_poll_db_handler=db_handler,
        min_date=poll_min_date,
    )
