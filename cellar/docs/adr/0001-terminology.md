# ADR-0001: Terminology and Architectural Invariants

## Status

Draft

## Context

The backend reacts to changes in the business domain and performs asynchronous work such as sending notifications, housekeeping and scheduled maintenance.

The system is initially designed for:

- low traffic
- low operational cost
- single-process execution
- process restart recovery
- eventual migration from Python to Go

Firestore operations are considered expensive and should be minimised.

The system must be designed such that execution infrastructure can evolve independently from business logic.

## Terminology

### Domain Event

A Domain Event is an immutable statement that some fact in the business domain has become true.

Examples:

- PollOpened
- PollCompleted
- ChatMessageSent
- DiagnosticPingRequested

Properties:

- immutable
- stateless
- uniquely identifiable
- may be observed multiple times
- may trigger zero or more Cells

The runtime must assume that duplicate observations are normal.

DomainEventID uniqueness is a required property.

The mechanism that guarantees uniqueness is intentionally left as an implementation detail.

### Business Store

The Business Store is the canonical source of business truth.

The current implementation uses Firestore.

Examples:

- polls
- users
- chat messages
- venue information
- ledgers
- diagnostic requests

The Business Store does not contain execution state.

### Executor Store

The Executor Store contains state required to execute backend work.

The initial implementation is expected to use SQLite.

Examples:

- Cells
- cached business data
- executor metadata

The Executor Store is not a source of business truth.

Executor state is allowed to disappear during deployments.

### Listener

A Listener observes exactly one collection within the Business Store.

A Listener reacts to Domain Events and creates Cells.

Examples:

- Poll Open Listener
- Poll Complete Listener
- Chat Message Listener

Properties:

- a Listener observes exactly one collection
- a Listener accepts responsibility for a Domain Event
- a Listener creates Cells
- a Listener owns a Ledger

There may exist multiple Listeners for a Domain Event.

Architecturally, there should normally be exactly one Listener per Domain Event.

### Ledger

A Ledger records that a Listener has accepted responsibility for a Domain Event.

A Ledger exists to provide business-level idempotency.

Properties:

- stored in the Business Store
- written only by backend systems
- durable
- driven by business requirements
- optional

Conceptually:

```go
LedgerEntry {
    DomainEventID
    ListenerID
    ACCEPTED
}
```

The Ledger tracks acceptance, not execution.

### Cell

A Cell is the fundamental execution primitive of the system.

Cells represent atomic units of work.

Examples:

- send a mailing-list email
- send a push notification
- clean expired diagnostics
- schedule tomorrow's reminder

Properties:

- stored in the Executor Store
- atomic
- transient
- highly fallible
- may create other Cells
- do not own other Cells
- do not wait for other Cells

Cells are the only execution primitive visible to the runtime.

### Handler

A Handler is the code associated with a Cell.

Conceptually, a Handler behaves like a stateless function:

```go
func Handle(ctx context.Context, cell Cell) error
```

Properties:

- conceptually stateless
- receives a Cell
- may create Cells
- returns success or failure
- does not directly mutate arbitrary executor state

### Service

A Service is a logical grouping of backend functionality.

Examples:

- Poll Open Mailing List Email Service
- Poll Open Push Notification Service
- Poll Complete Email Service

Services are documentation and deployment concepts.

Services are not runtime primitives.

### Workflow

A Workflow describes how multiple Services cooperate to satisfy a business requirement.

Examples:

- Poll Open Notification Workflow
- Poll Completion Workflow
- Chat Notification Workflow

Workflows are documentation concepts.

Workflows are not runtime primitives.

### Frontend

A Frontend is an untrusted client application.

Examples:

- Android application
- web client

Frontends may write business state.

Frontends must never write executor state.

### Backend

A Backend is a trusted server process.

Examples:

- legacy Python backend
- Go backend

Multiple backend implementations may coexist.

Only one backend may own a Listener.

## Architectural Invariants

- Domain Events are immutable and stateless.
- The Business Store contains business state.
- The Executor Store contains execution state.
- Cells are the only runtime execution primitive.
- Cells may create Cells but do not own Cells.
- Listeners consume Domain Events and create Cells.
- Listeners own Ledgers.
- Ledgers exist for business reasons, not infrastructure reasons.
- Frontends never write executor state.
- Listeners should rarely write to collections they observe.
- Multiple backends may coexist.
- Only one backend may own a Listener.
- Loss of the Executor Store is an acceptable failure mode.
