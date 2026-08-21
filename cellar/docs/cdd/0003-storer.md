# CDD: Cell Store

## Purpose

The Cell Store is the persistence layer used by Cellar to maintain the current state of Cells.

The Store is the authoritative source of persisted Cell state.

The Store is responsible for ensuring that Cell lifecycle transitions requested by Cellar are persisted correctly.

The Store does not execute Cells or understand application behaviour.

The persistence boundary is a single Base DB that may host both Cellar-owned and application-owned schema. The Store implementation therefore preserves Cellar lifecycle semantics while also enabling the application to create and manage its own schema and transactions in the shared database.

---

## Responsibilities

The Store is responsible for:

* persisting newly created Cells;
* retrieving runnable Cells;
* atomically claiming Cells for execution;
* atomically replacing completed Cells with zero or more child Cells;
* recovering claimed Cells after restart;
* providing access to persisted Cell data for inspection and administration;
* enabling the application to create and manage its own schema and transactions within the shared Base DB.

The Store is not responsible for:

* generating CellIDs;
* executing Handlers;
* decoding Payloads;
* deciding retry policy;
* emitting Notices;
* scheduling policy.

---

## Stored Entity

The Store persists Cells.

Conceptually:

```go
type Cell struct {
    ID          CellID
    HandlerName HandlerName
    Payload     []byte

    State       CellState
    NotBefore   *time.Time
}
```

The Store treats Payload as opaque bytes.

The Store does not understand the type represented by Payload.

---

## Cell Lifecycle Ownership

The Store does not own lifecycle decisions.

Cellar decides when lifecycle transitions should occur.

The Store provides the persistence primitives required to make those transitions safe.

Lifecycle:

```text
READY
  |
  | Cellar Scheduler
  |
  v
CLAIMED
  |
  | Cellar Worker
  |
  v
DELETED
```

Recovery:

```text
CLAIMED
  |
  | Cellar startup recovery
  |
  v
READY
```

---

## Required Operations

Conceptually, the Store provides:

```go
type Store interface {

  Add([]CellRequest) ([]CellID, error)

    ClaimNext(now time.Time) (Cell, bool, error)

  Complete(
    CellID,
    []CellRequest,
    ...ApplicationWork,
  ) error

  Retry(CellID, *time.Time) error

    Recover() error
}
```

Additional inspection operations may exist:

```go
List() ([]Cell, error)

Get(CellID) (Cell, error)
```

These operations support debugging and administration.

---

## Adding Cells

When Cellar creates a Cell:

1. Cellar obtains a CellID from the Allocator.
2. Cellar constructs the Cell.
3. The Store persists the Cell.

The Store does not generate identifiers.

The Store does not modify the Cell contents.

---

## Claiming Cells

The Scheduler requires an atomic operation:

```text
Find runnable Cell
        +
Transition READY -> CLAIMED
        +
Return Cell
```

The Store must ensure that only one Scheduler action can successfully claim a given Cell.

A Cell is runnable when:

```text
State == READY

AND

(NotBefore == NULL OR NotBefore <= now)
```

---

## Completing Cells

Completion atomically removes the claimed parent and creates zero or more child Cells.
An empty `CellRequest.ID` requests allocation; a non-empty ID is preserved. Both forms
participate in the same operation.

When a Handler result requires completion:

```text
CLAIMED parent
  +
zero or more READY children
```

If validation, ID allocation, application work, insertion, or deletion fails, the
operation must leave the claimed parent and all pre-existing Cells unchanged.

---

## Recovery

On startup, Cellar requests recovery of unfinished work.

The Store supports:

```text
CLAIMED -> READY
```

Recovery does not distinguish between:

* panic;
* process termination;
* power failure;
* unexpected shutdown.

All claimed Cells are treated as incomplete execution.

---

## Transaction Requirements

The Store must provide atomicity for lifecycle-critical operations.

In particular:

### Claim

The following must behave as one operation:

```text
SELECT runnable Cell

UPDATE state READY -> CLAIMED

RETURN Cell
```

Multiple competing claims for the same Cell must not occur.

---

### Complete

The following must behave as one operation:

```text
Validate all child requests

Allocate IDs for ordinary child requests

Execute application transaction work

Insert every allocated and identified child

Delete the claimed parent
```

Caller-selected IDs remain opaque to the Store. An existing or repeated ID causes
`ErrCellAlreadyExists` and rolls back the entire completion.

---

## Payload Handling

Payloads are opaque.

The Store stores:

```go
[]byte
```

and does not:

* decode;
* validate;
* inspect;
* modify.

Payload interpretation belongs to the registered Codec.

---

## Implementation

The initial Store implementation may use a relational database.

The initial design targets:

* single Cellar process;
* single Scheduler;
* low throughput;
* simple recovery.

Distributed storage semantics are not required.

---

## Testing

The Store should support tests for:

* creating Cells;
* retrieving runnable Cells;
* claiming Cells atomically;
* deleting Cells;
* atomically replacing a parent with mixed allocated and pre-identified children;
* rolling back the whole completion when a child ID collides;
* recovering claimed Cells;
* handling duplicate IDs;
* handling concurrent access.

A test Store implementation may be provided for unit tests.

---

## Non-goals

The V0 Store does not provide:

* distributed coordination;
* multi-Scheduler locking;
* event sourcing;
* execution history;
* retry history;
* payload storage separate from Cells;
* business queries.

---

## Future Considerations

Future versions may introduce:

* execution history tables;
* durable Notice storage;
* advanced indexing;
* alternative persistence engines;
* distributed execution support.

These should not change the Store's core responsibility:

> Persist and safely transition Cell state.
