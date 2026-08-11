# CDD: Idempotency Components

## 1. Purpose

The backend performs work in response to observations made by listeners.

The same observation may be encountered more than once due to:

* listener retries;
* process restarts;
* webhook retries;
* concurrent Cells processing the same observation;
* delayed or duplicated external notifications;
* timer events being observed more than once.

The backend therefore requires a consistent mechanism for ensuring that a particular piece of work is only dispatched once.

This document defines the common idempotency model and the implementations required by the backend.

The central principle is:

> **An idempotency component determines whether a particular `(component, key)` has already been processed. If it has not, it atomically records that the key has been processed and creates the downstream Fanout Cell. If it has already been processed, execution terminates without creating a Fanout Cell.**

The mechanism is deliberately implemented as Cells rather than as synchronous infrastructure surrounding listeners.

---

# 2. Terminology

### Idempotency Component

A logical component responsible for deduplicating a particular class of observations.

Examples:

* `NewPoll`
* `CompletedPoll`
* `Chat`
* `AdminDelete`
* a future timer-based operation

An idempotency component has a stable name and defines how an observation is converted into an idempotency key.

### Idempotency Key

A string uniquely identifying an observation for the purposes of a particular idempotency component.

The key only needs to be unique **within its component**.

For example:

```text
NewPoll / abc123
Chat / message456
```

may both legitimately exist because the component name forms part of the logical identity.

### Fanout Cell

The Cell created when an idempotency operation establishes that an observation is new.

The Fanout Cell is responsible for dispatching the resulting work to the application handlers registered for that observation.

The idempotency mechanism itself does not execute application work.

---

# 3. Architectural Principle

Idempotency is a filtering stage in the Cell pipeline.

The general pattern is:

```text
Listener
   │
   ▼
Observed / Idempotency Cell
   │
   ▼
Idempotency Component
   │
   ├── already processed ───────► finish
   │
   └── newly processed
              │
              ▼
         Fanout Cell
              │
              ▼
       Application Handlers
```

The listener does not attempt to maintain idempotency itself.

Instead, the listener creates a Cell representing the observation.

The Cell performs the idempotency operation.

This ensures that the observation itself is durable in Cellar before the idempotency operation is attempted.

---

# 4. Atomicity Requirement

The most important invariant is:

> **Establishing idempotency and creating the Fanout Cell must be atomic.**

If establishing the idempotency state succeeds but creation of the Fanout Cell fails, the system must not be left in a state where the work is permanently suppressed.

Conversely, if the Fanout Cell is created, the idempotency state must indicate that this observation has been claimed for dispatch.

For a local idempotency implementation, these operations occur in a single Base DB transaction.

For a Firebase-backed implementation, additional intermediate state is required because Firebase is an external authority.

---

# 5. Local Idempotency

Local idempotency is used when the Base DB is itself the authoritative source of idempotency state.

Examples include:

* timer events;
* housekeeping operations;
* other backend-internal observations.

There is no external idempotency authority to reconcile.

## 5.1 State

The local implementation has two logical states:

```text
NotPresent
Present
```

A newly observed key is initially absent.

The idempotency Cell performs an atomic transaction:

```text
NotPresent
    │
    └── atomic transition + Fanout creation
                │
                ▼
             Present
```

If the key is already `Present`, the Cell completes without creating a Fanout Cell.

## 5.2 Required invariant

The following must be one atomic Base DB transaction:

```text
idempotency key: NotPresent → Present
+
create Fanout Cell
+
delete/complete current Cell
```

Therefore concurrent Cells attempting the same key cannot both create Fanout Cells.

Exactly one succeeds in establishing the transition.

The others observe that the key is already `Present` and terminate.

## 5.3 Local state retention

Local idempotency state may be retained according to the requirements of the individual component.

Unlike Firebase-backed idempotency, there is no external durable authority from which the local state can later be reconstructed.

Therefore deletion of local idempotency state is a semantic decision for the individual component.

For timer events, for example, state may eventually be compacted or deleted according to the event's retention requirements.

---

# 6. Firebase Idempotency

Firebase-backed idempotency is required when the durable idempotency authority must survive beyond the local lifetime of the backend.

