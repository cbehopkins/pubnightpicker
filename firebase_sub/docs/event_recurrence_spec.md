# Event Recurrence Specification

## 1. Purpose

This specification defines how recurring event venues are materialised into dated poll instances and how the next due date is computed and maintained.

Primary goals:
- define the recurrence field contract in Firestore
- define how next_occurrence_date is calculated for all recurrence variants
- define what database writes are performed during each maintenance run

## 2. Scope

In scope:
- recurrence evaluation logic in event_recurrence module
- orchestration in maintain_event_recurrence_polls
- poll materialisation for recurring events
- next_occurrence_date roll-forward and clear behaviour

Out of scope:
- poll auto-completion winner selection
- notification send behaviour unrelated to recurring event materialisation

## 3. Data model and database fields

## 3.1 Venue documents (pubs collection)

Path:
- pubs/{venueId}

Required for this feature:
- venueType: must equal event to participate
- recurrence: recurrence rule object

Managed by this feature:
- next_occurrence_date: ISO date string YYYY-MM-DD

Observed/typed recurrence fields:
- frequency: once | weekly | monthly | yearly
- start_date: ISO date (anchor)
- date: ISO date (used by once)
- interval: integer >= 1 (default 1)
- weekdays: list of weekday ints (0..6, Monday=0)
- weekday: single weekday int (0..6)
- nth: integer occurrence index in month (positive or -1 for last)
- month: 1..12
- month_day: 1..31 (validated against month length)

Weekday encoding:
- Monday=0, Tuesday=1, Wednesday=2, Thursday=3, Friday=4, Saturday=5, Sunday=6

References:
- [firebase_sub/my_types.py](firebase_sub/my_types.py#L21)
- [firebase_sub/my_types.py](firebase_sub/my_types.py#L45)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L429)

## 3.2 Materialised poll documents

Path:
- polls/event-{venueId}-{occurrenceDate}

Created when due:
- date: occurrenceDate ISO
- completed: false
- pubs: map with one entry keyed by venueId
- eventVenueId: venueId
- eventOccurrenceDate: occurrenceDate ISO

Companion docs also created:
- votes/{pollId}: {"any": []}
- attendance/{pollId}: {}

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L455)

## 3.3 Audit records

Path:
- poll_action_audit/{pollId}_create_{timestampMicros}

Written on poll creation with:
- actionType=create
- actorUid=backend:auto
- pollId
- pollDate
- at timestamp

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L475)

## 4. Runtime orchestration

Entry point:
- maintain_event_recurrence_polls

Per venue workflow:
1. Iterate all venue docs in pubs.
2. Skip unless venueType == event.
3. Resolve recurrence and occurrence date.
4. If no occurrence date, skip with log.
5. Create dated poll if creation window is open and poll does not already exist.
6. Advance or clear next_occurrence_date when occurrence week has started/completed.
7. On venue-specific exceptions, log and continue to next venue.

References:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L528)
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py#L785)
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py#L803)

## 5. Recurrence calculation algorithm

Core function:
- next_occurrence(recurrence, reference_date) -> date | None

## 5.1 Shared rules

- frequency defaults to once if absent.
- anchor_date = parse(start_date) if valid else reference_date.
- interval = max(int(interval or 1), 1).
- invalid date strings parse to None.
- unknown frequency raises ValueError.

Reference:
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L65)

## 5.2 once

Input:
- recurrence.date

Rule:
- return parsed date or None if invalid/missing.

## 5.3 weekly

Inputs:
- weekdays list preferred
- fallback to single weekday if weekdays missing
- interval week cadence from anchor_date

Rule:
1. Start search_date at max(reference_date, anchor_date).
2. Iterate forward day-by-day (bounded to ~20 years).
3. For each day, compute weeks_since_anchor = floor((search_date-anchor_date).days / 7).
4. Keep only days where weeks_since_anchor % interval == 0.
5. Find first configured weekday on/after search_date.
6. Re-check candidate still on valid interval week boundary.
7. Return first candidate found.
8. If none found in bound, return None.

Notes:
- weekday domain is 0..6.
- helper _first_weekday_on_or_after scans at most 14 days.

Reference:
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L76)

## 5.4 monthly

Inputs:
- either month_day OR weekday (+ nth)
- interval month cadence from anchor month

Rule:
1. search month starts at first day of reference month.
2. anchor month is first day of anchor_date month.
3. Iterate months forward (bounded to 240 months).
4. Skip months before anchor month.
5. Skip months where months_since_anchor % interval != 0.
6. Candidate resolution:
- if month_day provided: use that day if valid for month
- else if weekday provided: use nth weekday-of-month helper
- else return None
7. Return first candidate >= reference_date.
8. If none in bound, return None.

nth weekday semantics:
- nth > 0: nth occurrence from month start
- nth = -1: last occurrence in month
- nth = 0 invalid

Reference:
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L102)

## 5.5 yearly

