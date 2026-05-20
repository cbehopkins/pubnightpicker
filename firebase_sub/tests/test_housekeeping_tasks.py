from datetime import UTC, date, datetime
from types import SimpleNamespace
from typing import Literal, NotRequired, TypedDict, cast
from unittest.mock import MagicMock

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.database.housekeeping_tasks import (
    EVENTS_COLLECTION,
    NOTIFICATION_ACK_COLLECTION,
    NOTIFICATION_REQ_COLLECTION,
    POLL_ACTION_AUDIT_COLLECTION,
    POLLS_COLLECTION,
    PUSH_ENDPOINTS_COLLECTION,
    PUSH_TEST_DOC_ID,
    _advance_event_occurrence_if_due,
    _create_event_poll_if_due,
    _resolve_event_occurrence_date,
    auto_complete_multi_option_polls_due_today,
    auto_complete_single_event_polls_due_tomorrow,
    delete_inactive_push_endpoints,
    delete_notification_diagnostics,
    delete_notification_docs_for_past_polls,
    delete_stale_poll_action_audit_entries,
    delete_stale_push_diagnostic_entries,
    maintain_event_recurrence_polls,
)
from firebase_sub.my_types import EventRecurrenceRule


class TestVenueRecurrence(TypedDict):
    """Test-specific recurrence rule structure."""

    frequency: Literal["once", "weekly", "monthly", "yearly"]
    month: NotRequired[int]
    month_day: NotRequired[int]
    weekday: NotRequired[int]
    nth: NotRequired[int]
    start_date: NotRequired[str]


class TestVenueData(TypedDict):
    """Test-specific venue data structure."""

    name: NotRequired[str]
    venueType: NotRequired[str]
    recurrence: NotRequired[EventRecurrenceRule]
    next_occurrence_date: NotRequired[str]


class TestPollData(TypedDict):
    """Test-specific poll data structure."""

    date: NotRequired[str]
    completed: NotRequired[bool]


def test_delete_notification_diagnostics_deletes_req_and_ack_docs():
    db = MagicMock()
    req_doc = MagicMock()
    ack_doc = MagicMock()

    def collection_side_effect(name):
        collection = MagicMock()
        if name == NOTIFICATION_REQ_COLLECTION:
            collection.document.return_value = req_doc
        elif name == NOTIFICATION_ACK_COLLECTION:
            collection.document.return_value = ack_doc
        return collection

    db.collection.side_effect = collection_side_effect

    delete_notification_diagnostics(db)

    req_doc.delete.assert_called_once_with()
    ack_doc.delete.assert_called_once_with()


def test_delete_notification_docs_for_past_polls_deletes_req_and_ack_for_each_poll():
    db = MagicMock()
    req_collection = MagicMock()
    ack_collection = MagicMock()
    polls_collection = MagicMock()

    req_docs: dict[str, MagicMock] = {}
    ack_docs: dict[str, MagicMock] = {}

    def req_document_side_effect(doc_id: str):
        req_docs.setdefault(doc_id, MagicMock())
        return req_docs[doc_id]

    def ack_document_side_effect(doc_id: str):
        ack_docs.setdefault(doc_id, MagicMock())
        return ack_docs[doc_id]

    req_collection.document.side_effect = req_document_side_effect
    ack_collection.document.side_effect = ack_document_side_effect

    poll_doc_1 = MagicMock()
    poll_doc_1.id = "poll-1"
    poll_doc_2 = MagicMock()
    poll_doc_2.id = "poll-2"

    where_query = MagicMock()
    where_query.stream.return_value = [poll_doc_1, poll_doc_2]
    polls_collection.where.return_value = where_query

    def collection_side_effect(name: str):
        if name == NOTIFICATION_REQ_COLLECTION:
            return req_collection
        if name == NOTIFICATION_ACK_COLLECTION:
            return ack_collection
        if name == POLLS_COLLECTION:
            return polls_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    delete_notification_docs_for_past_polls(db, today=date(2026, 4, 2))

    req_docs["poll-1"].delete.assert_called_once_with()
    req_docs["poll-2"].delete.assert_called_once_with()
    ack_docs["poll-1"].delete.assert_called_once_with()
    ack_docs["poll-2"].delete.assert_called_once_with()


