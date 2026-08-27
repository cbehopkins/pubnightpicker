# CDD: Idempotency and Fact Emission

## 1. Purpose

The backend performs work in response to Facts observed by listeners and other backend mechanisms.

The same underlying observation may be encountered more than once due to:

* listener retries;
* process restarts;
* webhook retries;
* concurrent Cells processing the same observation;
* delayed or duplicated external notifications;
* timer events being observed more than once.

The backend therefore requires a consistent mechanism ensuring that a particular Fact is **emitted for application processing at most once for a particular idempotency key**.

This document defines the architecture of that mechanism.

The central principle is:

> **The Idempotency Layer determines whether a Fact with a particular idempotency key may be emitted. When it establishes that the Fact is new, it durably schedules a standard Cellar Fanout Cell which emits the Fact to all registered handlers. If the Fact has already been established, the Idempotency Sequence terminates without creating another Fanout Cell.**

The Idempotency Layer does not execute application handlers itself.

Its responsibility ends when it has atomically established the idempotency state and scheduled the Fact's Fanout Cell.

---

# 2. Architectural Model

The general processing model is:

```text
Observation
    │
    ▼
Idempotency Sequence
    │
    ├── duplicate ───────────────────► terminate
    │
    └── newly established
              │
              ▼
         Fact Emission
              │
              ▼
         Fanout Cell
              │
      ┌───────┼────────┐
      ▼       ▼        ▼
   Handler  Handler  Handler
```

An observation does not directly invoke application handlers.

Instead:

1. an observation produces a potential Fact and an idempotency key;
2. the Idempotency Layer determines whether that Fact may be emitted;
3. if so, the Idempotency Layer creates a standard Fanout Cell;
4. the Fanout Cell dispatches the Fact to all registered handlers.

This separates:

* **observation** — recognising that something happened;
* **idempotency** — determining whether the Fact may be emitted;
* **fact emission** — scheduling delivery of the Fact;
* **application handling** — performing application-specific work.

---

# 3. Terminology

## 3.1 Observation

An Observation is information recognised by a listener or backend mechanism which may result in a Fact being emitted.

Examples include:

* a Firebase document change;
* an incoming webhook;
* a completed timer;
* an internally generated backend event.

An Observation is not necessarily unique.

The same underlying event may be observed more than once.

---

## 3.2 Fact

A Fact is the application-level statement which is emitted for processing.

Examples might include:

* `NewPoll`;
* `CompletedPoll`;
* `Chat`;
* `AdminDelete`.

Facts are delivered to registered application handlers through a Fanout Cell.

---

## 3.3 Idempotency Key

An Idempotency Key uniquely identifies an observation for the purposes of a particular idempotency component.

The key need only be unique within its component.

Conceptually:

```text
component + key
```

forms the complete idempotency identity.

For example:

```text
NewPoll / abc123
Chat    / abc123
```

are distinct identities.

Each Fact-producing observation must therefore provide:

```text
Fact
+
Idempotency Component
+
Idempotency Key
```

---

## 3.4 Idempotency Component

An Idempotency Component defines the idempotency semantics for a particular class of Facts.

Examples include:

* `NewPoll`;
* `CompletedPoll`;
* `Chat`;
* `AdminDelete`;
* future timer-based operations.

An Idempotency Component determines:

* how an observation derives its key;
* whether idempotency is local-only or externally backed;
* which Fact is emitted when the key is newly established.

The component does not itself execute application handlers.

---

## 3.5 Fanout Cell

A Fanout Cell is a standard Cellar Cell responsible for dispatching a Fact to all registered handlers.

Conceptually:

```text
Fact
   │
   ▼
Fanout Cell
   │
   ├── Handler A
   ├── Handler B
   └── Handler C
```

The Fanout Cell is created only when the Idempotency Layer has successfully established that the Fact may be emitted.

---

# 4. Core Architectural Principle

Idempotency is an **emission gate**.

Its question is not:

> "Has this handler already run?"

Its question is:

> **"May this Fact be emitted?"**

Therefore:

```text
Potential Fact
      │
      ▼
Idempotency
      │
      ├── already established
      │         │
      │         ▼
      │    terminate sequence
      │
      └── newly established
                │
                ▼
           create Fanout
                │
                ▼
            emit Fact
```

Application handlers may themselves have their own retry or transactional guarantees.

Those concerns are separate from the idempotency decision which determines whether the Fact is emitted in the first place.

