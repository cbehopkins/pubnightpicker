# ADR: Truths

## 1. Status

Accepted

---

# 2. Context

The backend performs work in response to things that have happened, or things that have become due.

These observations originate from a variety of sources, including:

* Firebase document listeners;
* Cellar timer events;
* other backend processes;
* future external event sources.

The source of an observation is an implementation detail. The application needs a common representation of the meaningful fact that was observed so that the remainder of the system does not need to know how that fact was discovered.

We call these meaningful facts **Truths**.

For example:

* a Poll has opened;
* a Poll has completed;
* a Poll has been rescheduled;
* a chat message has been created;
* a notification ping has occurred;
* weekly housekeeping is due;
* a Poll should now be considered for automatic creation.

The system uses Cellar for durable execution, Fanout, retry and Cell Sequences. Consequently, a Truth must be suitable for persistence and later re-execution.

This creates an important requirement for Truths originating from Firebase.

A Firebase `DocumentSnapshot` is a useful representation of the document state observed by a listener, but it is an SDK object rather than a durable application representation. It should therefore not become part of the persisted Truth contract.

The Truth must instead capture the relevant observed state in an application-owned Go representation.

---

# 3. Decision

The backend will represent meaningful observations as **typed Truths**.

A Truth is a durable, typed statement that:

1. describes what happened or what has become due;
2. contains the relevant evidence/state associated with that occurrence;
3. has a stable identity which defines when two observations represent the same Truth occurrence.

The Truth is the boundary between the mechanism which discovers an observation and the application work which responds to it.

The general architecture is:

```text
Source
   │
   │ Firebase listener / timer / other mechanism
   ▼
Truth construction
   │
   ├── Truth type
   ├── Evidence
   └── Truth identity
   │
   ▼
Idempotency
   │
   ├── already established ─────► finish
   │
   └── newly established
              │
              ▼
           Fanout
              │
              ▼
       Application Cells
```
The source is responsible for recognising an observation and constructing the appropriate Truth.

The remainder of the application deals with the Truth rather than the source which produced it.

# 4. Truths Are Types

Truths will be represented as Go types rather than as values from a global enumeration.

For example:

// PollOpened states that a Poll has opened.
type PollOpened struct {
    Poll PollSnapshot
}

and:

// PollCompleted states that a Poll has completed.
type PollCompleted struct {
    Poll PollSnapshot
}

The type itself provides the primary identity and documentation of the Truth.

This is preferred over a generic structure such as:

type Truth struct {
    Type string
    Data any
}

because the latter moves important application semantics into runtime data and weakens Go's type system.

The set of Truths is therefore represented by the set of supported Truth types and their documentation rather than by a central enumeration.

# 5. Truth Meaning Is Independent of Its Source

A Truth describes an application-level fact, not the mechanism by which that fact was discovered.

For example:

PollOpened

means:

A Poll has opened.

It does not mean:

A Firebase document-create listener fired.

Similarly:

WeeklyHousekeepingDue

means:

Weekly housekeeping is now due.

It does not mean:

A Cellar timer tick occurred.

The source is therefore an implementation concern of Truth construction.

This allows the same Truth to potentially be produced by different mechanisms in the future.

# 6. Due Rather Than Tick

Time-based Truths describe work becoming due rather than exposing the mechanics of the timer which caused them.

Therefore Truths should be named according to their application meaning.

For example:

WeeklyHousekeepingDue
WeeklyPollCreateDue
DailyPollAutoCompleteDue

rather than:

WeeklyHousekeepingTick
WeeklyPollCreateTick
DailyPollAutoCompleteTick

A timer is merely one mechanism for discovering that the work is due.

# 7. Truth Evidence

A Truth must contain the relevant state that was observed when the Truth was constructed.

For Firebase-originated Truths, the listener initially receives a Firebase DocumentSnapshot.

That snapshot is treated as transient input to Truth construction.

The listener converts the relevant fields from the Firebase snapshot into an application-owned Go structure.

Conceptually:

Firebase DocumentSnapshot
        │
        ▼
Application Snapshot
        │
        ▼
