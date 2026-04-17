from dataclasses import dataclass

PUSH_EVENT_POLL_OPENED = "poll_opened"
PUSH_EVENT_POLL_COMPLETED = "poll_completed"
PUSH_EVENT_POLL_RESCHEDULED = "poll_rescheduled"
PUSH_EVENT_DIAGNOSTIC_PUSH_TEST = "diagnostic_push_test"


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
