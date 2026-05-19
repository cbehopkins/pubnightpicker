from datetime import date, timedelta

from firebase_sub.my_types import EventRecurrenceRule


def parse_iso_date(value: object | None) -> date | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        return date.fromisoformat(value)
    except ValueError:
        return None


def week_start(value: date) -> date:
    return value - timedelta(days=value.weekday())


def event_poll_id(venue_id: str, occurrence_date: date) -> str:
    return f"event-{venue_id}-{occurrence_date.isoformat()}"


def _days_in_month(year: int, month: int) -> int:
    if month == 12:
        next_month = date(year + 1, 1, 1)
    else:
        next_month = date(year, month + 1, 1)
    return (next_month - timedelta(days=1)).day


def _add_months(value: date, months: int) -> date:
    month_index = value.month - 1 + months
    year = value.year + month_index // 12
    month = month_index % 12 + 1
    day = min(value.day, _days_in_month(year, month))
    return date(year, month, day)


def _nth_weekday_of_month(year: int, month: int, weekday: int, nth: int) -> date | None:
    if not 0 <= weekday <= 6 or nth == 0:
        return None

    if nth > 0:
        first_day = date(year, month, 1)
        days_until_weekday = (weekday - first_day.weekday()) % 7
        candidate = first_day + timedelta(days=days_until_weekday + (nth - 1) * 7)
        return candidate if candidate.month == month else None

    last_day = date(year, month, _days_in_month(year, month))
    days_back = (last_day.weekday() - weekday) % 7
    candidate = last_day - timedelta(days=days_back)
    return candidate if candidate.month == month else None


def _first_weekday_on_or_after(start: date, weekdays: list[int]) -> date | None:
    candidate_weekdays = sorted({weekday for weekday in weekdays if 0 <= weekday <= 6})
    if not candidate_weekdays:
        return None
    candidate = start
    for _ in range(14):
        if candidate.weekday() in candidate_weekdays:
            return candidate
        candidate += timedelta(days=1)
    return None


def next_occurrence(
    recurrence: EventRecurrenceRule,
    reference_date: date,
) -> date | None:
    frequency = recurrence.get("frequency", "once")

    if frequency == "once":
        return parse_iso_date(recurrence.get("date"))

    anchor_date = parse_iso_date(recurrence.get("start_date")) or reference_date
    interval = max(int(recurrence.get("interval", 1)), 1)

    if frequency == "weekly":
        weekdays = recurrence.get("weekdays")
        if weekdays is None:
            weekday = recurrence.get("weekday")
            weekdays = [weekday] if weekday is not None else []

        search_date = max(reference_date, anchor_date)
        for _ in range(366 * 20):
            if search_date < anchor_date:
                search_date += timedelta(days=1)
                continue

            weeks_since_anchor = (search_date - anchor_date).days // 7
            if weeks_since_anchor % interval == 0:
                candidate = _first_weekday_on_or_after(search_date, weekdays)
                if candidate is not None:
                    candidate_weeks_since_anchor = (candidate - anchor_date).days // 7
                    if candidate_weeks_since_anchor % interval == 0:
                        return candidate

            search_date += timedelta(days=1)
        return None

    if frequency == "monthly":
        weekday = recurrence.get("weekday")
        month_day = recurrence.get("month_day")
        nth = int(recurrence.get("nth", 1))
        search_date = date(reference_date.year, reference_date.month, 1)
        anchor_month = date(anchor_date.year, anchor_date.month, 1)

        for _ in range(240):
            if search_date < anchor_month:
                search_date = _add_months(search_date, 1)
                continue

            months_since_anchor = (
                (search_date.year - anchor_month.year) * 12
                + search_date.month
                - anchor_month.month
            )
            if months_since_anchor % interval != 0:
                search_date = _add_months(search_date, 1)
                continue

            if month_day is not None:
                if (
                    not 1
                    <= month_day
                    <= _days_in_month(search_date.year, search_date.month)
                ):
                    search_date = _add_months(search_date, 1)
                    continue
                candidate = date(search_date.year, search_date.month, month_day)
            elif weekday is not None:
                candidate = _nth_weekday_of_month(
                    search_date.year, search_date.month, weekday, nth
                )
            else:
                return None

            if candidate is not None and candidate >= reference_date:
                return candidate

            search_date = _add_months(search_date, 1)
        return None

    if frequency == "yearly":
        month = recurrence.get("month")
        if month is None or not 1 <= month <= 12:
            return None

        month_day = recurrence.get("month_day")
        weekday = recurrence.get("weekday")
        nth = int(recurrence.get("nth", 1))

        for year in range(reference_date.year, reference_date.year + 40):
            if year < anchor_date.year:
                continue

            if month_day is not None:
                if not 1 <= month_day <= _days_in_month(year, month):
                    continue
                candidate = date(year, month, month_day)
            elif weekday is not None:
                candidate = _nth_weekday_of_month(year, month, weekday, nth)
            else:
                return None

            if candidate is not None and candidate >= reference_date:
                return candidate

        return None

    raise ValueError(f"Unknown recurrence frequency: {frequency!r}")


def _matches_recurrence(recurrence: EventRecurrenceRule, candidate: date) -> bool:
    return next_occurrence(recurrence, candidate) == candidate


def _materialized_next_occurrence_from_current_date(
    recurrence: EventRecurrenceRule | None,
    current_date: date | None,
    *,
    today: date,
) -> date | None:
    if recurrence is None:
        return None

    if current_date is not None and _matches_recurrence(recurrence, current_date):
        if current_date < today and today >= event_week_completion_start(current_date):
            return next_occurrence(recurrence, current_date + timedelta(days=1))
        if current_date >= today:
            return current_date

    return next_occurrence(recurrence, today)


def materialized_next_occurrence_date(
    recurrence: EventRecurrenceRule | None,
    current_value: object,
    *,
    today: date,
) -> date | None:
    """Resolve the canonical next occurrence date for storage.

    This keeps valid future values stable while still allowing fast updates after
    recurrence edits and deterministic roll-forward once a week is completed.
    """
    current_date = parse_iso_date(current_value)
    return _materialized_next_occurrence_from_current_date(
        recurrence,
        current_date,
        today=today,
    )


def materialized_next_occurrence_iso_state(
    recurrence: EventRecurrenceRule | None,
    current_value: object,
    *,
    today: date,
) -> tuple[str | None, str | None]:
    """Return normalized current/target ISO dates for next_occurrence_date."""
    current_date = parse_iso_date(current_value)
    next_date = _materialized_next_occurrence_from_current_date(
        recurrence,
        current_date,
        today=today,
    )
    current_iso = current_date.isoformat() if current_date is not None else None
    next_iso = next_date.isoformat() if next_date is not None else None
    return current_iso, next_iso


def creation_window_start(occurrence_date: date, lead_days: int = 7) -> date:
    if lead_days < 0:
        raise ValueError("lead_days must be >= 0")
    return occurrence_date - timedelta(days=lead_days)


def event_week_completion_start(occurrence_date: date) -> date:
    return week_start(occurrence_date)
