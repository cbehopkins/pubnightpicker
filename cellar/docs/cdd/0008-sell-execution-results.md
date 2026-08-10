# CDD: Cell Execution Result

## Purpose

A Handler returns a Result to Cellar after executing a Cell.

The Result describes the outcome of the execution in terms understood by Cellar.

The Result is not an application event and does not contain application-specific business semantics.

The Worker transports the Result to the Cellar Runtime.

The Runtime interprets the Result and applies the corresponding Store operation.

---

## Execution Flow

```text
Handler

    |
    | Result
    v

Worker

    |
    | Result
    v

Runtime

    |
    | Store operation
    v

Store
```

The Handler does not directly interact with the Store.

---

## Result Interface

Conceptually:

```go
type Result interface{}
```

The Result interface is deliberately open so that future execution outcomes can be introduced.

V0 defines two Result types.

---

# Complete

```go
type Complete struct {
    NewCells []CellRequest
}
```

`Complete` means:

> The current Cell has completed successfully.

The Runtime instructs the Store to atomically:

1. complete the current Cell;
2. create the requested new Cells;
3. apply any returned application transaction work.

The empty form:

```go
Complete{
    NewCells: nil,
}
```

means that the Cell has completed and no new work is required.

This is the normal successful completion path.

---

## Application Transaction Work

`Complete` may additionally carry application persistence work.

Conceptually:

```go
type Complete struct {
    NewCells         []CellRequest
    ApplicationWork  ApplicationWork
}
```

`ApplicationWork` describes database mutations that should be committed atomically with completion.

The exact type is defined by the Store implementation and application transaction CDD.

The application work:

* operates against the transaction supplied by Cellar;
* may modify application-owned state;
* must not modify Cellar-owned state;
* must not commit or roll back the transaction;
* must not retain the transaction after completion.

---

# Retry

```go
type Retry struct {
    NotBefore *time.Time
}
```

`Retry` means:

> The current execution has not completed. Return the current Cell to the scheduler.

The lifecycle transition is:

```text
CLAIMED -> READY
```

If:

```go
NotBefore == nil
```

the Cell is immediately eligible for execution.

If `NotBefore` is supplied, the Cell is not eligible before that time.

Retry preserves the identity of the existing Cell.

Retry does not create a replacement Cell.

---

# Result Interpretation

The Worker does not interpret the business meaning of a Result.

The Runtime interprets the Result.

Conceptually:

```go
switch result := result.(type) {

case Complete:
    // atomically complete Cell,
    // create NewCells,
    // apply ApplicationWork

case Retry:
    // return Cell to READY
}
```

The Runtime then invokes the appropriate Store operation.

---

# Atomic Completion

A `Complete` Result represents one atomic persistence operation.

For example:

```text
Complete

    NewCells:
        SendEmail(alice)
        SendEmail(bob)

    ApplicationWork:
        UPDATE users ...
```

must result in a transaction equivalent to:

```text
BEGIN

application persistence work

complete current Cell

create SendEmail(alice)

create SendEmail(bob)

COMMIT
```

The Handler does not control the transaction boundary.

---

# Result Errors

A Handler Result describes the intended outcome of execution.

Failure to persist that outcome is not a Handler Result failure.

For example:

```text
Handler
    |
    | Complete
    v
Runtime
    |
    | Store failure
    v
Cellar fatal runtime error
```

V0 treats Store failure during Result application as fatal.

---

# Extensibility

Future Result types may be added.

Examples might include:

```text
Quarantine
DeadLetter
Pause
```

These are not part of V0.

Adding a Result type must not require exposing Store internals to Handlers.

---

# Non-goals

The Result model does not provide:

* distributed transactions;
* exactly-once external side effects;
* application retry policy;
* business workflow semantics;
* direct Store access from Handlers.

The Result represents what Cellar should do with the execution outcome.

---

# Design Principle

The Result boundary deliberately separates application logic from Cellar persistence:

```text
Handler decides:

    "What happened?"

and:

    "What work should follow?"

Cellar decides:

    "How do I persist that safely?"
```

The Handler therefore describes an execution outcome rather than issuing Cellar persistence commands directly.
