# CDD: Event Recurrence and Poll Materialisation

> **Terminology note:** this document predates ADR-0010. Where it says "Fact"
> it means what the codebase now calls a **Truth** (ADR-0009); `StaleEvent` and
> `CreateEventPoll` are declared in `internal/lastorders/truths` and dispatched
> via their own native `cellar.Fanout[T]`.

## 1. Purpose

Define the backend architecture and behaviour for recurring event venues.

The service maintains each recurring event's next occurrence date and
materialises a poll when that occurrence enters its poll-creation window. The
design is listener-driven rather than time-based housekeeping and is safe to
replay and retry through durable Truth, Fact, and Cell processing.

## 2. Scope

### In scope

- Detection of stale or missing event occurrence dates.
- Calculation and persistence of the next recurrence occurrence.
- Detection of occurrences entering the poll-creation window.
- Creation of event polls, votes, attendance, and poll audit records.
- Idempotency, retry, replay, and concurrent execution behaviour.
- Recurrence calculation for supported recurrence forms.

### Out of scope

- Poll auto-completion, winner selection, and notification delivery.
- Application-data deletion, event creation UX, and authentication.
- General-purpose housekeeping scheduling.

## 3. Architecture

Firestore is the source of truth. The database listener observes event-venue
state and creates the application-level `EventVenueObserved` Truth. The
recurrence plugin evaluates that immutable evidence and, when appropriate,
creates either a `StaleEvent` or `CreateEventPoll` Fact. Their Cells perform the
state-changing work.

```text
Firestore pubs/{eventId} change or periodic re-evaluation
        |
        v
EventVenueObserved Truth
        |
        v
recurrence plugin evaluation
        |
        +-- stale or missing occurrence --> StaleEvent Cell
        |
        +-- occurrence due -------------> Event Poll Creation Cell
```

The database listener constructs Truths but does not decide or execute the
subsequent recurrence work. The plugin defines application connectivity; the
recurrence component defines recurrence predicates and calculations.

## 4. Event Venue Data Model

Recurring events are documents in `pubs/{eventId}`. A venue participates only
when `venueType == "event"`.

The document contains a recurrence definition and a persisted occurrence date:

```text
next_occurrence_date: YYYY-MM-DD
```

The persisted date is the next calculated occurrence. It is derived convenience
state, not an independent recurrence definition; the recurrence rule remains
authoritative. The field name is a frontend/backend compatibility contract.

## 5. Recurrence Data Model

The recurrence definition supports `once`, `weekly`, `monthly`, and `yearly`
frequencies. Supported fields are `date`, `interval`, `weekdays`, `weekday`,
`nth`, `month`, and `month_day`. Weekdays use Monday = 0 through Sunday = 6.
This is a persistent frontend/backend contract.

The schema must not contain a separate `start_date` field. For a `once` rule,
the rule's `date` is its configured occurrence; it is distinct from the mutable
`next_occurrence_date` field.

- `once`: the configured date is the occurrence; a missing, invalid, or passed
  date has no future occurrence.
- `weekly`: one or more weekdays and an optional interval in weeks.
- `monthly`: either `month_day`, or `weekday` with `nth`, and an optional
  interval in months.
- `yearly`: a month plus either `month_day`, or `weekday` with `nth`, and an
  optional interval in years.

`nth = -1` means the final matching weekday. Invalid dates must not silently
produce another date.

## 6. Timezone and Calculation

All recurrence evaluation uses `Europe/London`. It is the authoritative
timezone for today, week boundaries, occurrence dates, and poll creation.

The calculator is deterministic: the same recurrence definition and reference
state produce the same next occurrence. When a stored date exists, the reference
must be strictly later than it; otherwise the calculator returns the stored date
again and the venue never advances. A dormant venue uses:

```text
max(storedDate + 1 day, today)
```

This may re-anchor an interval greater than one on the reference date. That is
accepted.

## 7. EventVenueObserved Truth

The event-venue database listener observes venues through the Firestore query
`venueType == "event"`. A periodic durable timer also enumerates current event
venues so date-boundary changes are observed without a document change.

