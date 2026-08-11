# ADR: Scheduled and Timer-Based Observations

**Status:** Proposed
**Version:** V0 design with V1 extension point
**Date:** 2026-08-11

## Context

The backend contains work that is triggered by the passage of time rather than by an external database change or webhook.

Examples include:

* housekeeping tasks;
* cleanup of old records;
* automatic poll completion after a voting deadline;
* other future time-based business events.

These do not all have the same semantics when the backend is unavailable at the scheduled time.

A housekeeping operation such as:

> "Tidy old debug records every night"

does not need to execute once for every missed occurrence.

If the backend is unavailable for three days, it is sufficient to perform the housekeeping operation when it next becomes available.

A business event such as:

> "Poll ABC123 has passed its automatic completion deadline"

is different. If the backend is unavailable when the deadline passes, the event remains relevant after the backend restarts.

The system therefore needs to distinguish **maintenance schedules** from **time-based business obligations**.

## Decision

### V0

V0 will implement timer-driven work as simple scheduled observations.

When a timer becomes due, it creates the appropriate Cell.

The timer infrastructure itself will not persist missed timer occurrences.

Conceptually:

```text
Clock
  │
  ▼
Timer becomes due
  │
  ▼
Create Cell
  │
  ▼
Execute work
```

If the backend is unavailable when a V0 timer becomes due, that occurrence may be lost.

This is explicitly acceptable for V0 housekeeping tasks.

The V0 timer mechanism therefore does not attempt to provide durable timer delivery.

---

# V0 timer semantics

A timer represents an opportunity to perform a task when the scheduled time is reached.

For housekeeping tasks, the intended semantic is:

> **The work should happen at the next available opportunity; individual missed timer occurrences do not need to be replayed.**

For example, if a daily cleanup task is scheduled for 02:00 and the backend is unavailable for three days, the system does not need to execute three cleanup operations when it restarts.

Instead, it performs the cleanup at the next available opportunity.

This is deliberately simple.

---

# V1: Durable timer observations

V1 should extend the timer mechanism to support time-based business events that must survive the backend being unavailable when they become due.

Examples include automatic poll completion.

Consider a poll:

```text
Poll ABC123
deadline: 19:00
```

If the backend is unavailable from 18:00 until 21:00, the event:

> "Poll ABC123 has become eligible for automatic completion"

remains relevant.

When the backend restarts, it should discover and process that obligation.

The V1 model should therefore use the same general observation/idempotency architecture as database listeners.

```text
Timer
  │
  ▼
Timer Observation
  │
  ▼
Idempotency
  │
  ▼
Observed Cell
  │
  ▼
Dispatch
  │
  ▼
Handler Cells
```

The principal difference from Firebase listeners is the authoritative idempotency store.

For a Firebase listener:

```text
Firebase
    ↓
Firebase idempotency record
```

For a durable timer:

```text
Local database
    ↓
Local idempotency record
```

The timer infrastructure should therefore not require Firebase-specific idempotency.

---

# Timer event identity

A V1 timer event must have an idempotency key.

The key must represent the **logical business obligation**, rather than merely the clock tick that discovered it.

For example, automatic poll completion should conceptually use a key such as:

```text
poll-auto-complete:<poll-id>
```

rather than:

```text
poll-auto-complete:<timestamp>
```

This allows repeated timer evaluations to converge on the same obligation.

If the backend is unavailable for several timer intervals, the system should not create several independent automatic-completion events for the same poll.

Instead:

```text
Timer evaluation
    │
    ├── Poll ABC123 is overdue
    │
    ▼
poll-auto-complete:ABC123
```

Repeated discovery of the same obligation produces the same key.

---

# Collapsing missed timer occurrences

The V1 timer model deliberately allows multiple timer evaluations to collapse into a single logical observation.

For example:

```text
02:00 evaluation
03:00 evaluation
04:00 evaluation
05:00 evaluation
```

may all discover:

```text
Housekeeping is due
```

or:

```text
Poll ABC123 is overdue
```

The resulting logical event is determined by its idempotency key.

For housekeeping:

```text
housekeeping:debug-cleanup
```

may represent the entire outstanding obligation, regardless of how many scheduled occurrences were missed.

For automatic poll completion:

```text
poll-auto-complete:ABC123
```

represents the specific outstanding business obligation.

The timer infrastructure therefore does not need to preserve every historical clock tick.

---

# Responsibility for missed-event semantics

The generic timer mechanism should **not** decide whether a missed timer occurrence should be:

* discarded;
* replayed;
* collapsed;
* caught up;
* or treated as a persistent business obligation.

That decision belongs to the timer task/application logic.

The timer infrastructure provides the ability to generate observations.

The task defines the meaning of those observations.

This prevents the scheduler from becoming coupled to application-specific business semantics.

---