Typed Truth

For example:

type PollSnapshot struct {
    ID            string
    CreatedAt     time.Time
    SelectedVenue string

    // Other fields required by application processing.
}

A Truth can then contain:

type PollOpened struct {
    Poll PollSnapshot
}

The application-owned snapshot represents the state observed at the time the Truth was created.

# 8. Why Truths Carry Evidence

The evidence is deliberately captured at Truth creation time rather than requiring downstream handlers to re-read the source document.

This provides an important temporal guarantee.

For example:

10:00
Firebase listener observes Poll A as open
        │
        ▼
PollOpened{
    Poll: snapshot of Poll A at 10:00
}

If the Poll subsequently changes:

10:01
Poll A changes

the existing Truth does not change.

If the Cell is delayed or retried:

10:10
PollOpened handler executes

it still processes the same observed evidence.

A retry therefore means:

Retry processing this Truth.

It does not mean:

Re-read the source and process whatever happens to be true now.

This avoids a particularly undesirable failure mode where a failed Cell is retried against a newer version of the document than the version which originally caused the Truth.

# 9. Truth Evidence Is Conceptually Immutable

The evidence contained in a Truth represents the state observed when the Truth was constructed.

It should therefore be treated as immutable.

Handlers may perform work based on the evidence and may eventually schedule mutations to the originating document, but such mutations should occur only after the document has been fully processed.

The preferred conceptual pattern is:

Observed snapshot
       │
       ▼
Construct Truth
       │
       ▼
Digest/process Truth
       │
       ▼
Complete all required work
       │
       ▼
Schedule originating-document mutation

The snapshot is therefore the evidence for the work, rather than a mutable object which handlers progressively modify.

# 10. Application-Owned Snapshot Representation

A Firebase DocumentSnapshot is not the durable representation of Truth evidence.

Although it represents an already-observed document state and is not a lazy reference which subsequently fetches the document, it is an SDK object with implementation-specific internal state and behaviour.

It is therefore unsuitable as the persisted payload of a Cellar execution.

Instead, Truth construction converts the Firebase snapshot into an application-owned Go structure containing the fields relevant to processing the Truth.

For example:

type PollSnapshot struct {
    ID            string
    CreatedAt     time.Time
    SelectedVenue string
    // ...
}

This representation is serialisable and can therefore survive:

Cell persistence;
process termination;
process restart;
delayed execution;
retries.

The eventual application model may become more sophisticated as the system develops. The current architecture deliberately keeps the conversion boundary clear so that such a model can be introduced later without changing the fundamental Truth contract.

# 11. Evidence Versus Current State

Truth evidence and current database state are deliberately different concepts.

The evidence answers:

What state was observed when this Truth was created?

A database lookup answers:

What state is true now?

A handler may explicitly require current state in addition to Truth evidence, in which case it may perform a lookup deliberately.

However, a handler must not perform an implicit lookup merely because the Truth failed to preserve the state required to understand the original observation.

The default expectation is that a Truth is sufficiently self-contained to understand and process the occurrence it represents.

# 12. Truth Identity

Each Truth defines what constitutes the same occurrence of that Truth.

Truth identity is distinct from Truth evidence.

Evidence describes:

What was observed.

Identity describes:

What makes this particular Truth occurrence unique.

The identity is therefore not necessarily derived from the complete snapshot.

For example, PollOpened may have the identity:

PollOpened / <poll document ID>

because there can only be one meaningful PollOpened occurrence for a particular Poll.

A PollCompleted Truth may have a more complex identity, potentially incorporating values such as:

PollCompleted /
    <poll ID> /
    <venue ID> /
    <occurrence time> /
    <pub or venue>

The exact identity is part of the semantics of the individual Truth.

It is not the responsibility of generic idempotency infrastructure to understand the domain meaning of the key.

13. Relationship to Idempotency

Idempotency enforces Truth identity; it does not define it.

The conceptual pipeline is:

Truth
 ├── Type
 ├── Evidence
 └── Identity
          │
          ▼
     Idempotency
          │
          ▼
       Fanout

