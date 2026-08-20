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

Cell identifiers are normally not part of application business logic.

Applications may observe Cell identifiers, but must not depend on the representation of runtime-allocated identifiers. Some applications need a stable identifier for singleton work, such as ensuring that a timer or background worker exists after every start.

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

Cellar owns the default allocation of Cell identifiers.

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

External application code may instead provide a Cell identifier when creating work. Caller-selected identifiers support stable, idempotent application patterns without changing the identity of an existing Cell. The Store rejects an identifier that is already present with `ErrCellAlreadyExists`; callers may treat that result as confirmation that the required work already exists.

Caller-selected identifiers must be non-empty. They remain opaque to Cellar: Cellar stores and compares them but does not interpret their contents.

The normal creation operation allocates identifiers and delegates persistence to the same atomic operation used for caller-selected identifiers. This keeps validation, collision handling and lifecycle initialisation consistent between both paths.

Cellar may validate newly allocated identifiers against the Store before persisting a Cell.

Such validation is an implementation detail of Cellar and does not affect the Allocator interface.

The Allocator remains independent of persistence concerns.

Allocators must therefore be self-allocating: an allocator must not depend on state that is
held only in process memory and would be lost on restart. A counter held solely in memory is
not a valid production allocator, because a restart rewinds it and reissues identifiers that
are already in use.

The default allocator emits UUIDv7 identifiers. UUIDv7 requires no persisted state, remains
unique across restarts and crashes, and is time-ordered so that identifier order approximates
creation order for debugging and inspection.

Deterministic counter-based allocators remain available for tests and simulations, where a
fresh allocator accompanies a fresh store.

## Consequences

### Positive

- Identity allocation remains independent of persistence.
- Allocation survives process restarts without a recovery step or shutdown hook.
- Allocation strategies may change without affecting handlers or the Store.
- Applications can assign stable identifiers to singleton work.
- Cell identifiers remain suitable for debugging and observability.
- Business logic remains decoupled from operational concerns.
- Test implementations may provide deterministic allocators.

### Negative

- Cell creation requires an additional runtime component.
- Runtime-allocated Cell identifiers cannot safely encode business semantics.
- Applications that select identifiers become responsible for their naming scheme and collision handling.
- Collision handling is the responsibility of Cellar.
- Default identifiers are long and not human-memorable.

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
