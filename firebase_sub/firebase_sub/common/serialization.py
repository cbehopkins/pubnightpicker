import re
from datetime import datetime, timezone
from typing import Any

from google.api_core.datetime_helpers import DatetimeWithNanoseconds

ISO8601_REGEX = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$"
)


def restore_datetimes(obj: Any) -> Any:
    # ISO 8601 regex for datetime with optional microseconds and timezone
    if isinstance(obj, dict):
        return {k: restore_datetimes(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [restore_datetimes(i) for i in obj]
    if isinstance(obj, str) and ISO8601_REGEX.match(obj):
        try:
            # Handle 'Z' as UTC
            if obj.endswith("Z"):
                return datetime.fromisoformat(obj[:-1]).replace(tzinfo=timezone.utc)
            return datetime.fromisoformat(obj)
        except Exception:
            return obj
    return obj


def convert_datetimes(obj: Any) -> Any:
    if isinstance(obj, dict):
        return {k: convert_datetimes(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [convert_datetimes(i) for i in obj]
    if isinstance(obj, (DatetimeWithNanoseconds, datetime)):
        return obj.isoformat()
    return obj