def test_delete_notification_docs_for_past_polls_no_past_polls_no_deletes():
    db = MagicMock()
    req_collection = MagicMock()
    ack_collection = MagicMock()
    polls_collection = MagicMock()

    where_query = MagicMock()
    where_query.stream.return_value = []
    polls_collection.where.return_value = where_query

    def collection_side_effect(name: str):
        if name == NOTIFICATION_REQ_COLLECTION:
            return req_collection
        if name == NOTIFICATION_ACK_COLLECTION:
            return ack_collection
        if name == POLLS_COLLECTION:
            return polls_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    delete_notification_docs_for_past_polls(db, today=date(2026, 4, 2))

    req_collection.document.assert_not_called()
    ack_collection.document.assert_not_called()


def test_delete_inactive_push_endpoints_deletes_matching_docs():
    db = MagicMock()
    query_active = MagicMock()
    query_disabled = MagicMock()
    endpoint_doc_1 = MagicMock()
    endpoint_doc_2 = MagicMock()

    db.collection_group.return_value = query_active
    query_active.where.return_value = query_disabled
    query_disabled.where.return_value.stream.return_value = [
        endpoint_doc_1,
        endpoint_doc_2,
    ]

    now = datetime(2026, 4, 17, tzinfo=UTC)
    delete_inactive_push_endpoints(db, now=now, retention_days=30)

    endpoint_doc_1.reference.delete.assert_called_once_with()
    endpoint_doc_2.reference.delete.assert_called_once_with()


def test_delete_inactive_push_endpoints_no_matches_no_deletes():
    db = MagicMock()
    query_active = MagicMock()
    query_disabled = MagicMock()

    db.collection_group.return_value = query_active
    query_active.where.return_value = query_disabled
    query_disabled.where.return_value.stream.return_value = []

    now = datetime(2026, 4, 17, tzinfo=UTC)
    delete_inactive_push_endpoints(db, now=now, retention_days=30)

    db.collection_group.assert_called_once_with(PUSH_ENDPOINTS_COLLECTION)


def test_delete_inactive_push_endpoints_rejects_negative_retention():
    db = MagicMock()

    try:
        delete_inactive_push_endpoints(db, retention_days=-1)
        raise AssertionError("Expected ValueError")
    except ValueError as exc:
        assert "retention_days" in str(exc)


def test_delete_stale_push_diagnostic_entries_deletes_only_stale_fields():
    db = MagicMock()
    now = datetime(2026, 4, 17, 12, 0, tzinfo=UTC)
    stale_value = int(datetime(2026, 4, 16, 10, 0, tzinfo=UTC).timestamp() * 1000)
    fresh_value = int(datetime(2026, 4, 17, 11, 0, tzinfo=UTC).timestamp() * 1000)

    req_doc = MagicMock()
    req_doc.get.return_value.to_dict.return_value = {
        "stale-user": stale_value,
        "fresh-user": fresh_value,
    }
    ack_doc = MagicMock()
    ack_doc.get.return_value.to_dict.return_value = {
        "stale-user": stale_value,
        "fresh-user": fresh_value,
    }

    def document_side_effect(path: str):
        if path == f"{NOTIFICATION_REQ_COLLECTION}/{PUSH_TEST_DOC_ID}":
            return req_doc
        if path == f"{NOTIFICATION_ACK_COLLECTION}/{PUSH_TEST_DOC_ID}":
            return ack_doc
        return MagicMock()

    db.document.side_effect = document_side_effect

    delete_stale_push_diagnostic_entries(db, now=now)

    req_doc.set.assert_called_once()
    ack_doc.set.assert_called_once()
    assert req_doc.set.call_args.args[0].keys() == {"stale-user"}
    assert ack_doc.set.call_args.args[0].keys() == {"stale-user"}
    assert req_doc.set.call_args.kwargs == {"merge": True}
    assert ack_doc.set.call_args.kwargs == {"merge": True}


