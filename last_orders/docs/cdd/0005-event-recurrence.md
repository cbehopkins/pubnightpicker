# CDD: Event Recurrence Maintenance

> **DEPRECATED.** Superseded by the listener-driven design in `000x.md`. This
> document describes the scheduled housekeeping sweep, which no longer exists.
> Retained for reference only.

## 1. Purpose

The Event Recurrence service maintains recurring event venues and materialises their upcoming occurrences as polls.

The service is responsible for:

* determining the next occurrence of a recurring event;
* maintaining the venue's `next_occurrence_date`;
* creating the corresponding poll when its creation window opens;
* creating the associated initial vote and attendance documents;
* recording the creation in the poll audit trail.

The service is designed around **safe replay and reconciliation** rather than exactly-once execution.

A repeated execution must converge on the same desired state without creating duplicate polls or corrupting recurrence state.

---

# 2. Scope

### In scope

* Discovering recurring event venues.
* Evaluating recurrence rules.
* Maintaining `next_occurrence_date`.
* Materialising recurring event polls.
* Initialising the associated vote and attendance documents.
* Recording poll creation audit information.
* Handling replay and concurrent execution safely.
* Maintaining deterministic recurrence behaviour.

### Out of scope

* Poll auto-completion.
* Winner selection.
* Poll notification delivery.
* Recurring event business logic unrelated to occurrence calculation and poll materialisation.
* Front-end recurrence configuration.

---

# 3. Architectural Model

The service is implemented using the Cellar architecture.

The conceptual flow is:

```text
Housekeeping trigger
        │
        ▼
Recurrence Sweep Cell
        │
        │ discover eligible venues
        ▼
Event Recurrence Cell
        │
        ├── evaluate recurrence
        ├── reconcile next_occurrence_date
        └── create/materialise poll if due
```

The exact Cell names are implementation details.

The important architectural boundary is:

> The housekeeping trigger discovers work; Cells perform the recurrence reconciliation and materialisation.

The handler must not depend upon exactly-once execution.

A Cell may be replayed after partial completion, and every externally visible operation must therefore either be idempotent or safely converge through reconciliation.

---

# 4. Source of Truth

Recurring event configuration is stored on the venue document:

```text
pubs/{venueId}
```

A venue participates in recurrence maintenance only when:

```text
venueType == "event"
```

The venue's recurrence configuration determines its valid occurrence dates.

The venue also contains the maintained field:

```text
next_occurrence_date
```

This is a materialised convenience state derived from the recurrence rule.

It is not an independent recurrence definition.

The recurrence rule remains authoritative.

---

# 5. Recurrence Data Model

A recurrence rule supports:

```text
frequency:
    once
    weekly
    monthly
    yearly
```

Common fields include:

```text
interval
```

Weekly rules may specify:

```text
weekdays
```

or a single:

```text
weekday
```

Monthly and yearly rules may specify either:

```text
month_day
```

or:

```text
weekday
nth
```

Yearly rules additionally specify:

```text
month
```

Weekday numbering is:

```text
Monday    = 0
Tuesday   = 1
Wednesday = 2
Thursday  = 3
Friday    = 4
Saturday  = 5
Sunday    = 6
```

This is a persistent frontend/backend contract and must not be changed independently by either side.

---

# 6. Timezone

All recurrence evaluation is performed using:

```text
Europe/London
```

This is the authoritative timezone for event recurrence.

The service must therefore evaluate "today", week boundaries, occurrence dates and creation windows using London local calendar dates.

The implementation should use the IANA timezone identifier rather than attempting to encode daylight-saving rules manually.

Cambridge does not require a separate timezone: Cambridge, like London, uses:

```text
Europe/London
```

Therefore the service should not introduce a Cambridge-specific timezone configuration.

---

# 7. Housekeeping Trigger

A scheduled housekeeping operation initiates recurrence maintenance.

The trigger establishes the execution context and creates Cellar work.

It should not itself contain the recurrence algorithm or materialise polls.

Conceptually:

```text
scheduled housekeeping
        │
        ▼
Recurrence Sweep
        │
        ▼
Event Recurrence Cell(s)
```

The scheduler may run repeatedly or overlap with another execution.

Correctness must not depend on a particular schedule execution occurring exactly once.

---

# 8. Recurrence Sweep

The sweep identifies venues which may require recurrence maintenance.

For each venue:

1. Read the venue document.
2. Ignore venues whose `venueType` is not `event`.
3. Ignore venues with no recurrence configuration.
4. Create/execute recurrence work for the venue.

