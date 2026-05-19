from datetime import date

from firebase_sub.database.event_recurrence import (
    creation_window_start,
    event_poll_id,
    event_week_completion_start,
    materialized_next_occurrence_date,
    materialized_next_occurrence_iso_state,
    next_occurrence,
)
from firebase_sub.my_types import EventRecurrenceRule


def test_next_occurrence_once_uses_explicit_date():
    assert next_occurrence(
        {"frequency": "once", "date": "2026-08-23"},
        date(2026, 5, 14),
    ) == date(2026, 8, 23)


def test_next_occurrence_weekly_finds_next_matching_weekday():
    assert next_occurrence(
        {
            "frequency": "weekly",
            "start_date": "2026-05-04",
            "weekdays": [2],
        },
        date(2026, 5, 14),
    ) == date(2026, 5, 20)


def test_next_occurrence_monthly_last_wednesday():
    assert next_occurrence(
        {
            "frequency": "monthly",
            "weekday": 2,
            "nth": -1,
            "start_date": "2026-05-01",
        },
        date(2026, 5, 14),
    ) == date(2026, 5, 27)


def test_next_occurrence_yearly_fixed_date():
    assert next_occurrence(
        {"frequency": "yearly", "month": 8, "month_day": 23},
        date(2026, 5, 14),
    ) == date(2026, 8, 23)


def test_creation_window_and_completion_week_helpers():
    occurrence = date(2026, 8, 23)
    assert creation_window_start(occurrence) == date(2026, 8, 16)
    assert event_week_completion_start(occurrence) == date(2026, 8, 17)


def test_event_poll_id_is_deterministic():
    assert event_poll_id("cambridge-beer-festival", date(2026, 8, 23)) == (
        "event-cambridge-beer-festival-2026-08-23"
    )


def test_materialized_next_occurrence_recomputes_non_matching_future_value():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 5,
        "weekday": 2,
        "nth": 3,
        "start_date": "2026-05-01",
    }
    assert materialized_next_occurrence_date(
        recurrence,
        "2027-05-20",
        today=date(2026, 5, 18),
    ) == date(2026, 5, 20)


def test_materialized_next_occurrence_advances_when_week_is_complete():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 5,
        "weekday": 2,
        "nth": 3,
        "start_date": "2026-05-01",
    }
    assert materialized_next_occurrence_date(
        recurrence,
        "2026-05-21",
        today=date(2026, 5, 25),
    ) == date(2027, 5, 19)


def test_materialized_next_occurrence_keeps_same_week_future_date():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 5,
        "month_day": 20,
        "start_date": "2026-05-01",
    }
    assert materialized_next_occurrence_date(
        recurrence,
        "2026-05-20",
        today=date(2026, 5, 19),
    ) == date(2026, 5, 20)


def test_materialized_next_occurrence_keeps_today_date():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 5,
        "month_day": 20,
        "start_date": "2026-05-01",
    }
    assert materialized_next_occurrence_date(
        recurrence,
        "2026-05-20",
        today=date(2026, 5, 20),
    ) == date(2026, 5, 20)


def test_materialized_next_occurrence_keeps_valid_future_value():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 5,
        "weekday": 2,
        "nth": 3,
        "start_date": "2026-05-01",
    }
    assert materialized_next_occurrence_date(
        recurrence,
        "2027-05-19",
        today=date(2026, 5, 18),
    ) == date(2027, 5, 19)


def test_materialized_next_occurrence_recomputes_invalid_stored_value():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 8,
        "month_day": 23,
    }
    assert materialized_next_occurrence_date(
        recurrence,
        "2026-09-01",
        today=date(2026, 5, 14),
    ) == date(2026, 8, 23)


def test_materialized_next_occurrence_iso_state_normalizes_values():
    recurrence: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 8,
        "month_day": 23,
    }
    assert materialized_next_occurrence_iso_state(
        recurrence,
        "invalid-date",
        today=date(2026, 5, 14),
    ) == (None, "2026-08-23")
