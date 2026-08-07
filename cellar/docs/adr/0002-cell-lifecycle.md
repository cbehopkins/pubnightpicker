# ADR-0002: Cell Lifecycle and Execution Model

## Status

Draft

## Context

Cells are the only execution primitive understood by the runtime.

The runtime is initially designed for:

- a single backend process
- process restart recovery
- low throughput
- low operational complexity

Distributed execution is explicitly out of scope.

The runtime is responsible for persistence, scheduling and recovery.

Handlers are responsible only for business logic.

## Cell Structure

Conceptually, a Cell contains:

```go
type Cell struct {
    ID         CellID
    Kind       CellKind
    Payload    []byte

    State      CellState

    NotBefore  *time.Time
}
```

## Cell States

A Cell exists in exactly one state.

```text
READY
    ↓
CLAIMED
    ↓
DELETED
```

Primary execution path:

```text
READY -> CLAIMED -> DELETED
```

Recovery path on runtime startup:

```text
CLAIMED -> READY
```

`RetryNow` and `RetryIn(duration)` are handler results that return a claimed Cell to READY instead of deleting it.

### READY

The Cell is available for scheduling.

A Cell is runnable if:

```text
State == READY
AND
(NotBefore == NULL OR NotBefore <= now)
```

### CLAIMED

The scheduler has accepted responsibility for executing the Cell.

A claimed Cell must not be scheduled again.

Cells in the claimed state are expected to be transient.

### DELETED

Deletion is terminal.

Deleted Cells no longer exist in the Executor Store.

## Scheduler Semantics

The scheduler repeatedly performs the following transaction:

```sql
BEGIN;

SELECT id
FROM cells
WHERE state = 'READY'
  AND (
      not_before IS NULL
      OR not_before <= ?
  )
LIMIT 1;

UPDATE cells
SET state = 'CLAIMED'
WHERE id = ?;

COMMIT;
```

The scheduler then dispatches the Cell to a worker.

Only the scheduler may transition a Cell from READY to CLAIMED.

By architectural constraint, exactly one scheduler instance runs at a time.

Competing schedulers are out of scope.

## Worker Semantics

Workers receive claimed Cells from the scheduler.

Workers execute the Cell's Handler:

```go
result := handler.Handle(ctx, cell)
```

Handlers do not directly modify executor state.

Handlers return instructions to the runtime.

The runtime is responsible for applying those instructions.

## Handler Contract

Conceptually:

```go
type Handler interface {
    Handle(ctx context.Context, cell Cell) Result
}
```

Handlers are expected to behave like ordinary Go functions.

Properties:

- conceptually stateless
- may create Cells
- return instructions to the runtime
- may fail
- may panic

Panics are bugs.

## Handler Results

Conceptually:

```go
type Result interface{}
```

The current runtime supports:

### Success{}

Delete the Cell.

### RetryNow{}

Transition the Cell to:

```text
State = READY
NotBefore = NULL
```

### RetryIn(duration)

Transition the Cell to:

```text
State = READY
NotBefore = now + duration
```

### Abandon{}

Delete the Cell.

Future results may be added.

## Recovery Semantics

Process termination is treated as if execution never completed.

On startup, the runtime executes:

```sql
UPDATE cells
SET state = 'READY'
WHERE state = 'CLAIMED';
```

Recovery does not distinguish between:

- panic
- SIGTERM
- power loss
- kernel crash
- process restart

Handlers must therefore tolerate duplicate execution.

## Retry Semantics

Retries are explicit.

The runtime provides scheduling primitives.

Business logic determines retry policy.

Examples:

- retry immediately
- retry in one hour
- abandon permanently

The runtime itself contains no retry policy.

## Scheduling Semantics

The scheduler understands only:

- Cell state
- execution time

The scheduler does not understand:

- workflows
- services
- business concepts

## Non-goals

The runtime does not guarantee:

- exactly-once execution
- at-least-once execution
- workflow recovery
- distributed execution
- lease semantics
- handler timeouts

Cross-cutting architectural philosophy is defined in ADR-0000.