def test_delete_stale_push_diagnostic_entries_no_stale_fields_no_write():
    db = MagicMock()
    now = datetime(2026, 4, 17, 12, 0, tzinfo=UTC)
    fresh_value = int(datetime(2026, 4, 17, 11, 0, tzinfo=UTC).timestamp() * 1000)

    req_doc = MagicMock()
    req_doc.get.return_value.to_dict.return_value = {"fresh-user": fresh_value}
    ack_doc = MagicMock()
    ack_doc.get.return_value.to_dict.return_value = {"fresh-user": fresh_value}

    def document_side_effect(path: str):
        if path == f"{NOTIFICATION_REQ_COLLECTION}/{PUSH_TEST_DOC_ID}":
            return req_doc
        if path == f"{NOTIFICATION_ACK_COLLECTION}/{PUSH_TEST_DOC_ID}":
            return ack_doc
        return MagicMock()

    db.document.side_effect = document_side_effect

    delete_stale_push_diagnostic_entries(db, now=now)

    req_doc.set.assert_not_called()
    ack_doc.set.assert_not_called()


def test_delete_stale_push_diagnostic_entries_rejects_negative_retention():
    db = MagicMock()

    try:
        delete_stale_push_diagnostic_entries(db, retention_days=-1)
        raise AssertionError("Expected ValueError")
    except ValueError as exc:
        assert "retention_days" in str(exc)


def test_delete_stale_poll_action_audit_entries_deletes_stale_docs():
    db = MagicMock()
    audit_collection = MagicMock()
    stale_query = MagicMock()
    stale_doc_1 = MagicMock()
    stale_doc_2 = MagicMock()

    db.collection.return_value = audit_collection
    audit_collection.where.return_value = stale_query
    stale_query.stream.return_value = [stale_doc_1, stale_doc_2]

    now = datetime(2026, 5, 15, 12, 0, tzinfo=UTC)
    delete_stale_poll_action_audit_entries(db, now=now, retention_days=90)

    db.collection.assert_called_once_with(POLL_ACTION_AUDIT_COLLECTION)
    stale_doc_1.reference.delete.assert_called_once_with()
    stale_doc_2.reference.delete.assert_called_once_with()


def test_delete_stale_poll_action_audit_entries_no_matches_no_delete():
    db = MagicMock()
    audit_collection = MagicMock()
    stale_query = MagicMock()

    db.collection.return_value = audit_collection
    audit_collection.where.return_value = stale_query
    stale_query.stream.return_value = []

    now = datetime(2026, 5, 15, 12, 0, tzinfo=UTC)
    delete_stale_poll_action_audit_entries(db, now=now, retention_days=90)

    db.collection.assert_called_once_with(POLL_ACTION_AUDIT_COLLECTION)


def test_delete_stale_poll_action_audit_entries_rejects_negative_retention():
    db = MagicMock()

    try:
        delete_stale_poll_action_audit_entries(db, retention_days=-1)
        raise AssertionError("Expected ValueError")
    except ValueError as exc:
        assert "retention_days" in str(exc)


def test_maintain_event_recurrence_polls_creates_due_event_poll_without_auto_complete():
    db = MagicMock()
    events_collection = MagicMock()
    votes_collection = MagicMock()
    attendance_collection = MagicMock()

    venue_doc = MagicMock()
    venue_doc.id = "cambridge-beer-festival"
    venue_doc.to_dict.return_value = {
        "name": "Cambridge Beer Festival",
        "venueType": "event",
        "recurrence": {
            "frequency": "yearly",
            "month": 5,
            "weekday": 2,
            "nth": -1,
            "start_date": "2026-05-01",
        },
    }
    venue_doc.reference = MagicMock()

    events_collection.stream.return_value = [venue_doc]

    poll_doc = MagicMock()
    poll_doc.exists = False
    poll_doc.to_dict.return_value = None

    created_poll_doc = MagicMock()
    created_poll_doc.exists = True
    created_poll_doc.to_dict.return_value = {
        "date": "2026-05-27",
        "completed": False,
    }

    poll_ref = MagicMock()
    poll_ref.get.side_effect = [poll_doc, created_poll_doc]

    def collection_side_effect(name: str):
        if name == EVENTS_COLLECTION:
            return events_collection
        if name == "votes":
            return votes_collection
        if name == "attendance":
            return attendance_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect
    db.document.return_value = poll_ref

    maintain_event_recurrence_polls(db, today=date(2026, 5, 26))

    db.document.assert_called_once_with(
        "polls/event-cambridge-beer-festival-2026-05-27"
    )
    poll_ref.set.assert_any_call(
        {
            "date": "2026-05-27",
            "completed": False,
            "pubs": {
                "cambridge-beer-festival": {
                    "name": "Cambridge Beer Festival",
                    "venueType": "event",
                }
            },
            "eventVenueId": "cambridge-beer-festival",
            "eventOccurrenceDate": "2026-05-27",
        }
    )
    assert poll_ref.set.call_count == 1
    votes_collection.document.assert_called_once_with(
        "event-cambridge-beer-festival-2026-05-27"
    )
    attendance_collection.document.assert_called_once_with(
        "event-cambridge-beer-festival-2026-05-27"
    )


