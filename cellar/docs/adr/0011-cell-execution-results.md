# ADR-0008: Cell Execution Results and Completion Semantics

## Status

Draft

## Context

Cells represent persisted units of execution.

Handlers execute Cells and must communicate the outcome of that execution back to Cellar.

The runtime requires a mechanism that allows application code to describe:

* successful completion;
* creation of additional work;
* retry of incomplete work.

The design must preserve the separation between:

* application logic;
* Cellar execution semantics;
* persistence.

Handlers must not directly modify Cell state or interact with the Store.

Cellar must also support atomic replacement of completed Cells with newly created Cells, including application persistence work that must commit with the same transaction when the application and Cellar share a Base DB.

Example:

```text
SendAllEmails Cell

    executes

    creates:

        SendEmail(alice)
        SendEmail(bob)
        SendEmail(christine)
```

The parent Cell must not disappear unless the child Cells are successfully persisted.

---

# Decision

Handlers return execution Results.

The Runtime interprets Results and applies the corresponding Cellar operation.

Conceptually:

```go
type Result interface{}
```

V0 supports:

```go
type Complete struct {
    NewCells []CellRequest
}
```

and:

```go
type Retry struct {
    NotBefore *time.Time
}
```

Future Result types may be introduced as required.

---

# Complete Semantics

A Complete Result means:

> The current execution has finished. Replace this Cell with the specified new work.

The Runtime applies completion atomically through the Store.

When the Result includes application persistence work, the Store applies that work in the same transaction as the Cellar lifecycle operation.

Conceptually:

```text
BEGIN

application persistence work

DELETE completed Cell

CREATE NewCells

COMMIT
```

The list of new Cells may be empty.

Therefore:

```go
Complete{
    NewCells: nil,
}
```

means:

```text
Delete completed Cell.
Create no additional work.
```

This represents ordinary successful completion.

---

# Retry Semantics

A Retry Result means:

> The current execution has not completed successfully. Return this Cell to the scheduler.

Retry is a lifecycle transition:

```text
CLAIMED -> READY
```

with an optional execution delay.

Examples:

Immediate retry:

```go
Retry{
    NotBefore: nil,
}
```

Delayed retry:

```go
Retry{
    NotBefore: tomorrow,
}
```

Retry is deliberately not modelled as creation of a new Cell.

A retry represents continuation of the same persisted execution token.

---

# Handler Restrictions

Handlers:

* do not modify Cell state;
* do not access the Store;
* do not directly create Cells;
* do not decide persistence operations.

Handlers return descriptions of desired outcomes.

Cellar decides how those outcomes affect runtime state.

---

# Result Interpretation Ownership

Result interpretation belongs to the Cellar Runtime.

The flow is:

```text
Handler

    |

    v

Result

    |

    v

Runtime

    |

    v

Store lifecycle operation
```

The Worker only transports the Result.

The Store only performs persistence operations.

---

# Atomic Child Creation

When a Handler creates additional Cells, those Cells are created as part of completion.

Cellar does not support:

```text
Execute Handler

Create child Cell

Delete parent Cell
```

because failure between those operations could leave inconsistent state.

Instead:

```text
Delete parent Cell

Create child Cells

```

must be one atomic Store operation.

---

# Reliability Model

Cellar provides:

* persisted execution state;
* recovery after restart;
* scheduling of persisted work.

Cellar does not provide:

* exactly-once execution;
* distributed transactions;
* coordination with external systems.

Handlers remain responsible for making external side effects safe under duplicate execution.

Examples:

* sending emails;
* calling APIs;
* modifying external databases.

---

# Store Failure Semantics

A Store failure during Result application means Cellar cannot guarantee that execution state has been persisted correctly.

Examples:

* unable to write persistence state;
* completed Cell does not exist;
* new Cell identity conflicts.

These failures are not considered Handler failures.

V0 behaviour:

```text
Store lifecycle failure

        |

        v

Cellar shutdown
```

After restart, normal recovery semantics apply.

Continuing execution with uncertain persisted state is considered unsafe.

---

# Non-goals

This ADR does not define:

* business retry policies;
* external transaction handling;
* Handler idempotency strategies;
* workflow semantics.

Those remain application responsibilities.

---

# Rationale

The Result model keeps responsibilities separated:

Handlers decide what happened.

Runtime decides what that means for Cellar.

Store guarantees persistence.

This allows Cellar to remain a simple execution engine without understanding application semantics.
