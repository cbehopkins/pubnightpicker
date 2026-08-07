# CDD: CellID Allocator

## Purpose

The CellID Allocator is a Cellar component responsible for generating unique identifiers for newly created Cells.

The allocator exists to isolate identity generation from:

* application code;
* handlers;
* persistence;
* Cell lifecycle management.

Cell IDs are operational identifiers only. They exist to allow Cells to be persisted, inspected, logged, and administered.

Cell IDs do not carry business meaning.

---

## Responsibilities

The CellID Allocator is responsible for:

* generating new Cell identifiers;
* providing an abstraction over the allocation strategy;
* allowing allocation strategies to change without affecting Cellar consumers.

The CellID Allocator is not responsible for:

* persisting Cells;
* checking Cell lifecycle state;
* storing allocated identifiers;
* understanding Handler types;
* understanding payloads.

---

## Interface

Conceptually:

```go
type Allocator interface {
    Next() (CellID, error)
}
```

The allocator returns an opaque CellID.

The caller does not make assumptions about:

* format;
* length;
* ordering;
* embedded information.

---

## Ownership

Cellar owns CellID allocation.

The creation flow is:

```text
Handler
    |
    | Request creation of work
    |
    v
Cellar
    |
    | Request new identifier
    |
    v
Allocator
    |
    | Return CellID
    |
    v
Cellar
    |
    | Persist Cell
    |
    v
Store
```

Handlers never allocate CellIDs directly.

The Store persists CellIDs but does not generate them.

---

## Allocation Strategy

The allocation algorithm is deliberately unspecified.

Possible implementations include:

* monotonically increasing counters;
* UUID-based allocation;
* ULID-based allocation;
* other unique identifier strategies.

The Allocator interface must remain unchanged when changing strategies.

Example:

```go
type CounterAllocator struct {
    next uint64
}
```

may later be replaced by:

```go
type UUIDAllocator struct{}
```

without affecting Cellar behaviour.

---

## Collision Handling

The Allocator is responsible for producing candidate identifiers.

Cellar may perform validation before persisting a new Cell.

Conceptually:

```text
Allocator
    |
    v
Candidate CellID
    |
    v
Cellar validation
    |
    v
Store
```

The Allocator does not require knowledge of the Store.

This preserves separation of concerns:

* Allocator owns identity generation.
* Cellar owns runtime policy.
* Store owns persistence.

---

## Failure Behaviour

If the Allocator cannot produce a CellID:

```go
id, err := allocator.Next()
```

then Cell creation fails.

Cellar must not create a Cell without an identifier.

Allocation failure is an infrastructure failure, not a Handler result.

---

## Testing

The Allocator interface allows deterministic implementations for testing.

Examples:

```go
type FixedAllocator struct {
    IDs []CellID
}
```

or:

```go
type CollisionAllocator struct{}
```

Tests may verify:

* Cell creation behaviour;
* collision handling;
* error propagation;
* deterministic Cell references.

---

## Non-goals

The CellID Allocator does not provide:

* globally distributed allocation;
* business identifiers;
* parent/child Cell relationships;
* ordering guarantees;
* execution history;
* persistence.

---

## Future Considerations

Future versions may introduce:

* stronger uniqueness guarantees;
* allocation metrics;
* allocation tracing;
* distributed allocation strategies.

These changes should not require modifications to Cell creation APIs or Handler implementations.