# Local idempotency

V1 durable timer observations may use the same conceptual idempotency interface as external listeners.

The interface should allow the authoritative idempotency mechanism to be selected independently of the observation source.

Conceptually:

```go
type IdempotencyStore interface {
    // Establish and/or verify an observation key.
}
```

Possible implementations include:

```text
FirebaseIdempotencyStore
LocalIdempotencyStore
```

The exact Go interface is intentionally deferred until the listener and idempotency architecture is specified in detail.

The important architectural requirement is that **idempotency is not inherently tied to Firebase**.

---

# Relationship with the Durable Listener Architecture

Timer observations should ultimately use the same Cell-based dispatch mechanism as database listeners and webhooks.

The source of the observation differs:

```text
Firestore:
    database change
        ↓
    observation

Webhook:
    HTTP request
        ↓
    observation

Timer:
    clock/schedule
        ↓
    observation
```

After observation, the processing model is common:

```text
Observation
    ↓
Observed Cell
    ↓
Idempotency
    ↓
Check / confirmation
    ↓
Dispatch Cell
    ↓
Handler Cells
```

The precise V1 implementation of the timer idempotency path may differ from Firebase because the local database is authoritative for timer observations.

---

# Examples

## Example 1 — Daily housekeeping

Task:

> Delete old debug records once per day.

V0:

```text
02:00
  ↓
create housekeeping Cell
```

If the backend is down:

```text
02:00 missed
03:00 missed
04:00 missed
05:00 backend starts
```

No historical timer events are replayed.

The next available housekeeping opportunity performs the work.

This is acceptable because the operation does not represent a persistent business obligation tied to a particular occurrence.

---

## Example 2 — Automatic poll completion

Task:

> Automatically complete a poll after its deadline if sufficient information is available.

Poll:

```text
ABC123
deadline: 19:00
```

Backend is unavailable:

```text
18:00 → 21:00
```

At restart, the system discovers that:

```text
ABC123 is overdue
```

and generates the logical observation:

```text
poll-auto-complete:ABC123
```

The event is then processed through the normal durable observation pipeline.

The eventual handler may:

* automatically complete the poll if sufficient information exists; or
* generate notifications requesting human intervention if automatic completion is not possible.

The automatic completion operation must itself be idempotent.

Any resulting notification work must also use the appropriate notification idempotency mechanism.

---

# Failure semantics

V0 deliberately accepts loss of timer occurrences during backend downtime for tasks where that is semantically safe.

V1 durable timer observations must instead follow the same convergence principles as other durable observations.

For example:

```text
Timer discovers overdue Poll ABC123
    ↓
local idempotency key established
    ↓
process dies
```

On restart, the timer may discover ABC123 again.

The same logical key is generated:

```text
poll-auto-complete:ABC123
```

and the observation converges on the existing durable state.

Repeated discovery therefore does not result in unbounded duplicate work.

---

# Consequences

## Positive

* V0 timer infrastructure remains extremely simple.
* Housekeeping tasks do not require a durable scheduler database.
* Missed housekeeping occurrences do not need replay.
* The architecture has a clear path to durable business timers.
* Timer idempotency can use the local database rather than Firebase.
* Timer observations can use the same Cell/Dispatch architecture as other listeners.
* Business semantics remain in the application task rather than the generic scheduler.

## Negative

* V0 does not guarantee delivery of timer events across backend downtime.
* V1 requires additional local idempotency state.
* Timer tasks must carefully choose their idempotency keys.
* Some timer tasks will need explicit catch-up/reconciliation logic.
* The distinction between maintenance work and persistent business obligations must be understood by implementers.

---

# Architectural invariants

The following principles should remain true when V1 is implemented:

1. **A timer key identifies a logical obligation, not necessarily a clock tick.**

2. **Repeated discovery of the same logical obligation must converge on the same idempotency key.**

3. **The timer infrastructure must not assume that every missed occurrence needs replay.**

4. **The application task determines the semantics of missed work.**

5. **Timer idempotency must be capable of using a local authoritative store.**

6. **Durable timer observations should ultimately use the same Cell-based dispatch mechanism as other observations.**

7. **V0 may deliberately omit durable timer observations where the task semantics make lost occurrences acceptable.**

---

# Open questions

The following are deferred:

* Exact Go interfaces for timer schedules and timer observations.
* Exact local idempotency schema.
* Whether V1 timer observations require a separate Check Cell, given that the local database is authoritative.
* How timers discover overdue business obligations after downtime.
* Whether timer schedules are evaluated using UTC or another clock representation.
* How recurring business events construct their logical keys.
* Whether a timer may have multiple outstanding logical obligations simultaneously.
* How timer configuration is persisted and modified at runtime.

These questions should be resolved when implementing V1 durable timer support rather than prematurely complicating the V0 timer implementation.