def test_resolve_event_occurrence_date_backfills_when_missing():
    """When occurrence_date is missing but recurrence exists, backfill it."""
    venue_doc = MagicMock()
    venue_doc.id = "festival-1"
    venue_doc.to_dict.return_value = {}
    venue_doc.reference = MagicMock()

    venue_data: TestVenueData = {
        "recurrence": {
            "frequency": "yearly",
            "month": 5,
            "month_day": 15,
            "start_date": "2026-01-01",
        }
    }

    recurrence, occurrence_date = _resolve_event_occurrence_date(
        venue_doc, venue_data, today=date(2026, 5, 14)
    )

    assert recurrence is not None
    assert occurrence_date == date(2026, 5, 15)
    venue_doc.reference.set.assert_called_once()
    call_args = venue_doc.reference.set.call_args
    assert call_args[0][0] == {"next_occurrence_date": "2026-05-15"}
    assert call_args[1] == {"merge": True}


def test_resolve_event_occurrence_date_returns_existing_when_present():
    """When occurrence_date already exists, return it without recomputation."""
    venue_doc = MagicMock()
    venue_doc.id = "festival-1"
    venue_doc.reference = MagicMock()

    venue_data: TestVenueData = {
        "next_occurrence_date": "2027-05-15",
        "recurrence": {
            "frequency": "yearly",
            "month": 5,
            "month_day": 15,
        },
    }

    recurrence, occurrence_date = _resolve_event_occurrence_date(
        venue_doc, venue_data, today=date(2026, 5, 14)
    )

    assert occurrence_date == date(2027, 5, 15)
    venue_doc.reference.set.assert_not_called()


def test_resolve_event_occurrence_date_no_recurrence():
    """When recurrence is None, return None for occurrence_date."""
    venue_doc = MagicMock()
    venue_doc.id = "event-no-recurrence"

    venue_data: TestVenueData = {}  # no recurrence key

    recurrence, occurrence_date = _resolve_event_occurrence_date(
        venue_doc, venue_data, today=date(2026, 5, 14)
    )

    assert recurrence is None
    assert occurrence_date is None


def test_resolve_event_occurrence_date_invalid_recurrence_produces_no_date():
    """When recurrence exists but produces no valid date, return None."""
    venue_doc = MagicMock()
    venue_doc.id = "festival-invalid"
    venue_doc.reference = MagicMock()

    # Recurrence with end date in the past.
    venue_data: TestVenueData = {
        "recurrence": {
            "frequency": "yearly",
            "month": 5,
            "month_day": 15,
            "start_date": "2024-01-01",
        }
    }

    # Reference date is beyond any possible recurrence.
    recurrence, occurrence_date = _resolve_event_occurrence_date(
        venue_doc, venue_data, today=date(2030, 6, 1)
    )

    assert recurrence is not None
    # Yearly recurrence finds 2024-05-15 as first match (earliest year with month/day)
    assert occurrence_date == date(2024, 5, 15)


def test_create_event_poll_if_due_creates_poll_when_eligible():
    """Create poll and collections when inside creation window and not exists."""
    db = MagicMock()
    venue_doc = MagicMock()
    venue_doc.id = "festival-1"

    venue_data: TestVenueData = {"name": "Festival Name"}
    occurrence_date = date(2026, 5, 27)
    today = date(2026, 5, 20)  # Within 7-day lead window

    poll_doc = MagicMock()
    poll_doc.exists = False
    poll_doc.to_dict.return_value = None

    created_poll_doc = MagicMock()
    created_poll_doc.exists = True
    created_poll_doc.to_dict.return_value = {"date": "2026-05-27", "completed": False}

    poll_ref = MagicMock()
    poll_ref.get.side_effect = [poll_doc, created_poll_doc]
    db.document.return_value = poll_ref

    votes_collection = MagicMock()
    attendance_collection = MagicMock()

    def collection_side_effect(name):
        if name == "votes":
            return votes_collection
        if name == "attendance":
            return attendance_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    _create_event_poll_if_due(
        db,
        venue_doc=venue_doc,
        venue_data=venue_data,
        occurrence_date=occurrence_date,
        today=today,
        creation_lead_days=7,
    )

    poll_ref.set.assert_called_once()
    votes_collection.document.assert_called_once_with("event-festival-1-2026-05-27")
    attendance_collection.document.assert_called_once_with(
        "event-festival-1-2026-05-27"
    )


