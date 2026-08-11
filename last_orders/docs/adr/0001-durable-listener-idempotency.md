# ADR: Durable Listener Observation and External Idempotency

**Status:** Proposed
**Date:** 2026-08-11

## Context

The backend receives events from external sources such as:

* Firebase/Firestore document changes;
* scheduled/time-based housekeeping;
* webhook requests.

A listener must be able to observe an event and cause the corresponding application work to be performed **at least once**, without repeatedly processing the same external event.

The backend uses Cellar as its durable execution mechanism. Cells are comparatively cheap, whereas calls to external services such as Firebase are comparatively expensive and should therefore be minimised.

A previous design considered allowing a listener to perform an idempotency check directly against Firebase before creating work. This creates an undesirable failure mode:

1. The listener records the event as processed in Firebase.
2. The backend fails before the corresponding Cell is durably created.
3. The event is subsequently suppressed as a duplicate.
4. The application work is lost.

The architecture therefore needs to separate:

* observation of an external event;
* durable recording of that observation within Cellar;
* establishment of external idempotency;
* verification of the external idempotency state;
* dispatch of application work.

## Decision

Listeners will **not directly perform the external idempotency operation**.

Instead, observing a new event causes an **Observed Cell** to be created.

The creation of the Observed Cell and its corresponding local application-state record will occur in a single local database transaction.

The resulting processing pipeline is:

```text
External event
      │
      ▼
   Listener
      │
      │ creates
      ▼
┌───────────────────────┐
│ Observed Cell         │
│ local state: Pending  │
└───────────┬───────────┘
            │
            ▼
     Firebase idempotency
            │
            ▼
      local state: Pushed
            │
            ▼
        Check Cell
            │
            ▼
     Firebase verification
            │
            ▼
┌──────────────────────────────────┐
│ Local DB transaction              │
│                                  │
│ state = Present                  │
│ create Dispatch Cell             │
└──────────────────────────────────┘
            │
            ▼
       Dispatch Cell
            │
            ▼
     Handler Work Cells
```

Cells are therefore responsible for progressing an observation through the external idempotency protocol.

A Cell performs one logical idempotent operation.

## Local observation state

Each observed event has a local record identified by:

```text
(listener_id, event_key)
```

The V0 state machine is:

```text
Pending → Pushed → Present
```

### Pending

The Listener has observed the event and the Observed Cell has been durably created.

The corresponding external idempotency record has not yet been established or confirmed.

### Pushed

The external idempotency record has been successfully established, or has been found to already exist.

The event is now known to have an external idempotency record.

### Present

The external idempotency record has been independently verified.

The transition to `Present` is performed atomically with creation of the Dispatch Cell.

Therefore the system must not reach:

```text
Present
```

without also having durably created the Dispatch Cell.

## Firebase idempotency store

Firebase/Firestore will contain a dedicated idempotency hierarchy:

```text
last_orders_idempotency/
    new_poll/
        <event-key>
    completed_poll/
        <event-key>
    chat/
        <event-key>
    admin_delete/
        <event-key>
    ...
```

Each Listener has a stable identifier.

Each observation generates an event-specific idempotency key.

For example:

```text
last_orders_idempotency/new_poll/ABC123
```

represents the observation of poll `ABC123` by the `new_poll` listener.

For Chat:

```text
last_orders_idempotency/chat/<firebase-document-id>
```

represents the observation of that particular chat document by the Chat listener.

The listener-specific key construction is application-specific. The idempotency infrastructure does not need to understand the meaning of the key.

## Why collections are used

Each listener gets a Firestore collection containing one document per idempotency key.

The alternative of storing all keys for a listener in a single Firestore document is rejected.

A collection of documents is preferred because it:

* avoids the Firestore document size limit;
* avoids a single hot document receiving all writes;
* permits independent concurrent operations;
* makes individual idempotency records independently addressable;
* avoids requiring large map updates for every event.

The logical identity remains:

```text
(listener_id, event_key)
```

rather than the physical Firestore path.

## Establishing idempotency

The Observed Cell establishes the external idempotency record.

The fundamental operation is an atomic "create if absent".

Conceptually:

```text
Create:
    last_orders_idempotency/{listener}/{key}
```

Possible outcomes are:

### Created

The event has successfully established its external idempotency record.

Local state progresses:

```text
Pending → Pushed
```

### Already exists

The event has already established its external idempotency record.

This is treated as an idempotent success.

Local state progresses:

```text
Pending → Pushed
```

### Other error

The local state remains `Pending`.

The Cell is retryable.

## Optional existence check

The Observed Cell may optionally perform an existence read before attempting the create.

This is an optimisation only.

Correctness must not depend on the preliminary read because concurrent Cells may race:

```text
Cell A: read → absent
Cell B: create → success
Cell A: create → already exists
```

`already exists` must therefore be treated as successful idempotent completion.

V0 may omit the preliminary read to minimise Firebase calls.

## Check Cell

After successfully establishing the external idempotency record, the Observed Cell creates a Check Cell as part of its Cell result.

The Check Cell independently reads the Firebase idempotency document.

Its purpose is to establish that the external idempotency state is actually present before application work is released.

If the document is present, the Check Cell performs a local transaction which:

1. changes the observation state from `Pushed` to `Present`;
2. creates the Dispatch Cell.

These two operations must be atomic.

If the Firebase document cannot be confirmed, the Check Cell fails and is retryable.

