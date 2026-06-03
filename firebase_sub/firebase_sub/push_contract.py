from dataclasses import dataclass

PUSH_EVENT_POLL_OPENED = "poll_opened"
PUSH_EVENT_POLL_COMPLETED = "poll_completed"
PUSH_EVENT_POLL_RESCHEDULED = "poll_rescheduled"
PUSH_EVENT_POLL_MANUAL_COMPLETION_REQUIRED = "poll_manual_completion_required"
PUSH_EVENT_DIAGNOSTIC_PUSH_TEST = "diagnostic_push_test"
PUSH_EVENT_CHAT_MESSAGE_GLOBAL = "chat_message_sent_global"
PUSH_EVENT_CHAT_MESSAGE_EVENT = "chat_message_sent_event"

# Maps each push event type to the pushPreferences field that gates it.
PUSH_PREFERENCE_FIELD: dict[str, str] = {
    PUSH_EVENT_POLL_OPENED: "pollOpens",
    PUSH_EVENT_POLL_COMPLETED: "pollCompletes",
    PUSH_EVENT_POLL_RESCHEDULED: "pollCompletes",
    PUSH_EVENT_POLL_MANUAL_COMPLETION_REQUIRED: "pollCompletes",
    PUSH_EVENT_CHAT_MESSAGE_GLOBAL: "globalChat",
    PUSH_EVENT_CHAT_MESSAGE_EVENT: "eventChat",
}

# Migration defaults: when pushPreferences is absent from a user doc, use these.
PUSH_PREFERENCE_DEFAULTS: dict[str, bool] = {
    "pollOpens": True,
    "pollCompletes": True,
    "globalChat": False,
    "eventChat": False,
}


@dataclass(frozen=True)
class PushDedupeKeys:
    @staticmethod
    def open_key(poll_id: str) -> str:
        return f"open:{poll_id}"

    @staticmethod
    def complete_key(
        pub_id: str,
        restaurant_id: str | None,
        restaurant_time: str | None,
    ) -> str:
        normalized_restaurant = restaurant_id or ""
        normalized_time = restaurant_time or ""
        if not normalized_restaurant and not normalized_time:
            return pub_id
        return f"{pub_id}:{normalized_restaurant}:{normalized_time}"