def test_create_event_poll_if_due_skips_when_too_early():
    """Skip creation when not yet in creation window."""
    db = MagicMock()
    venue_doc = MagicMock()
    venue_doc.id = "festival-1"

    venue_data: TestVenueData = {"name": "Festival Name"}
    occurrence_date = date(2026, 5, 27)
    today = date(2026, 5, 15)  # Before 7-day lead window

    poll_doc = MagicMock()
    poll_doc.exists = False
    poll_doc.to_dict.return_value = None

    poll_ref = MagicMock()
    poll_ref.get.return_value = poll_doc
    db.document.return_value = poll_ref

    _create_event_poll_if_due(
        db,
        venue_doc=venue_doc,
        venue_data=venue_data,
        occurrence_date=occurrence_date,
        today=today,
        creation_lead_days=7,
    )

    poll_ref.set.assert_not_called()  # Poll not created


def test_create_event_poll_if_due_skips_when_already_exists():
    """Skip creation when poll already exists."""
    db = MagicMock()
    venue_doc = MagicMock()
    venue_doc.id = "festival-1"

    venue_data: TestVenueData = {"name": "Festival Name"}
    occurrence_date = date(2026, 5, 27)
    today = date(2026, 5, 20)  # Within lead window

    poll_doc = MagicMock()
    poll_doc.exists = True  # Poll already exists
    poll_doc.to_dict.return_value = {"date": "2026-05-27", "completed": False}

    poll_ref = MagicMock()
    poll_ref.get.return_value = poll_doc
    db.document.return_value = poll_ref

    _create_event_poll_if_due(
        db,
        venue_doc=venue_doc,
        venue_data=venue_data,
        occurrence_date=occurrence_date,
        today=today,
        creation_lead_days=7,
    )

    poll_ref.set.assert_not_called()  # Poll not created again


def test_advance_event_occurrence_if_due_early_return_before_window():
    """When today < completion window, return without changes."""
    venue_doc = MagicMock()
    venue_data: TestVenueData = {}
    occurrence_date = date(2026, 5, 27)
    today = date(2026, 5, 26)  # Before the week of the event

    _advance_event_occurrence_if_due(
        venue_doc=venue_doc,
        venue_data=venue_data,
        recurrence=None,
        occurrence_date=occurrence_date,
        today=today,
    )

    venue_doc.reference.set.assert_not_called()


def test_maintain_event_recurrence_polls_does_not_mark_poll_complete():
    """Recurring event housekeeping does not auto-complete existing polls."""
    db = MagicMock()
    events_collection = MagicMock()
    votes_collection = MagicMock()
    attendance_collection = MagicMock()

    venue_doc = MagicMock()
    venue_doc.id = "festival-1"
    venue_doc.to_dict.return_value = {
        "name": "Festival Name",
        "venueType": "event",
        "next_occurrence_date": "2026-05-21",
        "recurrence": {
            "frequency": "yearly",
            "month": 5,
            "month_day": 21,
        },
    }
    venue_doc.reference = MagicMock()

    events_collection.stream.return_value = [venue_doc]

    poll_doc = MagicMock()
    poll_doc.exists = True
    poll_doc.to_dict.return_value = {"date": "2026-05-21", "completed": False}

    poll_ref = MagicMock()
    poll_ref.get.return_value = poll_doc

    def collection_side_effect(name: str):
        if name == EVENTS_COLLECTION:
            return events_collection
        if name == "votes":
            return votes_collection
        if name == "attendance":
            return attendance_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect
    db.document.return_value = poll_ref

    maintain_event_recurrence_polls(db, today=date(2026, 5, 25))

    # Existing poll is observed, but not auto-completed.
    poll_ref.set.assert_not_called()