## Crash and failure semantics

The architecture deliberately uses Cells to make every important transition recoverable.

### Failure before Observed Cell creation

The Listener's local transaction either succeeds or fails.

If it fails, no observation has been accepted by Cellar.

If it succeeds, the Observed Cell exists.

### Failure after Observed Cell creation but before Firebase interaction

The Observed Cell remains retryable with local state `Pending`.

No event is lost.

### Firebase create succeeds but the process fails before local state is updated

The local state remains `Pending`.

On retry, the Firebase idempotency document already exists.

The operation therefore resolves as an idempotent success and progresses to `Pushed`.

### Failure after `Pushed` but before Check Cell creation

The Observed Cell's result/transaction must ensure that the Check Cell is durably created as part of the appropriate Cell transition.

Normal Cell recovery must allow the operation to resume.

### Firebase verification fails

The Check Cell remains retryable.

Local state remains `Pushed`.

### Firebase verification succeeds but process fails before local transaction

The Check Cell retries.

The Firebase document remains present, so verification succeeds again.

### Local transaction updates `Present` and creates Dispatch Cell

These operations are atomic.

Therefore there must not be a durable state in which:

```text
Present
```

exists without the corresponding Dispatch Cell.

## Retry semantics

The design intentionally permits repeated Firebase operations.

For example, after an uncertain failure:

```text
Pending
    ↓
CREATE Firebase record
    ↓
network/process failure
```

the retry may perform another operation against the same idempotency key.

Because the Firebase operation itself is idempotent, this is safe.

Cells are therefore the preferred mechanism for handling uncertainty rather than attempting to make listeners maintain complex retry state.

## Listener responsibilities

A Listener is responsible for:

* observing its external source;
* decoding the source event;
* determining the event's listener identity;
* constructing the event-specific idempotency key;
* causing the corresponding Observed Cell to be created.

A Listener is **not** responsible for:

* performing the Firebase idempotency write;
* dispatching application Handlers;
* executing application work;
* maintaining retry state.

## Cell responsibilities

### Observed Cell

Responsible for:

* establishing external idempotency;
* transitioning local observation state from `Pending` to `Pushed`;
* creating the Check Cell.

### Check Cell

Responsible for:

* verifying the external idempotency record;
* transitioning local observation state from `Pushed` to `Present`;
* atomically creating the Dispatch Cell.

### Dispatch Cell

Responsible for:

* identifying the Handlers registered for the observed event;
* creating one Work Cell per registered Handler.

The Dispatch Cell does not execute the application work itself.

## Handler dispatch

Once the observation has reached `Present`, the Dispatch Cell obtains the configured Handlers for the event type.

For example:

```text
new_poll
    ├── PushNotificationHandler
    └── EmailNotificationHandler
```

The Dispatch Cell creates an independent Work Cell for each Handler.

This means that a single observed event can fan out into an arbitrary number of independent Cell executions.

## Consequences

### Positive

* Listeners remain simple and cheap.
* Firebase calls are performed by Cells rather than listeners.
* Expensive external operations become retryable durable work.
* Local state provides an explicit processing state machine.
* Firebase remains the external authority for idempotency.
* Cellar remains responsible for durable execution.
* The critical `Present + Dispatch Cell creation` transition is atomic.
* Multiple Handlers can independently process the same observed event.
* The architecture applies equally to database, timer, and webhook listeners.

### Negative

* Processing an event requires multiple Cells.
* A single event may require multiple Firebase calls.
* The local database contains additional observation state.
* The design is more sophisticated than simply checking Firebase from the Listener.
* The relationship between Cell lifecycle and observation-state lifecycle must be carefully specified.
* Firebase and Cellar still form two separate persistence systems; there is no distributed transaction between them.

## Rejected alternatives

### Listener performs Firebase idempotency directly

Rejected because a successful Firebase write followed by process failure before Cell creation can permanently suppress application work.

### Single Firestore document containing all keys

Rejected because of document size, contention, concurrency, and operational complexity.

### Read-then-write idempotency

Rejected as the correctness mechanism because a read followed by a create is vulnerable to races.

An atomic create-if-absent operation is required.

### Treat Firebase idempotency state as the processing queue

Rejected.

Firebase records establish external idempotency; Cellar remains the durable execution mechanism.

## Open questions

The following are deliberately not resolved by this ADR:

1. The exact Go interfaces for Listeners, Events, Cells, and the idempotency store.
2. The exact schema of the local observation table.
3. Whether the Firebase idempotency document should contain metadata beyond the listener/key and timestamps.
4. The exact mechanism used by the Firestore implementation to perform create-if-absent.
5. How obsolete idempotency records are eventually cleaned up, if at all.
6. The precise failure semantics when a Cell result transaction itself fails.
7. Whether the preliminary Firebase existence read is worthwhile in V0.
8. The exact relationship between event decoding and event-specific key construction.
9. Whether all listeners use this pattern identically or whether some listener types require specialised observation semantics.

## Architectural invariant

The central invariant established by this ADR is:

> **An external event is not considered fully observed until its external idempotency record has been verified and the Dispatch Cell has been durably created.**

The local state therefore provides a recoverable path:

```text
Observed
   ↓
Pending
   ↓
Pushed
   ↓
Present + Dispatch Cell
```

No step relies on a single cross-system transaction. Instead, each uncertain external operation is isolated into a retryable Cell and the local database provides the durable state machine connecting those operations.
