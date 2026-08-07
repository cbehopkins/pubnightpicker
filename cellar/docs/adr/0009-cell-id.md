# ADR-0009: Cell Identity and Allocation

## Status

Draft

## Context

Every Cell requires an identifier.

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

Cell identifiers are required for:

- persistence;
- scheduling;
- inspection;
- debugging;
- administration;
- observability.

Cell identifiers are not part of application business logic.

Applications may observe Cell identifiers, but must not depend on their representation or derive meaning from them.

The runtime requires a mechanism for allocating new identifiers while preserving the separation between execution and persistence.

## Decision

Cell identifiers are opaque values.

Conceptually:

```
type CellID string
```

The representation of a Cell identifier is intentionally unspecified.

Future implementations may use:

- counters;
- UUIDs;
- ULIDs;
- KSUIDs;
- other allocation strategies.

Applications and handlers must treat Cell identifiers as opaque.

Cell identifiers carry no business meaning.

Cellar owns the allocation of Cell identifiers.

Conceptually:

```go
type Allocator interface {
    Next() (CellID, error)
}
```

The allocator is a runtime concern.

The Store persists Cell identifiers but does not allocate them.

Conceptually:

```text
Handler
   |
   | Create work
   |
   v
Cellar
   |
   +---- Allocator
   |
   v
Store
```

Handlers request the creation of work.

Handlers do not construct Cell identifiers directly.

Cellar may validate newly allocated identifiers against the Store before persisting a Cell.

Such validation is an implementation detail of Cellar and does not affect the Allocator interface.

The Allocator remains independent of persistence concerns.

## Consequences

### Positive

- Identity allocation remains independent of persistence.
- Allocation strategies may change without affecting handlers or the Store.
- Cell identifiers remain suitable for debugging and observability.
- Business logic remains decoupled from operational concerns.
- Test implementations may provide deterministic allocators.

### Negative

- Cell creation requires an additional runtime component.
- Cell identifiers cannot safely encode business semantics.
- Collision handling is the responsibility of Cellar.

## Non-goals

This ADR does not define:

- a specific allocation algorithm;
- collision detection mechanisms;
- distributed allocation;
- relationships between Cells;
- business identifiers;
- parent-child execution semantics.

## Cross references

- ADR-0002: Cell Lifecycle and Execution Model
- ADR-0005: Payload Encoding and Type Safety
- ADR-0006: Cell Inspection and Debugging Model
- ADR-0007: Offline Cell Administration
- ADR-0008: Cell Lifecycle Notices