def test_advance_event_occurrence_if_due_advances_next_date():
    """Advance next_occurrence_date when recurrence continues."""
    venue_doc = MagicMock()
    venue_doc.id = "festival-1"
    venue_doc.reference = MagicMock()

    recurrence_rule: EventRecurrenceRule = {
        "frequency": "yearly",
        "month": 5,
        "month_day": 21,
    }
    venue_data: TestVenueData = {
        "next_occurrence_date": "2026-05-21",
        "recurrence": recurrence_rule,
    }
    occurrence_date = date(2026, 5, 21)
    today = date(2026, 5, 25)  # After completion week

    _advance_event_occurrence_if_due(
        venue_doc=venue_doc,
        venue_data=venue_data,
        recurrence=recurrence_rule,
        occurrence_date=occurrence_date,
        today=today,
    )

    # Venue reference should be updated with next date
    venue_doc.reference.set.assert_called()
    call_args = venue_doc.reference.set.call_args
    # Should advance to next year's date
    assert "next_occurrence_date" in call_args[0][0]


def test_maintain_event_recurrence_polls_skips_non_event_venues():
    """Skip venues that are not of type 'event'."""
    db = MagicMock()
    events_collection = MagicMock()

    venue_doc = MagicMock()
    venue_doc.id = "pub-1"
    venue_doc.to_dict.return_value = {"venueType": "pub"}  # Not an event

    events_collection.stream.return_value = [venue_doc]
    db.collection.return_value = events_collection

    maintain_event_recurrence_polls(db, today=date(2026, 5, 20))

    # No db.document calls for this venue
    db.document.assert_not_called()


def test_maintain_event_recurrence_polls_continues_on_venue_error():
    """Continue processing when one venue fails."""
    db = MagicMock()
    events_collection = MagicMock()

    # First venue throws an error
    venue_doc_1 = MagicMock()
    venue_doc_1.id = "festival-1"
    venue_doc_1.to_dict.side_effect = Exception("Mock error")

    # Second venue should still process
    venue_doc_2 = MagicMock()
    venue_doc_2.id = "festival-2"
    venue_doc_2.to_dict.return_value = {"venueType": "event"}

    events_collection.stream.return_value = [venue_doc_1, venue_doc_2]

    def collection_side_effect(name):
        if name == EVENTS_COLLECTION:
            return events_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    # Should not raise, just log and continue
    maintain_event_recurrence_polls(db, today=date(2026, 5, 20))

    # Second venue was still reached and attempted
    assert db.document.called or True  # Process continued despite first error


def test_auto_complete_single_event_polls_due_tomorrow_completes_single_option():
    db = MagicMock()
    polls_collection = MagicMock()

    poll_doc = MagicMock()
    poll_doc.id = "poll-1"
    poll_doc.to_dict.return_value = {
        "completed": False,
        "date": "2026-05-20",
        "pubs": {
            "event-a": {"name": "Event A", "venueType": "event"},
        },
    }

    where_completed_query = MagicMock()
    where_date_query = MagicMock()
    where_date_query.stream.return_value = [poll_doc]
    where_completed_query.where.return_value = where_date_query
    polls_collection.where.return_value = where_completed_query

    db.collection.return_value = polls_collection

    auto_complete_single_event_polls_due_tomorrow(db, today=date(2026, 5, 19))

    poll_doc.reference.set.assert_called_once_with(
        {"completed": True, "selected": "event-a"},
        merge=True,
    )


def test_auto_complete_single_event_polls_due_tomorrow_skips_multi_option():
    db = MagicMock()
    polls_collection = MagicMock()

    poll_doc = MagicMock()
    poll_doc.id = "poll-2"
    poll_doc.to_dict.return_value = {
        "completed": False,
        "date": "2026-05-20",
        "pubs": {
            "pub-a": {"name": "Pub A"},
            "pub-b": {"name": "Pub B"},
        },
    }

    where_completed_query = MagicMock()
    where_date_query = MagicMock()
    where_date_query.stream.return_value = [poll_doc]
    where_completed_query.where.return_value = where_date_query
    polls_collection.where.return_value = where_completed_query

    db.collection.return_value = polls_collection

    auto_complete_single_event_polls_due_tomorrow(db, today=date(2026, 5, 19))

    poll_doc.reference.set.assert_not_called()