The Firebase implementation uses a dedicated Firestore collection hierarchy.

Conceptually:

```text
last_orders_idempotency/
    NewPoll/
        <key>
    CompletedPoll/
        <key>
    Chat/
        <key>
    ...
```

Each idempotency component has its own namespace.

Each key is represented by a Firebase document.

The existence of the document represents the durable fact that the observation has already been established as processed.

---

# 7. Firebase State Machine

Because Firebase is an external system, the Firebase implementation requires an intermediate state.

The local state machine is:

```text
Pending
   │
   │ Push Cell
   ▼
Firebase check
   │
   ├── key already exists ─────────────► Present
   │                                      │
   │                                      ▼
   │                                    Finish
   │
   └── key absent
          │
          ▼
       Firebase Create
          │
          ▼
        Pushed
          │
          ▼
       Check Cell
          │
          ├── key missing ──────────────► retry
          │
          └── key exists
                 │
                 ▼
       atomic Pushed → Present
       + create Fanout Cell
```

## 7.1 Pending

`Pending` means:

> The observation has been durably recorded locally, but the Firebase idempotency authority has not yet been conclusively incorporated into the local state machine.

The Pending state must therefore be safely retryable.

## 7.2 Push

The Push Cell determines whether the Firebase idempotency document already exists.

If it exists:

```text
Pending → Present
```

and processing terminates without creating a Fanout Cell.

This is an important optimisation and is also what allows local idempotency records to be safely discarded after completion.

If the Firebase document does not exist, the Push Cell attempts to create it.

After a successful create:

```text
Pending → Pushed
```

The Push Cell then creates a Check Cell.

## 7.3 Pushed

`Pushed` means:

> The Firebase create operation was reported as successful, but the backend has not yet completed the read-back verification required to establish the local `Present` state.

The Push Cell must not assume that this is sufficient to complete the operation.

If the process dies after the Firebase create but before the Check Cell executes, recovery must be able to continue from `Pushed`.

A Push Cell encountering `Pushed` must therefore still be capable of retrying the Firebase operation.

This is safe because the Firebase create operation is itself idempotent with respect to the document key.

## 7.4 Present

`Present` means:

> The Firebase idempotency authority contains the idempotency document and the local state machine has conclusively established that fact.

A Check Cell that observes `Present` does not create another Fanout Cell.

---

# 8. Fanout Creation

The Fanout Cell is created only by the execution that successfully establishes the new idempotency state.

For the Firebase implementation, this is the Check Cell which successfully performs:

```text
Pushed → Present
```

The state transition and Fanout Cell creation must be atomic in the local Base DB transaction.

This gives the following concurrency behaviour:

```text
                  Pushed
                    │
           ┌────────┴────────┐
           ▼                 ▼
        Check A           Check B
           │                 │
           ▼                 ▼
    wins Pushed→Present   loses transition
           │                 │
           ▼                 ▼
    creates Fanout        observes Present
                             │
                             ▼
                           Finish
```

Exactly one Check Cell can therefore produce the Fanout Cell.

---

# 9. Firebase Idempotency Retention

Once an idempotency operation reaches `Present`, the local idempotency record may be deleted.

The Firebase document remains the durable authority.

If the same observation is subsequently encountered again:

```text
new observation
     │
     ▼
new local Pending record
     │
     ▼
Push Cell
     │
     ▼
Firebase key exists
     │
     ▼
Present
     │
     ▼
Finish
```

No Fanout Cell is created.

This means local Firebase-idempotency state represents **in-flight processing state**, rather than being a permanent historical database.

The Firebase idempotency document provides the permanent deduplication record.

---

# 10. Common Behaviour

Despite their different implementations, local and Firebase idempotency share the same conceptual contract:

```text
             observation
                  │
                  ▼
          idempotency key
                  │
                  ▼
       ┌─────────────────────┐
       │ Has this been       │
       │ established before? │
       └─────────┬───────────┘
                 │
          ┌──────┴──────┐
          │             │
         Yes            No
          │             │
          ▼             ▼
       Finish       Establish key
                        │
                        ▼
                 Create Fanout
```

The difference is solely in how the authoritative state is established.

### Local