---

# 5. Cellar Sequence State is the Processing State Machine

Idempotency processing is implemented as a Cell Sequence.

The sequence's current Step is itself part of the durable processing state.

This is important.

The backend does not need to duplicate the processing state machine into separate database fields such as:

```text
Pending
Pushed
Present
```

where those states merely describe which Cell Step should execute next.

Instead:

> **The durable Cell Sequence and its current Step represent the idempotency processing state machine.**

Cellar guarantees that a Step's transactional effects and advancement to the next Step are committed atomically.

Therefore, conceptually:

```text
Current Step
+
Transaction Payload Effects
+
Advance to Next Step
```

are committed together.

A process cannot recover into a state where the Step's transactional effects have committed but the Cell still appears to be at the previous Step.

Likewise, if the Step has not advanced, its transaction effects are not committed.

This property is fundamental to the idempotency architecture described in this document.

---

# 6. Local Idempotency

Local Idempotency is used when the local Base DB is itself the authoritative source of the idempotency state.

Examples may include:

* timer events;
* housekeeping operations;
* backend-internal observations;
* other operations which do not require an external authority.

The local implementation is deliberately simple.

---

## 6.1 Local Idempotency Sequence

The sequence consists conceptually of:

```text
Step 1 — Check and Establish
            │
            ▼
Step 2 — Fact Emission
```

Step 2 is implemented by scheduling a standard Fanout Cell.

---

## 6.2 Step 1 — Check and Establish

The Idempotency Step derives the identity:

```text
Component
+
Key
```

It checks the local idempotency store.

### If the key exists

The Fact has already been established.

The sequence terminates:

```text
Local key present
        │
        ▼
   Kill Sequence
```

No Fanout Cell is created.

### If the key does not exist

The Step returns a transactional Cell result containing:

```text
- insert local idempotency key;
- create the Fanout Cell;
- complete the current Step/Sequence.
```

These effects are committed atomically.

Conceptually:

```text
Key absent
    │
    ▼
Atomic Cellar Transaction
    ├── insert idempotency key
    ├── create Fanout Cell
    └── complete Idempotency Sequence
```

---

## 6.3 Local Atomicity Requirement

The following must be part of the same Base DB transaction:

```text
establish local idempotency key
+
create Fanout Cell
+
complete/advance the Idempotency Cell
```

Therefore, the system cannot durably reach either of these invalid states:

```text
Key present
but
no Fanout Cell was scheduled
```

or:

```text
Fanout Cell scheduled
but
idempotency key was not established
```

Concurrent attempts converge because only one transaction can successfully establish the previously absent key.

The successful transaction creates the Fanout Cell.

Other attempts subsequently observe the key and terminate.

---

# 7. Firebase-Backed Idempotency

Firebase-backed idempotency is used when an external Firebase/Firestore store is the durable authority for whether a Fact has previously been established.

The Firebase store contains a durable idempotency record.

Conceptually:

```text
idempotency/
    NewPoll/
        <key>
    CompletedPoll/
        <key>
    Chat/
        <key>
```

The remote record is the durable authority.

The local database maintains:

1. the processing state represented by the Idempotency Cell Sequence; and
2. a local record of keys which have already been established or claimed.

The local key therefore serves two purposes:

* preventing repeated local processing;
* caching the known state of the remote authority.

---

# 8. Firebase Idempotency Sequence

Firebase-backed idempotency consists conceptually of three Steps:

```text
Step 1 — Check
      │
      ▼
Step 2 — Populate Remote
      │
      ▼
Step 3 — Emit Fact
```

The Cell Sequence provides the durable state machine.

---

# 9. Step 1 — Check

The Check Step examines the idempotency key.

The checks occur conceptually in the following order.

---

## 9.1 Check Local Store

The local idempotency store is checked first.

### If the local key exists

The observation has already been claimed or established locally.

The sequence terminates:

```text
Local key present
        │
        ▼
   Kill Sequence
```

No remote call is required.

The local store therefore provides the fast path for repeated observations.

---

## 9.2 Check Remote Store

If the local key does not exist, the Firebase authority is checked.

### If the remote key exists

The Fact has already been established.

The Step atomically:

```text
- records the local idempotency key;
- terminates the sequence.
```

Conceptually:

```text
Local absent
Remote present
      │
      ▼
Atomic Cellar Transaction
    ├── cache local key
    └── terminate sequence
```

