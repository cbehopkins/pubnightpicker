# CDD: Executor Store Architecture

## Purpose

The Executor Store is the persistence boundary for Cellar.

Its responsibility is to persist Cells and provide safe lifecycle transition operations required by the runtime.

The Store does not expose arbitrary persistence mutation through the primary runtime interface.

Instead, it exposes operations that correspond to valid Cell lifecycle transitions.

The Store is therefore not simply a database abstraction.

It is the enforcement boundary for the Cell lifecycle model.

---

# Responsibilities

The Store is responsible for:

* persisting Cells;
* atomically transitioning Cells between valid lifecycle states;
* recovering Cells after runtime restart;
* providing access to active Cells for startup validation;
* providing transactional guarantees for execution completion.

The Store is not responsible for:

* executing Handlers;
* decoding payloads;
* scheduling work;
* interpreting business logic;
* deciding retry policy;
* understanding Handler semantics.

---

# Relationship to other components

The high-level relationship is:

```text
                         Cellar Runtime

                              |

                              v

                         Executor Store


        +---------------------+---------------------+

        |                     |                     |

        v                     v                     v


   Scheduler              Worker              Startup

 Claim Cells          Complete Cells       Validate Cells

 READY -> CLAIMED     CLAIMED -> ...       Handler lookup
```

The Store remains the authority for persisted Cell state.

---

# Cell lifecycle ownership

Cellar lifecycle transitions are:

```text
READY
  |
  |
  v
CLAIMED
  |
  |
  +----------------+
  |                |
  v                v

DELETED          READY

Completion       Retry
```

The Store exposes operations corresponding to these transitions.

The Store does not expose a general:

```go
UpdateCell(cell Cell)
```

operation through the runtime interface.

This prevents components from bypassing lifecycle rules.

---

# Operational Store Interface

Conceptually:

```go
type Store interface {

    Open(ctx context.Context) error

    Close(ctx context.Context) error


    ClaimNext(
        now time.Time,
    ) (Cell, bool, error)


    Complete(
        cellID CellID,
        additions []CellRequest,
    ) error


    Retry(
        cellID CellID,
        notBefore *time.Time,
    ) error


    Recover() error


    ListActive() ([]Cell, error)


    Add(
        requests []CellRequest,
    ) ([]CellID, error)
}
```

The exact Go interface may evolve, but the responsibilities remain fixed.

---

# Store Operations

## Open

Initialises Store resources.

Examples:

* database connections;
* filesystem resources;
* migrations;
* transaction infrastructure.

Cellar Runtime owns Store lifecycle.

---

## Close

Releases Store resources.

Shutdown ordering is controlled by Cellar Runtime.

---

# ClaimNext

Used by the Scheduler.

Purpose:

Discover and claim runnable work.

Semantics:

Find a Cell where:

```text
State == READY

AND

(NotBefore == NULL OR NotBefore <= now)
```

Then atomically transition:

```text
READY -> CLAIMED
```

The Store must ensure that a Cell cannot be claimed twice.

Only the Scheduler performs this operation.

---

# Complete

Used when a Handler execution has completed successfully.

Semantics:

Atomically:

```text
Delete completed Cell

+

Create zero or more replacement Cells
```

Conceptually:

```sql
BEGIN;

DELETE FROM cells
WHERE id = completed_cell;

INSERT replacement cells;

COMMIT;
```

The additions list may be empty.

Therefore:

```go
Complete(
    cellID,
    nil,
)
```

represents successful completion with no follow-up work.

A fan-out operation:

```go
Complete(
    parentID,
    []CellRequest{
        childA,
        childB,
        childC,
    },
)
```

represents replacing one execution token with several new execution tokens.

---

# Retry

Used when execution should be attempted again.

Semantics:

```text
CLAIMED -> READY
```

with an optional execution delay.

Examples:

Immediate retry:

```go
Retry(
    cellID,
    nil,
)
```

Delayed retry:

```go
Retry(
    cellID,
    tomorrow,
)
```

Retry is separate from Complete because it represents continuation of the same logical execution attempt.

It is not creation of new work.

---

# Recover

Used during startup.

Purpose:

Return incomplete work to an executable state.

Semantics:

```text
CLAIMED -> READY
```

Conceptually:

```sql
UPDATE cells
SET state = READY
WHERE state = CLAIMED;
```

Recovery does not distinguish between:

* process crash;
* SIGTERM;
* panic;
* machine failure.

A claimed Cell that was not completed is returned to execution.

---

# ListActive

Used during startup validation and administration.

Returns:

```go
[]Cell
```

for all Cells that still exist.

Deleted Cells are not returned.

Cellar uses this during startup to validate that all persisted Cells reference known Handlers.

The Store does not interpret HandlerName values.

---

# Add

Used to introduce new work into Cellar.

Examples:

* external listeners;
* application code;
* administrative tooling.

Adding Cells is different from completing execution.

A Handler completion path should use:

```text
Complete()
```

rather than:

```text
Add()
```

because completion requires atomic replacement of the completed Cell.

---

# DebuggableStore

Debugging and administration require operations that deliberately bypass normal lifecycle restrictions.

These operations are not part of the runtime Store interface.

Conceptually:

```go
type DebuggableStore interface {

    Store


    ListAll() ([]Cell, error)


    Get(
        id CellID,
    ) (Cell, error)


    ForceUpdate(
        cell Cell,
    ) error


    ForceDelete(
        id CellID,
    ) error
}
```

The DebuggableStore interface exists for:

* offline inspection;
* debugging tools;
* repair operations.

It must be treated as an administrative override.

---

# Debugging Safety Model

V0 debugging operates under the assumption that:

```text
Cellar runtime is stopped
```

while administrative mutations occur.

Live debugging against a running Scheduler is intentionally deferred.

Future versions may introduce:

* scheduler pause;
* controlled maintenance mode;
* transactional administrative operations.

---

# Startup Validation

Cellar startup uses:

```text
Store.ListActive()
```

to obtain persisted Cells.

Each Cell is checked:

```text
Cell.HandlerName exists in Registry
```

Startup validation does not require:

* payload decoding;
* business validation;
* external dependency checks.

Those occur during execution.

---

# Non-goals

The Store does not provide:

* workflow semantics;
* business transactions;
* Handler execution;
* distributed coordination;
* arbitrary object persistence;
* generic update APIs.

---

# Design Principles

## Lifecycle over mutation

The Store exposes meaningful transitions rather than arbitrary data updates.

## Atomic completion

A completed execution must produce a consistent persisted state.

## Runtime safety first

The operational interface prevents accidental lifecycle violations.

## Administrative power is separate

Debugging requires powerful tools, but those tools must be explicitly separate from normal execution.