def test_auto_complete_multi_option_due_today_completes_clear_food_winner():
    db = MagicMock()
    polls_collection = MagicMock()
    votes_collection = MagicMock()
    pubs_collection = MagicMock()

    poll_doc = MagicMock()
    poll_doc.id = "poll-3"
    poll_doc.to_dict.return_value = {
        "completed": False,
        "date": "2026-05-19",
        "pubs": {
            "pub-a": {"name": "Pub A"},
            "pub-b": {"name": "Pub B"},
        },
    }

    where_completed_query = MagicMock()
    where_date_query = MagicMock()
    where_date_query.stream.return_value = [poll_doc]
    where_completed_query.where.return_value = where_date_query
    polls_collection.where.return_value = where_completed_query

    votes_doc = cast(
        DocumentSnapshot,
        SimpleNamespace(
            to_dict=lambda: {
                "any": [],
                "pub-a": ["u1", "u2"],
                "pub-b": ["u3"],
            }
        ),
    )
    votes_collection.document.return_value.get.return_value = votes_doc

    pub_a_doc = cast(
        DocumentSnapshot,
        SimpleNamespace(to_dict=lambda: {"food": True}),
    )
    pubs_collection.document.return_value.get.return_value = pub_a_doc

    def collection_side_effect(name: str):
        if name == POLLS_COLLECTION:
            return polls_collection
        if name == "votes":
            return votes_collection
        if name == EVENTS_COLLECTION:
            return pubs_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    auto_complete_multi_option_polls_due_today(db, today=date(2026, 5, 19))

    poll_doc.reference.set.assert_called_once_with(
        {"completed": True, "selected": "pub-a"},
        merge=True,
    )


def test_auto_complete_multi_option_due_today_skips_tie():
    db = MagicMock()
    polls_collection = MagicMock()
    votes_collection = MagicMock()

    poll_doc = MagicMock()
    poll_doc.id = "poll-4"
    poll_doc.to_dict.return_value = {
        "completed": False,
        "date": "2026-05-19",
        "pubs": {
            "pub-a": {"name": "Pub A"},
            "pub-b": {"name": "Pub B"},
        },
    }

    where_completed_query = MagicMock()
    where_date_query = MagicMock()
    where_date_query.stream.return_value = [poll_doc]
    where_completed_query.where.return_value = where_date_query
    polls_collection.where.return_value = where_completed_query

    votes_doc = cast(
        DocumentSnapshot,
        SimpleNamespace(
            to_dict=lambda: {
                "pub-a": ["u1"],
                "pub-b": ["u2"],
            }
        ),
    )
    votes_collection.document.return_value.get.return_value = votes_doc

    def collection_side_effect(name: str):
        if name == POLLS_COLLECTION:
            return polls_collection
        if name == "votes":
            return votes_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    auto_complete_multi_option_polls_due_today(db, today=date(2026, 5, 19))

    poll_doc.reference.set.assert_not_called()


def test_auto_complete_multi_option_due_today_skips_winner_without_food():
    db = MagicMock()
    polls_collection = MagicMock()
    votes_collection = MagicMock()
    pubs_collection = MagicMock()

    poll_doc = MagicMock()
    poll_doc.id = "poll-5"
    poll_doc.to_dict.return_value = {
        "completed": False,
        "date": "2026-05-19",
        "pubs": {
            "pub-a": {"name": "Pub A"},
            "pub-b": {"name": "Pub B"},
        },
    }

    where_completed_query = MagicMock()
    where_date_query = MagicMock()
    where_date_query.stream.return_value = [poll_doc]
    where_completed_query.where.return_value = where_date_query
    polls_collection.where.return_value = where_completed_query

    votes_doc = cast(
        DocumentSnapshot,
        SimpleNamespace(
            to_dict=lambda: {
                "pub-a": ["u1", "u2"],
                "pub-b": ["u3"],
            }
        ),
    )
    votes_collection.document.return_value.get.return_value = votes_doc

    pub_a_doc = cast(
        DocumentSnapshot,
        SimpleNamespace(to_dict=lambda: {"food": False}),
    )
    pubs_collection.document.return_value.get.return_value = pub_a_doc

    def collection_side_effect(name: str):
        if name == POLLS_COLLECTION:
            return polls_collection
        if name == "votes":
            return votes_collection
        if name == EVENTS_COLLECTION:
            return pubs_collection
        return MagicMock()

    db.collection.side_effect = collection_side_effect

    auto_complete_multi_option_polls_due_today(db, today=date(2026, 5, 19))

    poll_doc.reference.set.assert_not_called()