No Fanout Cell is created.

The local key acts as a cache of the established remote state.

Future duplicate observations therefore terminate locally without requiring another Firebase lookup.

---

## 9.3 If Both Local and Remote Keys are Absent

The current sequence is responsible for proceeding with establishment of the remote key.

The Step atomically:

```text
- inserts the local idempotency key;
- advances the Cell Sequence to Step 2.
```

Conceptually:

```text
Local absent
Remote absent
      │
      ▼
Atomic Cellar Transaction
    ├── insert local key
    └── advance to Remote Population
```

This atomicity is essential.

After recovery, only one of the following states can exist.

### Transaction did not commit

```text
Local key absent
Sequence still at Step 1
```

The Check Step runs again.

### Transaction committed

```text
Local key present
Sequence now at Step 2
```

The system therefore knows that this sequence has progressed to remote establishment.

There is no recoverable state in which the local key has been committed while the sequence still appears to be at Step 1.

---

# 10. Step 2 — Populate Remote

Step 2 establishes the idempotency key in Firebase.

Conceptually:

```text
Local key established
Sequence at Step 2
        │
        ▼
Create/update remote idempotency record
```

The remote operation must be safely retryable.

In particular, repeating the operation for the same:

```text
Component
+
Key
```

must not create a different idempotency identity or cause duplicate Fact emission.

For example:

```text
attempt 1 → remote write succeeds
attempt 2 → same key written again safely
attempt 3 → same key written again safely
```

---

## 10.1 Failure During Remote Population

Suppose:

```text
Remote write
      │
      ▼
succeeds
      │
      💥
process terminates
```

before the Step completes.

The Step completion has not been committed.

On recovery:

```text
Local key present
Sequence still at Step 2
```

The Step is retried.

Repeating the remote operation is safe because it operates on the same idempotency identity.

The sequence eventually converges on successful completion of Step 2.

---

## 10.2 Successful Remote Population

Once the remote operation has successfully completed, the Cell Step completes and advances atomically to Step 3.

Therefore recovery produces:

```text
Local key present
Sequence at Step 3
Remote key established
```

The sequence may now emit the Fact.

---

# 11. Step 3 — Fact Emission

Fact emission is the responsibility of the Idempotency Layer.

The Idempotency Layer does not directly execute handlers.

Instead, it creates a standard Cellar Fanout Cell containing the Fact and its registered destinations.

The Step atomically:

```text
- creates the Fanout Cell;
- completes the Idempotency Sequence.
```

Conceptually:

```text
Remote key established
        │
        ▼
Atomic Cellar Transaction
    ├── create Fanout Cell
    └── complete Idempotency Sequence
```

The Fanout Cell then performs normal Cellar fanout processing.

---

# 12. Fact Handler Registration

Plugins and application components register interest in **Facts**.

They should not need to understand:

* whether the Fact uses local idempotency;
* whether the Fact uses Firebase-backed idempotency;
* the remote collection structure;
* the internal Cell Sequence used to establish idempotency.

Conceptually, a Plugin declares:

```text
I handle Fact X
```

The Fact Emission infrastructure records that registration.

When the Idempotency Layer establishes that a Fact may be emitted:

```text
Fact
   │
   ▼
Fanout Cell
   │
   ├── registered handler A
   ├── registered handler B
   └── registered handler C
```

Therefore:

> **Plugins register against Facts and Fact Emission. The Idempotency Layer determines whether an emission of that Fact is permitted.**

The public registration API should not leak the idempotency implementation.

---

# 13. Relationship Between Idempotency and Fanout

The responsibilities are deliberately separate.

## Idempotency Layer

Responsible for:

* deriving or receiving the idempotency identity;
* checking whether the Fact has already been established;
* establishing the required local and/or remote state;
* deciding whether the Fact may be emitted;
* atomically scheduling the Fanout Cell when emission is permitted.

## Fanout Cell

Responsible for:

* dispatching the Fact to all registered handlers;
* participating in normal Cellar handler processing and retry semantics.

Therefore:

> **Idempotency authorises Fact emission. Fanout performs Fact delivery.**

---

# 14. Firebase Processing Model

The complete Firebase-backed flow is:

