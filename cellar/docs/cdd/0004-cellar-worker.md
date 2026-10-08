# CDD: Cellar Worker

## Purpose

The Worker is the Cellar component responsible for executing claimed Cells.

A Worker forms the boundary between:

* Cellar runtime infrastructure;
* application-provided Handlers.

The Worker receives execution responsibility for a Cell and invokes the appropriate Handler.

---

## Responsibilities

The Worker is responsible for:

* receiving CLAIMED Cells from the Scheduler queue;
* locating the registered Handler;
* decoding the Cell payload using the registered Codec;
* invoking the Handler;
* providing the execution context;
* returning the Handler Result to Cellar.

The Worker is not responsible for:

* scheduling Cells;
* claiming Cells;
* deciding retry policy;
* creating child Cells;
* directly modifying Cell lifecycle state;
* understanding application semantics.

---

## Relationship to Other Components

The execution path is:

```text
Store
 |
 | READY -> CLAIMED
 |
 v

Scheduler

 |
 | claimed Cell
 |
 v

Worker

 |
 | decode payload
 | invoke Handler
 |
 v

Handler

 |
 | Result
 |
 v

Cellar Runtime

 |
 | apply Result
 |
 v

Store
```

The Worker is an execution mechanism, not a lifecycle coordinator.

---

## Worker Lifecycle

Conceptually:

```go
func worker(ctx context.Context, cell Cell) {

    handler := registry.Lookup(cell.HandlerName)

    payload := codec.Decode(cell.Payload)

    result := handler.Handle(
        ctx,
        payload,
    )

    runtime.ApplyResult(
        cell,
        result,
    )
}
```

The actual implementation may separate these responsibilities across internal components.

---

## Handler Invocation

Handlers receive:

```go
context.Context
```

and their strongly typed payload.

Conceptually:

```go
type Handler[T any] interface {
    Handle(
        ctx context.Context,
        payload T,
    ) Result
}
```

The Worker is responsible for converting persisted data:

```text
[]byte
```

into:

```text
T
```

using the registered Codec.

---

## Payload Decoding

The Worker uses the Handler registration metadata:

```text
HandlerName
        |
        v
Handler Registration
        |
        +--> Codec
        |
        +--> Handler
```

If decoding fails, the Worker cannot execute the Cell.

The handling of permanently invalid Cells is defined separately by the Cell failure model.

---

## Result Handling

The Handler returns a Result.

The Worker does not interpret the business meaning of the Result.

Instead:

```text
Handler
    |
    v
Result
    |
    v
Cellar Runtime
    |
    v
Store changes
```

Examples:

```text
Success
    -> delete Cell

RetryIn(duration)
    -> return Cell to READY

Create child Cells
    -> atomically create Cells and complete parent
```

The Worker only transports this information.

---

## Context Cancellation

Cellar provides a context to the Worker.

The Worker passes this context to the Handler.

Cancellation may occur due to:

* Cellar shutdown;
* operational control;
* future runtime policies.

Cancellation is advisory.

Cellar does not forcibly terminate Handler execution.

Handlers are responsible for deciding how to respond.

---

## Failure Model

The Worker operates according to ADR-0010.

In particular:

* Cells may execute more than once.
* Handlers must tolerate duplicate execution.
* External side effects remain the responsibility of application code.

---
## Panic Handling

Panics are treated as programming defects.

V0 Cellar does not recover Handler panics.

A Handler panic terminates the Cellar process.

This decision is intentional:

* it keeps the runtime model simple;
* it makes application bugs visible;
* it encourages Handlers to use explicit error handling;
* it avoids silently converting defects into retry loops.

Cellar recovery semantics still apply after restart.

If a panic occurs while executing a Cell:

1. The process terminates.
2. On restart, the Cell Store recovery process transitions:

```text
CLAIMED -> READY
```

3. The Cell may execute again.

Handlers must therefore still tolerate duplicate execution.

Future versions may introduce isolated Handler panic handling if operational experience demonstrates a need.


---

## Concurrency

Workers are independent execution loops.

Multiple Workers may execute different Cells concurrently.

The Worker does not provide ordering guarantees between Cells.

Per-Cell Notice ordering is handled by the Notice subsystem.

---

## Shutdown Behaviour

When Cellar shuts down:

* Workers stop accepting new Cells.
* Currently executing Handlers receive cancellation through context.
* Completion behaviour is determined by Handler cooperation.

Cellar does not enforce execution deadlines.

---

## Non-goals

The Worker does not provide:

* exactly-once execution;
* external transaction coordination;
* workflow orchestration;
* business retry logic;
* handler isolation beyond the defined runtime boundary.

---

## Future Considerations

Future versions may introduce:

* Handler timeout policies;
* execution metrics;
* tracing;
* worker pools;
* priority execution;
* improved panic isolation.

These should not alter the core responsibility:

> Execute claimed Cells and report the result to Cellar.