Per-venue failures must not prevent other venues from being processed.

The sweep is therefore a discovery/orchestration operation rather than the owner of recurrence correctness.

---

# 9. Event Recurrence Cell

The Event Recurrence Cell receives the venue identity:

```text
venueId
```

It obtains the current venue document and reconciles it against the recurrence definition.

The Cell must use the **current Firestore state**, rather than relying on a stale copy of the venue discovered by the sweep.

This is important for safe replay and concurrent execution.

The Cell performs the following conceptual operation:

```text
read current venue
        │
        ▼
resolve recurrence
        │
        ├── no recurrence ──► no-op / clear stale materialisation
        │
        ▼
determine next occurrence
        │
        ▼
reconcile next_occurrence_date
        │
        ▼
determine whether poll materialisation is due
        │
        ▼
materialise poll if required
```

---

# 10. Recurrence Evaluation

The recurrence engine provides the conceptual operation:

```text
next_occurrence(recurrence, reference_date)
    → occurrence date | None
```

The result must be deterministic.

Given the same recurrence definition and reference date, the same occurrence date must be returned.

Invalid recurrence configuration must not result in an invented occurrence.

---

# 11. `once` Recurrence

For a `once` recurrence, the configured recurrence date is the occurrence date.

If the date is missing or invalid:

```text
no occurrence
```

is returned.

---

# 12. `weekly` Recurrence

Weekly recurrence supports one or more weekdays.

`weekdays` is preferred when present.

A single `weekday` may be used as the fallback representation.