Both mechanisms create the same `EventVenueObserved` Truth. Its application-
owned evidence is the event ID, recurrence definition, persisted next occurrence
date, and `observedOn` London calendar date. The evidence is immutable and
serialisable. A delayed or retried Cell evaluates the state captured when the
Truth was created, not whatever state the venue subsequently reached.

### 7.1 Truth Identity and Idempotency

An `EventVenueObserved` identity contains:

```text
eventId + next_occurrence_date + recurrenceHash + observedOn
```

The recurrence hash is computed over a canonical representation, so equal rules
have equal identities regardless of map-field ordering. Including the recurrence
definition allows an edit to create a new Truth when the persisted date is
unchanged. Including `observedOn` makes a new calendar-day observation distinct,
while duplicate delivery of unchanged evidence on the same day is suppressed.

The identity is established through the Firebase idempotency sequence before the
Truth is fanned out. The listener must not maintain separate in-memory duplicate
suppression.

## 8. Recurrence Evaluation

The recurrence plugin consumes `EventVenueObserved` and evaluates its captured
evidence using `observedOn` in the London timezone.

If the stored date is absent, past, invalid, or no longer equals the occurrence
calculated from the current recurrence definition, the plugin creates a
`StaleEvent` Fact. Otherwise, if the stored date falls within the inclusive
creation window:

```text
today <= next_occurrence_date <= today + 7 days
```

the plugin creates a `CreateEventPoll` Fact. An occurrence in the past is stale,
not due, so the outcomes are mutually exclusive.

The plugin uses the following idempotency identities:

```text
stale_events: {eventId}_{next_occurrence_date}_{recurrenceHash}
event_due:    {eventId}_{next_occurrence_date}
```

The namespaces are independent. These downstream Facts are created through the
idempotency component; direct work execution is not permitted.

## 9. Stale Event Cell

The `StaleEvent` Cell receives the event identity and observed date. It re-reads
current Firestore state before mutation, verifies that the venue remains an event
venue, reads the current recurrence and stored occurrence, calculates the next
required occurrence, and updates `next_occurrence_date`.

The Cell deliberately revalidates current state for its convergent write. This
does not reinterpret the earlier Truth evidence: it ensures a delayed or replayed
mutation does not advance a newer state incorrectly. When no usable recurrence
or future occurrence remains, the stored occurrence field is cleared.

## 10. Event Poll Creation Cell

The `CreateEventPoll` Fact creates an Event Poll Creation Cell with `eventId`
and `occurrenceDate`. It reads current Firestore state and materialises the poll
only if the occurrence has not been superseded. Poll documents use normal
Firestore-generated IDs.

Before creation, it searches for an existing poll matching:

```text
eventVenueId == eventId
date == occurrenceDate
```

If a matching poll exists, the desired state is already achieved and the Cell
completes successfully without creating a poll, votes, attendance, or audit
record. This business-level idempotency check is required, not an optimisation.

## 11. Poll Materialisation

If no matching poll exists, the Cell creates a poll with at least:

```text
date: occurrenceDate
completed: false
pubs:
    eventId: { name: venueName }
eventVenueId: eventId
eventOccurrenceDate: occurrenceDate
```

It also creates `votes/{pollId}` and `attendance/{pollId}` with their normal
initial contents. The poll, votes document, and attendance document are created
in the same Firestore transaction as the existence check. This protects against
concurrent Cells both observing no poll and materialising duplicates.

A successful new poll produces an immutable audit record containing at least
`pollId`, `actionType = create`, `actorUid = backend:auto`, `pollDate`, and a
timestamp. Audit data is observability and historical evidence only; it is not
the source of truth for poll existence.

## 12. Safety Properties

The implementation must preserve these invariants:

1. Only `venueType == "event"` documents participate.
2. Truth evidence is application-owned, immutable, and sufficient to understand
   the original observation.
3. Duplicate observations with the same Truth identity produce no duplicate
   downstream work.
4. Recurrence edits produce a new observation identity even when the persisted
   occurrence date is unchanged.
5. Recurrence maintenance and poll materialisation are independently idempotent.
6. Poll creation uses a transactional business-state existence check and never
   depends on exactly-once execution.
7. Successful state transitions converge so replay does not repeat completed
   work.
