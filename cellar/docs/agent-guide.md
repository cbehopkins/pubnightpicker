# Cellar agent guide

This document is intended for agents and other automation that need to use Cellar as a durable execution runtime. The goal is to explain the public API in practical terms so an agent can create work, run handlers, and safely participate in application state changes.

## What Cellar is for

Use Cellar when you need to turn application events into durable background work that can survive restarts, retries, and replays.

Typical use cases include:

- creating follow-up work after a domain event is observed
- scheduling retries for work that should not be retried immediately
- keeping side effects outside the main request path while still making them durable
- expressing workflows as a graph of cells rather than ad hoc background jobs

Cellar is intentionally simple. It does not try to solve distributed coordination, competing consumers, or exactly-once delivery. Its main job is to persist work safely and execute it in a predictable lifecycle.

## Core concepts

### Cell
A cell is the fundamental unit of execution. A cell has:

- an identifier
- one or more ordered handler steps, each with its own payload
- a durable current-step cursor
- a lifecycle state
- an optional not-before time

A one-step Cell is the normal case. A multi-step Cell is an ordered sequence: steps
run one at a time, in order, and keep the same Cell ID. Steps do not pass return
values directly to one another; use durable application state when they need to
communicate.

### Handler
A handler is the business logic that executes for a cell. The public handler model is:

```go
type Handler[T any] interface {
    Handle(ctx context.Context, payload T) Result
}
```

Handlers should not directly manage persistence. Instead, they return a result that instructs the runtime what to do next.

### Result
A handler returns one of the runtime-supported results:

- `cellar.Complete{...}` to replace the current cell with zero or more child cells
- `cellar.Retry{...}` to return the current cell to `READY` for later execution
- `cellar.RetrySequence{...}` to restart the Cell from its first step after a delay
- `cellar.Kill{}` to terminate the Cell without executing later steps
- `cellar.ErrorResult{...}` to surface a handler or runtime failure

## Initialising a Cellar instance

The runtime is composed from a few small building blocks:

1. A store that persists cells and their lifecycle state.
2. A registry that binds handler names to executable registrations.
3. A worker that executes claimed cells.
4. A scheduler that repeatedly claims runnable cells and dispatches them.

A simple setup with SQLite looks like this:

```go
import (
    "context"
    "database/sql"
    "log"
    "time"

    publicsqlite "cellar/pkg/sqlite"
    "cellar/pkg/cellar"
)

type runtimeDispatcher struct {
    worker *cellar.Worker
}

func (d runtimeDispatcher) Dispatch(ctx context.Context, cell cellar.Cell) error {
    _ = d.worker.Run(ctx, cell)
    return nil
}

func main() {
    db, err := sql.Open("sqlite", "./cells.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    store, err := publicsqlite.NewStore(db, nil)
    if err != nil {
        log.Fatal(err)
    }

    registry := cellar.NewMemoryRegistry()
    // register handlers here

    registry.Freeze()

    worker := cellar.NewWorker(registry, cellar.NewStoreResultApplier(store))
    dispatcher := runtimeDispatcher{worker: worker}
    scheduler := cellar.NewScheduler(store, dispatcher, 1, 5*time.Second)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go scheduler.Run(ctx)
}
```

The worker and scheduler constructors are now part of the public package, so sibling modules can create and start the runtime without reaching into internal packages. In practice you will usually use the public SQLite package because it provides the persistence implementation and the underlying `database/sql` instance without requiring downstream code to import internal packages.

## Accessing the underlying database

For SQLite-backed deployments, the store exposes the underlying database connection directly:

```go
store, err := sqlite.Open("./cells.db", nil)
if err != nil {
    log.Fatal(err)
}

defer store.Close()

conn := store.DB()
if conn == nil {
    log.Fatal("sqlite store connection missing")
}
```

This is useful when an agent needs to:

- inspect or update application-specific tables
- participate in the same transaction as a cell completion
- read or write auxiliary state that is not part of Cellar’s core cell model

The important constraint is that application work must use the same transaction boundary as the cell completion. Cellar provides a transaction-aware callback for that purpose.

## Why create cells

Create cells when an application event should become durable work. The agent should create cells when:

- an inbound request needs follow-up action later
- a domain event should trigger asynchronous work
- a handler needs to fan out into multiple independent follow-up tasks
- a task should be retried later rather than failing immediately

A cell is a durable request for execution. It is not the business event itself; it is the execution primitive that the runtime will schedule.

## How to create cells

Use `Cellar.Add` for ordinary one-step work:

```go
id, err := runtime.Add("send-email", SendEmail{Recipient: "person@example.com"})
```

Use `Cellar.AddSequence` when handlers must run in order:

```go
id, err := runtime.AddSequence(
    cellar.Step{HandlerName: "order.validate", Payload: ValidateOrder{ID: orderID}},
    cellar.Step{HandlerName: "order.reserve", Payload: ReserveOrder{ID: orderID}},
    cellar.Step{HandlerName: "order.notify", Payload: NotifyOrder{ID: orderID}},
)
```

Use the store’s `Add` method only when supplying already JSON-encoded steps:

```go
ids, err := store.Add([]cellar.CellRequest{{
    Steps: []cellar.CellStep{{
        HandlerName: "send-email",
        Payload:     []byte(`{"recipient":"person@example.com"}`),
    }},
}})
```

Each `CellRequest` contains:

- `Steps`: one or more ordered handler invocations with opaque JSON bytes
- `NotBefore`: optional scheduling time

If the handler name is empty, the store will reject the request.

## How handlers participate in transactions

When a handler completes successfully and wants to make application state changes atomically with the cell transition, return `Complete` with `ApplicationWork` callbacks.

Example:

```go
result := cellar.Complete{
    NewCells: []cellar.CellRequest{{
        HandlerName: "send-push",
    }},
    ApplicationWork: []cellar.ApplicationWork{
        func(tx cellar.ApplicationTx) error {
            return tx.Exec(`INSERT INTO audit_log (id, message) VALUES (?, ?)`, "1", "completed")
        },
    },
}
```

These callbacks run in the same transaction as the cell completion over the shared Base DB. That means:

- the application work and the cell replacement happen together
- if the application work fails, the completion is rolled back
- the parent cell is not deleted unless the whole operation succeeds
- the application can use its own schema and tables in the same database without needing to modify Cellar-owned tables

This is the main mechanism for making Cellar and application state changes atomic.

## Error handling philosophy

Cellar treats errors as explicit runtime signals rather than implicit retries.

A handler should generally return:

- `Complete` when work has succeeded and should transition to child cells
- `Retry` when the work should be attempted again later
- `ErrorResult` when the failure is terminal or should be surfaced as an error

The runtime should not silently swallow failures. If a handler or result applier fails, the worker turns that into an `ErrorResult` rather than pretending the work completed.

Recommended policy:

- use `Retry` for transient failures such as temporary dependency problems
- use `ErrorResult` for unrecoverable or misconfiguration situations
- keep business logic decision-making explicit in the handler rather than hidden in infrastructure

## Retry and recovery

A handler can request a retry with an optional not-before time:

```go
return cellar.Retry{NotBefore: &when}
```

When a cell is retried, the runtime moves it back to `READY` and it becomes eligible for scheduling again.
`Retry` preserves the current step. `RetrySequence` resets the cursor to zero and
applies its delay. A persisted `NotBefore` value in the past is already eligible;
it is not cleared merely because the Cell has become runnable.

On process restart, claimed cells are automatically recovered to `READY` by the store’s recovery path. This provides a practical recovery model for crashes and restarts without requiring the handler to do anything special.

```go
if err := store.Recover(); err != nil {
    log.Fatal(err)
}
```

## Suggested integration pattern for an agent

A robust agent workflow is:

1. initialise a SQLite-backed store
2. register handlers with a memory registry
3. create cells for the work it wants to perform
4. let the scheduler/worker execute them
5. use `ApplicationWork` for any transactional side effects that must commit with the cell transition
6. use `Retry` for transient errors and `ErrorResult` for terminal ones

## Notes for authors and agents

- treat payloads as opaque bytes unless a decoder is explicitly provided
- keep handlers deterministic and idempotent where possible
- prefer `Complete` for normal success and `Retry` for transient failure
- use the underlying database access only when application-level state needs to be coordinated with the cell transaction