The recurrence is evaluated relative to the reference (today's) date and its `interval`.

For example:

```text
interval   = 1
weekdays   = [2]
```

produces Wednesdays beginning from the applicable recurrence boundary.

The implementation must preserve the Monday-based weekday contract.

---

# 13. `monthly` Recurrence

Monthly recurrence supports:

### Fixed day

```text
month_day
```

or:

### Nth weekday

```text
weekday
nth
```

For example:

```text
weekday = 2
nth     = -1
```

means the last Wednesday of the month.

Positive `nth` values count from the beginning of the month.

`nth = -1` means the final occurrence.

Invalid dates must not be silently converted to a different date.

---

# 14. `yearly` Recurrence

Yearly recurrence specifies:

```text
month
```

and either:

```text
month_day
```

or:

```text
weekday
nth
```

For example:

```text
month   = 5
weekday = 2
nth     = 3
```

means the third Wednesday in May.

The recurrence must continue to be evaluated using the configured anchor and interval semantics.

---

# 15. `next_occurrence_date`

The venue may contain:

```text
next_occurrence_date
```

This field is a materialised representation of the next relevant recurrence occurrence.

The recurrence definition remains authoritative.

The service must reconcile the stored value rather than blindly trusting it.

### Stable future value

If the stored date is:

* valid;
* consistent with the recurrence rule; and
* still in the future;

it should remain unchanged.

This avoids unnecessary writes.

### Stale value

If the stored occurrence has passed its relevant occurrence week, the service advances it to the next recurrence occurrence.

### Invalid value

If the stored value is invalid or inconsistent with the recurrence rule, it is recomputed.

### No future occurrence

If the recurrence no longer produces a future occurrence, the field is cleared.

### Recurrence removed

If recurrence configuration is removed, a previously materialised `next_occurrence_date` must not remain as stale recurrence state.

It should be cleared.

---

# 16. Occurrence Week

An occurrence is considered to have reached its maintenance/roll-forward boundary when the current London date is within or after the occurrence week.

Before that point, a valid future `next_occurrence_date` must remain stable.

This prevents the housekeeping process from advancing an occurrence prematurely.

Conceptually:

```text
before occurrence week
        │
        └── keep current occurrence

occurrence week begins
        │
        └── reconcile / roll forward
```

---

# 17. Replay Semantics

The recurrence Cell is explicitly replayable.

Consider:

```text
next_occurrence_date = 2026-08-17
today                = 2026-08-17
```

The first execution may:

1. determine that the occurrence is now due;
2. materialise the poll;
3. advance `next_occurrence_date` to the following occurrence.

A replay then reads the **new** venue state.

For example:

```text
first execution:

2026-08-17
    │
    ├── create poll for 2026-08-17
    │
    └── advance venue to 2026-09-17


replay:

2026-09-17
    │
    └── current occurrence is no longer the
        occurrence being maintained
```

The replay therefore naturally becomes a no-op or proceeds against the newly due occurrence according to the normal recurrence rules.

This is an important idempotency property:

> Successful progress changes the state such that replay no longer attempts to repeat the completed operation.

---

# 18. Poll Materialisation

A recurring event poll has a deterministic identity:

```text
event-{venueId}-{occurrenceDate}
```

For example:

```text
event-pub123-2026-08-17
```

This deterministic ID is the primary idempotency mechanism for poll creation.

The poll is created when:

```text
today >= occurrenceDate - lead_days
```

The default lead time is:

```text
7 days
```

Therefore an occurrence becomes eligible for materialisation seven London calendar days before the event date.

---

# 19. Existing Polls

Poll document identifiers are assigned by Firestore, exactly as they are for a
normal user-initiated poll creation. The service must not construct a poll
identifier of its own.

Materialisation is therefore detected by state rather than by identifier. A poll
is considered already materialised for a venue occurrence when a poll exists
with:

```text
date == occurrenceDate
pubs contains venueId
```

If such a poll exists, the service must **not recreate it**.

An existing poll is considered evidence that materialisation has already occurred.

The service should not overwrite or reset the existing poll merely because recurrence maintenance is being replayed.

This is an explicit idempotency requirement.

The existence check and the creation must occur within the same Firestore
transaction so that concurrent replays cannot both materialise the occurrence.

Conceptually:

```text
poll absent
    │
    └── create poll

poll exists
    │
    └── leave it alone
```

---

# 20. Poll Creation

A newly materialised poll contains at least:

```text
date              = occurrenceDate
completed         = false
pubs              = { venueId: { name: venueName } }
eventOccurrenceDate = occurrenceDate
```

The `pubs` entry uses the venue identifier as its key and carries the venue's
`name` as read from the venue document, matching the representation produced by
normal poll creation.

The creation operation must not overwrite an existing poll.

---

# 21. Companion Documents

When a new poll is materialised, the service also creates:

```text
votes/{pollId}
```

with the initial voting state:

```text
{
    "any": []
}
```

and:

```text
attendance/{pollId}
```

with its initial empty state.

These documents belong to poll materialisation.

They must not be recreated or reset during replay of an already-materialised poll.

---

# 22. Poll Creation Audit

Successful poll creation produces an append-only audit record:

```text
poll_action_audit/{pollId}_create_{timestamp}
```

The audit contains at least:

```text
actionType = create
actorUid   = backend:auto
pollId
pollDate
at
```

The audit trail is historical evidence.

It must not be used as the primary mechanism for determining whether a poll exists.

The deterministic poll document ID is the authoritative idempotency mechanism.

---

# 23. Poll Creation and Atomicity

Poll materialisation consists of related Firestore writes:

```text
poll
votes
attendance
audit
```

Where the application's transaction mechanism permits, the creation of the core poll state and its required companion state should be committed as one application-level transaction/batch.

The design must avoid a replay resetting an already-created poll.

If audit information is deliberately treated as best-effort, failure to write the audit must not cause an already-successful poll materialisation to be repeated destructively.

The exact transaction boundary should therefore distinguish:

### Correctness state

* poll existence;
* initial poll state;
* vote initialisation;
* attendance initialisation.

### Observability state

* creation audit.

The latter must never determine whether the former has succeeded.

---

# 24. Recurrence State and Poll State

The following are separate pieces of durable state:

```text
Venue
    └── next_occurrence_date

Poll
    └── materialised occurrence
```

They must not be treated as one state machine.

A poll may already exist while recurrence state still needs reconciliation.

Conversely, recurrence state may advance even though poll creation has already occurred.

The implementation must therefore reconcile each piece of state independently.

---

# 25. Ordering of Operations

The implementation must preserve a safe ordering between poll materialisation and recurrence advancement.

The desired conceptual result is:

```text
occurrence
    │
    ├── ensure poll exists
    │
    └── advance recurrence state
```

The service must not advance the venue's recurrence state in a way that can permanently lose an occurrence whose poll has not been materialised.

The implementation should therefore ensure that the poll for the current occurrence has been successfully established before considering that occurrence fully consumed.

This is particularly important when failures occur between Firestore writes.

---

# 26. Failure and Replay

The service must tolerate process failure at any point.

For example:

```text
determine occurrence
        │
        ▼
create poll
        │
        X process dies
        │
        ▼
replay
        │
        ▼
poll already exists
        │
        ▼
do not recreate/reset poll
```

Likewise:

```text
create poll
        │
        ▼
advance next_occurrence_date
        │
        X process dies
        │
        ▼
replay
        │
        ▼
read current venue state
        │
        ▼
reconcile current recurrence state
```

The service must converge without requiring exactly-once execution.

---

# 27. Concurrent Execution

Two recurrence Cells may theoretically process the same venue concurrently.

Correctness must not depend upon this being impossible.

Both executions may initially observe the same occurrence.

The implementation must ensure that:

* the deterministic poll ID prevents duplicate poll creation;
* an existing poll is never reset;
* recurrence state is reconciled against current Firestore state;
* a later execution does not destroy or invalidate the result of an earlier execution.

Where concurrent writes to `next_occurrence_date` are possible, the implementation should use the application's normal transactional/concurrency mechanism where required to prevent stale recurrence state from overwriting newer state.

The desired outcome is convergence on the same next occurrence.

---

# 28. Idempotency Invariants

The service must preserve the following invariants.

### Invariant 1 — Deterministic poll identity

For a given venue and occurrence date there is exactly one poll identity:

```text
event-{venueId}-{occurrenceDate}
```

### Invariant 2 — Existing polls are never recreated

If the deterministic poll already exists, recurrence maintenance must not create another poll or reset its state.

### Invariant 3 — Recurrence is derived state

`next_occurrence_date` is derived from the recurrence definition and may be recalculated.

### Invariant 4 — Replay is safe

Repeating the same Cell against current Firestore state must not produce duplicate polls or destructive rewrites.

### Invariant 5 — Progress changes eligibility

Once an occurrence has been successfully processed and `next_occurrence_date` advances, a replay normally no longer sees the previous occurrence as the current maintenance target.

### Invariant 6 — Poll state is durable

Once a poll exists, subsequent recurrence maintenance must leave its user-visible state alone.

### Invariant 7 — No occurrence may be silently lost

Advancing recurrence state must not permanently discard an occurrence before its required poll materialisation has been established.

---

# 29. No Completion Behaviour

This service does not complete polls.

It does not:

* select a winning venue;
* mark a poll completed;
* send completion notifications;
* invoke the poll auto-completion rules.

Once a recurring poll has been materialised, its subsequent lifecycle belongs to the normal poll architecture.

This creates a clean boundary:

```text
Event Recurrence
    │
    └── creates poll
            │
            ▼
       Poll lifecycle
            │
            ├── voting
            ├── auto-completion
            └── notifications
```

---

# 30. Safety Properties

The implementation must guarantee:

1. **Recurring events are evaluated using London local calendar time.**
2. **Only event venues participate in recurrence maintenance.**
3. **Recurrence evaluation is deterministic.**
4. **A deterministic occurrence produces a deterministic poll ID.**
5. **An existing poll is never recreated or reset.**
6. **Replay does not create duplicate polls.**
7. **Stale recurrence state can be repaired.**
8. **Removing recurrence removes stale materialised recurrence state.**
9. **Advancing recurrence does not lose an unmaterialised occurrence.**
10. **Poll completion is outside this service's responsibility.**

---

# 31. Conceptual Flow

```text
                Scheduled Housekeeping
                         │
                         ▼
                Recurrence Sweep Cell
                         │
                         ▼
                Event Recurrence Cell
                         │
                         ▼
                  Read venue state
                         │
                         ▼
                  Is venue an event?
                    /          \
                  no            yes
                  │              │
                no-op            ▼
                         Read recurrence
                                │
                         ┌──────┴──────┐
                         │             │
                    no recurrence   recurrence
                         │             │
                    clear stale        ▼
                    state         Resolve occurrence
                                       │
                                       ▼
                              Reconcile next_occurrence
                                       │
                                       ▼
                              Is poll creation window open?
                                  /              \
                                no                yes
                                │                  │
                              done                 ▼
                                      Deterministic poll ID
                                                 │
                                          ┌──────┴──────┐
                                          │             │
                                        exists        absent
                                          │             │
                                        leave          create
                                        alone        poll + state
                                                        │
                                                        ▼
                                               Advance recurrence
                                                        │
                                                        ▼
                                                       done
```

---

# 32. Architectural Principle

The fundamental design principle is:

> **Event recurrence is a reconciliation service, not an exactly-once job.**

The system does not need to guarantee that a particular scheduled execution happens exactly once.

Instead, every execution reconciles durable Firestore state towards the same desired state:

```text
recurrence definition
        +
current London date
        +
existing materialised poll
        ↓
correct next occurrence
+
correct poll materialisation
```

Deterministic poll identity, current-state reads, and safe recurrence roll-forward provide the idempotency boundary.

This allows the housekeeping scheduler and Cellar runtime to retry work freely without requiring the recurrence service itself to implement exactly-once execution.