Inputs:
- month required
- either month_day OR weekday (+ nth)
- anchor year lower bound

Rule:
1. Validate month in 1..12 else None.
2. Iterate years from reference_date.year to +39 years.
3. Skip years < anchor year.
4. Candidate resolution:
- month_day branch: valid day in month
- weekday branch: nth weekday-of-month helper
- neither: None
5. Return first candidate >= reference_date.
6. If none in bound, return None.

Reference:
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L138)

## 6. next_occurrence_date materialisation and roll-forward

Canonicaliser:
- materialized_next_occurrence_date / materialized_next_occurrence_iso_state

Behaviour:
1. Parse current stored next_occurrence_date.
2. If recurrence missing => target None.
3. If current stored date is a valid recurrence match:
- if current >= today: keep as-is
- if current < today and today >= start of occurrence week: advance to next occurrence after current
4. Otherwise recompute from today.

Implication:
- valid future values stay stable
- stale/current-week-completed values roll forward deterministically
- invalid stored values are corrected

References:
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L173)
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L200)

## 7. Occurrence resolution and backfill

During maintain_event_recurrence_polls, _resolve_event_occurrence_date does:
- read recurrence and next_occurrence_date
- if next_occurrence_date missing and recurrence exists:
- reference_date = parsed recurrence.start_date else today
- compute occurrence with next_occurrence(recurrence, reference_date)
- if result exists, write next_occurrence_date immediately

This write is merge on venue doc.

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L429)

## 8. Poll creation window

Helper:
- creation_window_start(occurrence_date, lead_days)

Default lead_days:
- 7

Poll is created only when:
- today >= occurrence_date - lead_days
- poll document for deterministic event poll ID does not exist

Poll ID format:
- event-{venueId}-{YYYY-MM-DD}

References:
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L20)
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L214)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L455)

## 9. Occurrence advancement and clear conditions

Advancement gate:
- do nothing while today < week_start(occurrence_date)

Once in or after occurrence week:
1. Compute current_iso and next_iso via materialized_next_occurrence_iso_state.
2. If next_iso == current_iso: no write.
3. If next_iso exists and changed: set venue next_occurrence_date = next_iso.
4. If next_iso is None: clear next_occurrence_date field.

Clear scenarios:
- recurrence removed
- recurrence no longer yields future date

References:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L485)
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py#L218)

## 10. Triggered writes/events summary

Writes that can happen in one run:
- pubs/{venueId}.next_occurrence_date set/advanced/cleared
- polls/{eventPollId} created (if due and absent)
- votes/{eventPollId} initialised
- attendance/{eventPollId} initialised
- poll_action_audit create entry for new poll

Not done by this flow:
- does not mark recurring event polls as completed
- does not run winner selection

Reference:
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py#L702)

## 11. Option-by-option examples

Yearly fixed date:
- recurrence: {frequency: yearly, month: 8, month_day: 23}
- next from 2026-05-14 => 2026-08-23

Yearly nth weekday:
- recurrence: {frequency: yearly, month: 5, weekday: 2, nth: 3}
- next from 2026-05-21 => 2027-05-19

Monthly last Wednesday:
- recurrence: {frequency: monthly, weekday: 2, nth: -1}
- next from 2026-05-14 => 2026-05-27

Weekly Wednesday:
- recurrence: {frequency: weekly, weekdays: [2], start_date: 2026-05-04}
- next from 2026-05-14 => 2026-05-20

References:
- [tests/test_event_recurrence.py](tests/test_event_recurrence.py#L20)
- [tests/test_event_recurrence.py](tests/test_event_recurrence.py#L30)
- [tests/test_event_recurrence.py](tests/test_event_recurrence.py#L41)
- [tests/test_event_recurrence.py](tests/test_event_recurrence.py#L10)

## 12. Frontend/backend weekday compatibility contract

Contract:
- frontend and backend must both use Monday-based weekday numbering
- Wednesday must persist as 2

Regression evidence confirms this mapping yields correct computed date for yearly 3rd Wednesday.

Reference:
- [tests/integration/test_event_recurrence_weekday_mismatch_e2e.py](tests/integration/test_event_recurrence_weekday_mismatch_e2e.py#L1)

## 13. Conformance requirements for reimplementation

A conforming implementation must:
- support once, weekly, monthly, yearly frequencies
- support interval cadence for weekly/monthly/yearly
- support monthly/yearly day-of-month and nth-weekday variants
- maintain deterministic poll IDs for recurring event polls
- materialise and roll forward next_occurrence_date using canonicaliser semantics
- create poll/votes/attendance documents only in creation window and only if absent
- write create audit entries
- skip non-event venues and continue after per-venue failures

## 14. Evidence sources

- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py)
- [tests/test_event_recurrence.py](tests/test_event_recurrence.py)
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py)
- [tests/integration/test_event_recurrence_weekday_mismatch_e2e.py](tests/integration/test_event_recurrence_weekday_mismatch_e2e.py)