A Truth therefore provides the semantic identity required by the idempotency mechanism.

The idempotency layer determines whether that particular Truth occurrence has already been established for dispatch.

For example:

PollOpened
    Identity = abc123

may become an idempotency namespace such as:

PollOpened / abc123

while:

PollCompleted
    Identity = abc123 / xyz789 / 2026-09-04T19:00

becomes a different logical idempotency identity.

The physical representation of this identity in the idempotency database is an implementation detail.

The existing Idempotency ADR defines how that identity is established and how Fanout creation is made atomic with the establishment of idempotency.

# 14. Truths and Persistence

Truths are designed on the assumption that their associated Cell execution may be persisted and resumed.

A persisted Truth must retain the same semantic meaning after:

process termination
process restart
delay
retry

Consequently, a Truth must not depend on transient references to external state for its fundamental meaning.

In particular, a Truth must not rely solely upon:

DocumentRef

and subsequently reload the document to reconstruct its evidence.

Doing so would allow a retry to process a different document state from the one which originally generated the Truth.

Instead:

Truth
  │
  ├── Identity
  └── Evidence
          │
          ▼
       persisted

must remain sufficient to understand the original occurrence.

# 15. Current Truth Catalogue

The currently identified Truths are:

## Time-derived Truths
### WeeklyHousekeepingDue 

Weekly housekeeping work has become due.

Produced by a Cellar timer.

### WeeklyPollCreateDue

The weekly Poll creation process has become due.

Produced by a Cellar timer.

### DailyPollAutoCompleteDue

Automatic Poll completion processing has become due.

Produced by a Cellar timer.

## Poll Truths
### PollOpened

A Poll has opened.

Initially discovered through a Firebase document creation listener.

The Truth contains the relevant observed Poll state.

### PollCompleted

A Poll has completed because a venue has been selected.

This may be discovered through creation or modification of the relevant Firebase document.

The Truth contains the relevant observed Poll state.

### PollRescheduled

A Poll has been rescheduled because its location/venue has been modified.

The Truth contains the relevant observed Poll state.

## Communication Truths
### ChatMessageCreated

A new chat message has been created.

Initially discovered through a Firebase document creation listener.

### NotificationPing

A notification ping has occurred.

Initially discovered through a Firebase listener.

## Other Domain Truths
### RecurrenceCalculation

Recurrence calculation work is required.

This represents the existing recurrence-processing mechanism and may have more specialised semantics than the simpler observation Truths.

# 16. Relationship to Firebase Listeners

Firebase listeners are responsible for:

receiving Firebase observations;
determining whether an application-level Truth has occurred;
converting the observed Firebase state into the appropriate application-owned snapshot;
constructing the typed Truth;
creating the initial durable Cell through the appropriate idempotency mechanism.

The listener is not responsible for executing the application work represented by the Truth.

Conceptually:

Firebase listener
       │
       ▼
DocumentSnapshot
       │
       ▼
Truth construction
       │
       ├── typed Truth
       ├── evidence
       └── identity
       │
       ▼
Idempotency Cell
       │
       ▼
Fanout Cell

Firebase listener delivery is therefore an input mechanism, not part of the Truth contract.

# 17. Relationship to Cellar

Truths are designed to be suitable as the payload for Cellar execution.

A Truth may therefore be:

created
   ↓
persisted
   ↓
queued
   ↓
executed
   ↓
retried

without losing the evidence associated with the original observation.

Cellar's Fanout and Cell Sequence mechanisms then provide the execution structure required to process the Truth.

The Truth itself does not describe which handlers should execute.

That connectivity is the responsibility of Plugins and their Fanout/Sequence configuration.

# 18. Separation of Concerns

The architecture therefore assigns responsibilities as follows:

Source
    "Something happened / became due."

Truth
    "This is what happened, and this is what it refers to."

Truth Identity
    "This is how this occurrence is uniquely identified."

Idempotency
    "This occurrence has / has not already been dispatched."

Fanout
    "These handlers need to process this Truth."

Cell Sequence
    "These handlers form the unit of work and execute with
     Cellar's retry semantics."