```text
Observation
    │
    ▼
Potential Fact + Idempotency Key
    │
    ▼
┌──────────────────────────────┐
│ Step 1 — Check Local         │
└──────────────┬───────────────┘
               │
        ┌──────┴──────┐
        │             │
      Present       Absent
        │             │
        ▼             ▼
      Kill      Check Firebase
                     │
              ┌──────┴───────┐
              │              │
           Present         Absent
              │              │
              ▼              ▼
       Atomic transaction    Atomic transaction
       ├─ add local key      ├─ add local key
       └─ kill sequence      └─ advance Step 2
                                   │
                                   ▼
                         ┌──────────────────┐
                         │ Step 2           │
                         │ Populate Remote  │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │ Step 3           │
                         │ Create Fanout    │
                         └────────┬─────────┘
                                  │
                                  ▼
                             Fanout Cell
                                  │
                       ┌──────────┼──────────┐
                       ▼          ▼          ▼
                    Handler A  Handler B  Handler C
```

---

# 15. Local Processing Model

The local-only flow is:

```text
Observation
    │
    ▼
Potential Fact + Idempotency Key
    │
    ▼
Check Local Key
    │
 ┌──┴────┐
 │       │
Yes      No
 │       │
 ▼       ▼
Kill   Atomic Transaction
       ├── insert local key
       ├── create Fanout Cell
       └── complete sequence
                │
                ▼
            Fanout Cell
                │
        ┌───────┼────────┐
        ▼       ▼        ▼
     Handler  Handler  Handler
```

---

# 16. Local Key Semantics

The meaning of the local key differs slightly between the two implementations.

## Local-only idempotency

The local key is the authoritative idempotency record.

```text
Local key present
        =
Fact already established
```

## Firebase-backed idempotency

The remote Firebase record is authoritative.

The local key acts as:

* a durable local cache of established remote state; and
* a marker that local processing has claimed the idempotency identity.

The Cell Sequence determines the current processing stage.

For example:

```text
Local key absent
Sequence Step 1
    =
not yet locally claimed
```

```text
Local key present
Sequence Step 2
    =
remote establishment in progress
```

```text
Local key present
Sequence Step 3
    =
remote establishment complete;
Fact emission pending
```

```text
Local key present
No active sequence
    =
Fact has already been established locally
```

Thus:

> **The local key identifies the idempotency identity. The Cell Sequence identifies the current processing stage.**

---

# 17. Retry and Recovery

All idempotency Steps must be safe to retry.

The architecture relies on Cellar's guarantees that transactional effects and Cell Step advancement are atomic.

---

## 17.1 Failure During Step 1

If the transaction which:

```text
adds the local key
+
advances the sequence
```

does not commit, neither effect becomes durable.

Recovery returns to:

```text
local key absent
sequence still at Step 1
```

The check is repeated.

---

## 17.2 Failure During Step 2

If the remote operation succeeds but the Cell Step does not complete:

```text
remote key may be present
local key present
sequence remains at Step 2
```

Recovery retries the remote operation.

The remote operation must be safe to repeat for the same key.

---

## 17.3 Failure During Step 3

The creation of the Fanout Cell and completion of the Idempotency Sequence are committed atomically.

Therefore recovery cannot observe:

```text
Fanout Cell created
but
Idempotency Sequence still incomplete
```

nor:

```text
Idempotency Sequence complete
but
Fanout Cell not created
```

---

# 18. Concurrency

Concurrent observations of the same Fact and idempotency identity must converge safely.

For local idempotency:

```text
Cell A ──┐
         ├── attempt to establish key
Cell B ──┘
```

Only one transaction can successfully establish the previously absent key.

That transaction creates the Fanout Cell.

The other execution observes the established key and terminates.

For Firebase-backed idempotency, concurrent observations may initially perform remote checks.

The local transactional claim ensures that only successfully committed local processing progresses through the Idempotency Sequence.

Subsequent duplicate observations observe the local key and terminate without performing additional remote work.

The resulting invariant is:

> **For a particular `(Component, Key)`, the backend must create at most one Fact Fanout Cell.**

---

# 19. Remote Authority and Local Cache

For Firebase-backed components:

> **Firebase is the durable external authority. The local idempotency store is a durable cache and processing marker.**

When a remote key is discovered:

```text
Remote present
    │
    ▼
cache locally
```

Future duplicate observations may therefore be resolved locally:

```text
Local present
    │
    ▼
terminate
```

This reduces unnecessary remote calls while preserving Firebase as the underlying durable authority.

---

# 20. Relationship to Listeners

