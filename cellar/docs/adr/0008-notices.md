# ADR-0008: Cell Lifecycle Notices

## Status

Draft

## Context

Cellar persists the current state of Cells in the Executor Store.

The Executor Store answers questions such as:

- which Cells currently exist;
- which Handler owns a Cell;
- whether a Cell is READY or CLAIMED;
- when a Cell may next execute.

The Executor Store does not answer questions such as:

- what happened to a Cell yesterday;
- how many times a Cell has been retried;
- whether a Handler panicked before recovery;
- in what order lifecycle transitions occurred.

Operators, tests and debugging tools require visibility into the lifecycle of Cells.

Cellar therefore exposes lifecycle Notices.

A Notice is an observation emitted by the runtime describing a lifecycle transition.

Notices are distinct from logs.

Logs are one possible representation of Notices.

Notices are distinct from persisted state.

The Executor Store remains the authoritative source of current Cell state.

## Decision

Cellar emits lifecycle Notices describing significant lifecycle transitions.

Conceptually:

```go
type Notice struct {
    Time   time.Time
    Kind   NoticeKind
    CellID CellID
}
```

Cellar guarantees that lifecycle transitions generate corresponding Notices.

Notices are part of the Cellar contract.

However, Notices are not authoritative state.

The Executor Store remains the source of truth for Cell execution.

Failure to persist or deliver a Notice must not affect Cell execution semantics.

## Initial notice vocabulary

The initial Notice kinds are:

- CellCreated
- CellClaimed
- HandlerStarted
- HandlerCompleted
- HandlerPanicked
- CellDeleted
- CellRecovered

Additional Notice kinds may be introduced in future versions.

## Ownership

Notices describe the behaviour of Cellar itself.

Handlers do not emit Notices.

Application code remains responsible for its own logging and observability.

Conceptually:

```text
Cellar ---------> Notices

Handler --------> Application logging
```

## Semantics

Notices describe lifecycle transitions.

The Executor Store records the current state of the world.

Notices record observations about how that state changed.

For example:

```text
CellCreated
    ↓
CellClaimed
    ↓
HandlerStarted
    ↓
HandlerCompleted
    ↓
CellDeleted
```

Notices are emitted in lifecycle order for an individual Cell.

No global ordering between unrelated Cells is guaranteed.

## Durability

Notices are observational and are not part of execution correctness.

Cell execution correctness depends only on the Executor Store.

A Cell transition remains valid even if its corresponding Notice is lost.

Therefore:

```text
Executor Store = authoritative state

Notices = lifecycle history
```

Future versions may introduce stronger durability guarantees.

## Uses

Notices may be consumed by:

- structured logging;
- debugging tools;
- metrics systems;
- test harnesses;
- replay and fault-reproduction systems.

This ADR does not define those systems.

## Non-goals

This ADR does not define:

- a logging format;
- a journal format;
- event sourcing;
- replay semantics;
- audit trails;
- metrics;
- distributed tracing.

## Consequences

### Positive

- Lifecycle transitions become observable.
- Tests can assert on runtime behaviour.
- Debugging tools gain a standard source of lifecycle information.
- Structured logging becomes straightforward.
- Future replay systems may consume the same Notice stream.

### Negative

- Cellar implementations must emit Notices consistently.
- Notice history is not guaranteed to be durable.
- Historical information may be incomplete after failures.

## Cross references

- ADR-0002: Cell Lifecycle and Execution Model
- ADR-0005: Payload Encoding and Type Safety
- ADR-0006: Cell Inspection and Debugging Model
- ADR-0007: Offline Cell Administration