Service
    "This is the application operation being performed."

Database
    "This is how application state is persisted or observed."

Plugin
    "This is how Truths are connected to application work."

Each layer therefore has a deliberately narrow responsibility.

# 19. Design Invariants

The following invariants are normative:

Truths are represented as concrete Go types.
A Truth describes application meaning rather than the mechanism by which it was discovered.
Time-based Truths describe work becoming due rather than exposing timer ticks.
A Truth contains the relevant evidence associated with the occurrence it represents.
Firebase DocumentSnapshot objects are transient inputs to Truth construction and are not the durable Truth representation.
Firebase-originated Truths convert the relevant observed document state into an application-owned Go representation before the Truth becomes durable.
Truth evidence represents the observed state at Truth creation time and is treated as immutable.
A retry processes the same Truth evidence rather than implicitly re-reading the source document to obtain current state.
Truth identity is distinct from Truth evidence.
Each Truth defines its own identity semantics.
Generic idempotency infrastructure enforces Truth identity but does not define its domain semantics.
A Truth must remain semantically meaningful across Cell persistence, process restart, delay and retry.
A document reference alone must not be used as a substitute for Truth evidence when doing so would require re-reading the source document to reconstruct the original observation.
The Truth contract does not prescribe which application handlers process the Truth.
Fanout and Cell Sequences determine how a Truth is converted into application work.

# 20. Consequences
## Positive
Durable semantics

A Truth remains the same Truth when execution is delayed or retried.

Temporal correctness

Handlers can distinguish the state which caused the Truth from the current state of the database.

Reduced unnecessary reads

Handlers do not need to re-read the source document merely to recover the state already observed by the listener.

Strong type safety

Truths are represented using concrete Go types rather than generic runtime event structures.

Clear architectural boundaries

Firebase-specific observation and SDK types remain on the input side of the Truth construction boundary.

Natural integration with Cellar

Truths can form durable Cell payloads suitable for retry and process restart.

Explicit identity

The domain decides what constitutes the same occurrence, while idempotency remains generic.

## Negative
Additional data must be persisted

Truths may contain substantially more information than a simple document reference.

This increases storage and potentially memory/copying costs.

This cost is accepted in favour of preserving durable execution semantics.

Snapshot structures require maintenance

Application-owned snapshot structures must be updated when downstream processing requires additional document fields.

Initial duplication of models

The snapshot representation may overlap with the eventual application/domain model of the underlying Firebase document.

This duplication is accepted initially.

If a more comprehensive application-owned model becomes appropriate later, Truth snapshot structures can be refactored to use it.

Larger Fanout payloads

A Truth containing a snapshot may be copied or serialised for multiple Cells.

This is accepted initially. Performance optimisation should be driven by evidence rather than prematurely complicating the model.

# 21. Future Evolution

The initial implementation may use relatively direct snapshot structures derived from Firebase documents.

As the application grows, it may become appropriate to introduce richer application-owned models for entities such as Polls and Venues.

The architectural boundary remains:

Firebase representation
        ↓
Application representation
        ↓
Truth

Introducing richer application models later should therefore not require changing the fundamental Truth architecture.

Likewise, the exact representation of Truth identity and its integration with the idempotency components may evolve independently of Truth evidence.

# 22. Summary

The backend treats a Truth as:

A durable, typed statement of something that happened or became due, together with the application-owned evidence and stable identity required to process that occurrence correctly.

The essential model is:

                 Truth
        ┌──────────┼──────────┐
        │          │          │
      Type      Evidence   Identity
        │          │          │
        │          │          ▼
        │          │      Idempotency
        │          │
        │          ▼
        │      Observed state
        │
        ▼
    Application
     meaning

For Firebase-originated observations:

Firebase DocumentSnapshot
          │
          ▼
Application-owned Snapshot
          │
          ▼
      Typed Truth
          │
          ▼
      Idempotency
          │
          ▼
        Fanout
          │
          ▼
   Cell Sequences / Services

This ensures that the system processes the Truth that was observed, rather than repeatedly asking external systems what happens to be true now.