Listeners are responsible for recognising Observations.

They do not:

* directly invoke application handlers;
* perform remote idempotency writes;
* decide whether handlers should execute.

Instead, a listener produces a durable Cell Sequence representing:

```text
Potential Fact
+
Idempotency Component
+
Idempotency Key
```

The Idempotency Layer then performs the required processing.

For example:

```text
New Poll Listener
       │
       ▼
Potential NewPoll Fact
       │
       ▼
NewPoll Idempotency Sequence
       │
       ├── duplicate → terminate
       │
       └── new
              │
              ▼
          Fanout Cell
              │
         ┌────┴─────┐
         ▼          ▼
      Push       Email
```

---

# 21. Relationship to Timer Events

Timer events may use the same Fact and Idempotency architecture.

A timer Observation produces:

```text
Potential Fact
+
Idempotency Key
```

For example:

```text
poll:X:auto-complete
```

The Local Idempotency implementation may then determine whether the timer Fact has already been emitted.

```text
Timer Observation
       │
       ▼
Local Idempotency
       │
   ┌───┴────┐
   │        │
Duplicate  New
   │        │
   ▼        ▼
 Stop    Fanout
```

This allows timer observations to be retried or rediscovered safely.

---

# 22. Idempotency Component API

The precise Go API is not defined by this document.

The public API should, however, preserve the architectural separation described above.

Application code should conceptually deal with:

```text
Fact
+
Fact registration
+
Idempotency identity
```

rather than directly managing:

* Firebase collections;
* local idempotency tables;
* Cell Step transitions;
* remote retry semantics.

The concrete API should be designed alongside the Cell, Sequence, Transaction Payload, and Fanout contracts.

---

# 23. Design Invariants

The following invariants are normative.

1. **An Observation must be durably represented by Cellar before downstream processing is relied upon.**

2. **Listeners do not directly perform application handling.**

3. **Idempotency determines whether a Fact may be emitted.**

4. **Idempotency does not directly execute application handlers.**

5. **A newly established Fact is emitted by creating a standard Fanout Cell.**

6. **Plugins register interest in Facts rather than in the internal implementation of idempotency.**

7. **A previously established `(Component, Key)` must not create another Fanout Cell.**

8. **The Cell Sequence's current Step represents the durable processing state machine.**

9. **Transactional effects and Cell Step advancement must be committed atomically according to Cellar's Transaction Payload guarantees.**

10. **For local idempotency, establishing the key and creating the Fanout Cell must be atomic.**

11. **For Firebase-backed idempotency, establishing the local processing claim and advancing to remote population must be atomic.**

12. **Remote idempotency population must be safe to retry for the same idempotency identity.**

13. **For Firebase-backed idempotency, the remote authority must be established before the Fact is emitted.**

14. **Creating the Fanout Cell and completing the Fact Emission Step must be atomic.**

15. **For Firebase-backed idempotency, the remote store is the authoritative external idempotency authority.**

16. **The local idempotency store may cache known remote idempotency records.**

17. **Once a remote key has been observed and cached locally, subsequent duplicate observations should normally be resolved without another remote lookup.**

18. **A process termination at any point must result in recovery through retry of the appropriate Cell Step rather than loss of the Fact's idempotency state.**

19. **For a particular `(Component, Key)`, the backend must create at most one Fact Fanout Cell.**

20. **All idempotency processing must be safe to retry.**

---

# 24. Summary

The complete architecture is based on a simple principle:

```text
Observe
   │
   ▼
Can this Fact be emitted?
   │
   ├── No
   │     │
   │     ▼
   │   Terminate
   │
   └── Yes
         │
         ▼
   Durably create Fanout
         │
         ▼
     Emit Fact
         │
         ▼
   Registered Handlers
```

Local idempotency establishes the key and schedules the Fanout atomically.

Firebase-backed idempotency uses the Cell Sequence to bridge the external authority:

```text
Check
  │
  ▼
Establish local processing state
  │
  ▼
Populate remote authority
  │
  ▼
Create Fanout
```

The Cell Sequence provides the durable processing state machine.

Cellar's transactional guarantees ensure that state changes and Step advancement are committed together.

The result is that:

> **Idempotency owns the decision to emit a Fact. Fact emission owns the creation of the Fanout Cell. Plugins own the handling of the Fact.**

This keeps observation, deduplication, emission, and application processing as separate architectural responsibilities.