```text
NotPresent → Present
```

### Firebase

```text
Pending → Pushed → Present
```

The additional Firebase states exist only because the authoritative state is external to the Base DB.

---

# 11. Failure and Recovery Requirements

An idempotency implementation must tolerate process termination at any point.

In particular:

### Before local observation is recorded

The listener must create the observation Cell durably before relying on downstream processing.

If the process dies before this happens, the listener may observe the source again.

This is acceptable.

### After observation is recorded but before idempotency processing

The Cell remains available for execution.

### During Firebase creation

The Push Cell may be retried.

If Firebase already contains the key, subsequent processing converges on `Present`.

### After Firebase creation but before local `Pushed` state

The operation may be retried.

The existing Firebase key causes convergence rather than duplicate dispatch.

### After `Pushed` but before Check execution

The Check Cell is retried or recovered.

### During concurrent Check execution

Only one Check Cell can successfully establish:

```text
Pushed → Present
```

and therefore only one can create the Fanout Cell.

### After Present but before local cleanup

The operation is already semantically complete.

Cleanup may be retried independently.

---

# 12. Idempotency Component Interface

The precise Go interface should remain deliberately small.

The application should not need to know whether an idempotency component is backed by Firebase or the local database.

Conceptually, an idempotency component provides:

```go
type IdempotencyComponent interface {
    KeyFor(fact Fact) (string, error)
    Observe(key string, fanout []cellar.CellRequest) error
}
```

However, this is a **conceptual interface**, not necessarily the final Go API.

The actual implementation should be expressed in terms of Cells and Cell results so that:

* idempotency work is itself retryable;
* Fanout Cell creation participates in Cell completion atomicity;
* application transactions can participate in the same Base DB transaction;
* the Firebase implementation can expose its intermediate `Pending`, `Pushed`, and `Present` states without leaking those concerns to listeners.

The concrete API should therefore be settled alongside the Cell/Cell Result contracts.

---

# 13. Relationship to Listeners

Listeners are responsible only for recognising observations and creating the initial Cell.

They do not perform the idempotency operation themselves.

For example:

```text
New Poll Listener
       │
       ▼
Observed Cell
       │
       ▼
NewPoll Idempotency Component
       │
       ├── already present → finish
       │
       └── newly established
                  │
                  ▼
             Fanout Cell
                  │
             ┌────┴────┐
             ▼         ▼
        Push Handler  Email Handler
```

This keeps listener implementations simple and ensures that duplicate observations have the same semantics regardless of why they occurred.

---

# 14. Relationship to Timer Events

Timer events may use the local idempotency implementation.

This allows timer observations to use exactly the same conceptual machinery as Firebase listeners without requiring Firebase to be involved.

For example:

```text
Timer observes "poll auto-complete for poll X"
                │
                ▼
          Observed Cell
                │
                ▼
       Local idempotency
       key = poll:X:auto-complete
                │
          ┌─────┴─────┐
          │           │
        new         already seen
          │           │
          ▼           ▼
       Fanout       Finish
```

This is intended to support future timer semantics where missed timer events must still be processed rather than simply discarded.

---

# 15. Design Invariants

The following invariants are normative:

1. **An observation must be durably represented by a Cell before relying on downstream processing.**

2. **Listeners do not perform idempotency directly.**

3. **Idempotency is implemented as a Cell-level filtering stage.**

4. **A previously established idempotency key must never create another Fanout Cell.**

5. **Only the execution which successfully establishes a new idempotency state may create the Fanout Cell.**

6. **For local idempotency, establishing `Present` and creating the Fanout Cell are one atomic Base DB transaction.**

7. **For Firebase idempotency, the `Pushed → Present` transition and Fanout Cell creation are one atomic Base DB transaction.**

8. **Firebase `Present` is authoritative and durable beyond the lifetime of local idempotency state.**

9. **Local Firebase-idempotency records may be deleted after reaching `Present`.**

10. **Re-observing a Firebase key after local state has been deleted must converge to `Present` without creating a Fanout Cell.**

11. **All idempotency operations must be safe to retry.**

12. **Concurrent attempts to process the same idempotency key must converge on exactly one Fanout Cell.**
