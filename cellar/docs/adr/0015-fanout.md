# ADR: Work Expansion, Persistence, and Capacity-Aware Admission

**Status:** Proposed
**Date:** 2026-08-21

## Context

Cellar already has a fundamental mechanism by which the execution of a Cell can produce additional work.

The current result model includes:

```go
type Complete struct {
    NewCells        []CellRequest
    ApplicationWork []ApplicationWork
}
```

A Cell therefore does not merely terminate with success or failure. It can also cause additional durable Cells to be created.

This is an important existing semantic and should remain the fundamental mechanism for representing generated work.

A proposed higher-level workload type, Fanout, should exploit this mechanism rather than introducing a separate persistence or scheduling model.

A Fanout should conceptually behave as though a Cell executed and returned:

```go
Complete{
    NewCells: []CellRequest{
        // one request for each fanout target
    },
}
```

The fact that Cellar knows that a particular workload is a Fanout may permit implementation optimisations, but those optimisations must not alter the observable semantics.

### Runtime registrations

As with handlers and timers, Fanout definitions are process-local registrations.

The application is responsible for registering them on every startup.

The database does not need to persist or reconstruct the Fanout definition.

For example:

```text
Application startup

"order.completed" → Fanout definition
"email.send"     → Handler
"customer.update" → Handler
"analytics.publish" → Handler
```

A durable Cell only needs to identify the registered workload and contain its instance payload.

This follows the same model already established for durable timers.

Cellar is not required to support arbitrary historical application versions against an existing database. The currently running application supplies the registrations required to execute existing Cells.

## Decision

### 1. Generated work uses the existing `Result.NewCells` mechanism

There will be no separate semantic concept of "Fanout-generated persistence".

Fanout produces work in the same conceptual way as any other Cell:

```text
Cell / Workload
      │
      ▼
    Result
      │
      ▼
   NewCells
    ```

A Fanout is therefore conceptually equivalent to a workload that returns:

```go
Complete{
    NewCells: []CellRequest{
        ...
    },
}
```

The existing result/application machinery remains the semantic contract.

This ensures that:

* normal handlers;
* timers;
* fanouts; and
* future workload types

can all produce new durable work through the same mechanism.

### 2. Fanout is a higher-level workload, not a new scheduling primitive

A Fanout may be registered by an application as a named workload.

Conceptually:

```text
"order.completed"
        │
        ▼
     Fanout
        │
        ├── email.send
        ├── customer.update
        └── analytics.publish
      ```

      When executed, the Fanout produces `NewCells` targeting those registered handlers.

The application should not need to individually enqueue each child Cell.

However, the resulting child Cells are ordinary Cells.

Once materialised, they obey the same:

* persistence;
* `NotBefore`;
* claim;
* retry;
* execution;
* result; and
* failure

semantics as any other Cell.

### 3. Persistence is the correctness boundary

When a result creates new Cells, those Cells must be made durable before Cellar considers the result successfully applied.

For example:

```text
Parent Cell
    │
    ▼
Complete{
    NewCells: A, B, C
}
    │
    ▼
atomic persistence
    │
    ├── A persisted
    ├── B persisted
    └── C persisted
  ```

The operation must not allow a process failure to leave an ambiguous partial expansion such as:

```text
A persisted
B persisted
C missing
```

The store must therefore provide an appropriate atomic mechanism for applying a result and creating its NewCells.

The exact store API is an implementation concern, but the semantic requirement is:

> A successfully applied result must durably establish all of its requested child work.

### 4. Idempotency must be preserved across expansion

Result application must remain safe under retry.

Consider:

```text
Parent
  │
  ├── create A
  ├── create B
  ├── create C
  │
  └── process crashes
```

Cellar must not subsequently produce duplicate child Cells merely because the parent is retried.

The existing Cell identity/idempotency mechanisms should be used for this purpose.

The store remains authoritative for whether a Cell has already been created.

This is particularly important for Fanout because a Fanout may create many Cells in one operation.

### 5. Newly-created Cells remain ordinary durable work

Once the child Cells have been persisted:

```text
Fanout
   │
   ▼
A B C D E
```

they should conceptually become ordinary durable Cells.

They are subject to normal scheduling rules.

In particular, each child must be evaluated against its own `NotBefore`

before it is considered eligible for execution.

A Fanout must not bypass normal scheduling semantics merely because its children were created by the current execution.

### 6. Admission is separate from materialisation

Cellar should distinguish between:

* **Materialisation:** Does this work now exist durably?
* **Admission:** Should this work be claimed for execution now?
* **Execution:** Which worker should execute the claimed work?

Conceptually:

```text
Result
  │
  ▼
Materialise durable Cells
  │
  ▼
Determine eligible work
  │
  ▼
Admission / Claim
  │
  ▼
Worker
```

This distinction allows Cellar to safely create large amounts of durable work without necessarily claiming all of it immediately.

### 7. Capacity-aware claiming

The scheduler should be able to use current worker capacity as a scheduling signal.

A proposed initial mechanism is an atomic count of idle workers.

Conceptually:

```text
IdleWorkers = 5
TotalWorkers = 80
```

When deciding how much work to claim, the scheduler can use the available capacity to avoid claiming substantially more work than can immediately execute.

For example:

```text
100 newly-created Cells
        │
        ▼
5 idle workers
        │
        ▼
claim approximately 5
        │
        ├── dispatch
        │
        └── leave remaining Cells available
      ```

As workers complete work and become idle again, subsequent scheduler iterations can claim additional work.

This creates natural backpressure between durable work production and execution capacity.

### 8. Worker capacity is a scheduling hint, not a correctness mechanism

The idle-worker count is necessarily subject to races.

For example:

```text
Scheduler observes:

IdleWorkers = 5

Another scheduler/work source claims 3 workers

Actual available capacity = 2
```

Therefore the scheduler must not assume that the observed worker count guarantees a particular number of successful claims.

The rule is:

```text
worker capacity
     ↓
scheduling decision / claim target
     ↓
store claim operation
     ↓
actual claim result is authoritative
  ```

The database/store remains the source of truth for work ownership and claim state.

Worker capacity must never be required for correctness.

### 9. Fast-path admission of newly-created work

When a Cell produces `NewCells`, the scheduler may already have useful information about those Cells:

* their identities;
* their handler names;
* their `NotBefore` values;
* the current execution context; and
* current worker capacity.

Cellar may therefore optimise the normal scheduling cycle.

Instead of:

```text
persist new Cells
    ↓
wait for scheduler polling
    ↓
discover new Cells
    ↓
claim them
    ↓
dispatch them
  ```

it may perform:

  ```text
persist new Cells
    ↓
evaluate eligibility
    ↓
apply capacity-aware admission
    ↓
claim eligible Cells
    ↓
dispatch immediately
  ```

This is an optimisation only.

If the process fails immediately after persistence, the remaining durable Cells must be discoverable and executable through the ordinary scheduler.

Therefore:

> The fast path must never be required for correctness.

### 10. Fanout must not require special scheduler semantics

The scheduler should not need a fundamental execution branch such as:

```go
if workload.IsFanout() {
    ...
}
```

merely to preserve Fanout semantics.

The semantic execution path remains:

```text
Workload
   ↓
Result
   ↓
NewCells
   ↓
Persistence
   ↓
Admission
   ↓
Execution
```

Fanout may be recognised internally where doing so enables optimisation, but this must not create a second persistence or scheduling model.

The preferred architecture is:

```text
Normal Handler ─┐
Timer ──────────┤
Fanout ─────────┤
Future Workload ─┘
        │
        ▼
      Result
        │
        ▼
    NewCells
        │
        ▼
 common persistence/admission path
      ```

      ### 11. Runtime registration remains application-owned

A Fanout definition is analogous to a Handler or Timer registration.

For example:

```text
Application startup
        │
        ├── Register Handler "email.send"
        ├── Register Handler "customer.update"
        └── Register Fanout "order.completed"
```

The registration is process-local.

The database contains instances of work, not the application's registration configuration.

For an existing durable Fanout Cell:

```go
Cell.HandlerName = "order.completed"
```

Cellar resolves that name against the registrations supplied by the current application process.

This is the same model as the existing Timer contract:

> Applications must register the same name on every startup before Cellar starts.

### 12. Handler identity remains stable and human-readable

Fanout does not change the existing HandlerName model.

A durable Cell should continue to contain a meaningful name such as `order.completed`

rather than a generated opaque identifier.

The registered Fanout is resolved using that name.

Generated authoritative handler IDs may be considered separately in the future, but are outside this decision.

## Consequences

### Positive consequences

**A single semantic mechanism for generated work**

Handlers, Timers, Fanouts, and future workload types can all produce new Cells using the same Complete.NewCells mechanism.

This significantly reduces the number of fundamental concepts Cellar must understand.

**Simple durability model**

The database only needs to persist actual work.

It does not need to persist application registration definitions.

**Strong idempotency model**

Child Cells can use the existing Cell identity and persistence mechanisms.

**Natural optimisation point**

The scheduler can optimise newly-created work without changing the semantic model.

**Capacity-aware backpressure**

Large Fanouts can create durable work without immediately claiming all of it.

Worker capacity determines how much work is admitted for execution.

**Failure-safe fast path**

If immediate dispatch fails, durable work remains available to the ordinary scheduler.

### Negative consequences

**Store operations become more important**

The store must provide atomic result application / child Cell persistence semantics sufficient to prevent partial expansion.

**Scheduler becomes capacity-aware**

The scheduler will need to maintain or consume a reliable approximation of worker availability.

**Claiming becomes more dynamic**

The scheduler cannot simply claim every eligible Cell it discovers. It may need to make admission decisions based on current capacity.

**Worker count is only a hint**

The implementation must be careful not to accidentally turn worker counters into correctness state.

## Non-goals

This ADR does not define:

* the public Fanout API;
* the exact Fanout type;
* the exact store transaction API;
* the exact worker idle-counter implementation;
* priority scheduling;
* per-handler concurrency limits;
* multiple worker pools;
* workflow/sequence execution;
* durable workflow definitions; or
* versioned workload registrations.

These can be designed independently.

## Future work: Sequence

A future Sequence workload may build upon the same fundamental concepts.

Unlike Fanout, a Sequence cannot necessarily materialise all of its work immediately because later steps depend upon earlier steps completing.

Conceptually:

```text
Sequence
   │
   ▼
Step A
   │
   ▼
Step B
   │
   ▼
Step C
```

This will likely require durable orchestration/state-machine semantics.

The important decision made here is that Sequence should, where possible, continue to participate in Cellar's existing primitives rather than introducing an unrelated execution model.

In particular, the following should remain desirable:

```text
Sequence
    ↓
Cellar execution
    ↓
Results
    ↓
NewCells / other existing primitives
  ```

The detailed Sequence design is intentionally deferred.

## Summary

Cellar treats generated work as a fundamental property of `Result`:

```go
Complete{
    NewCells: []CellRequest{...},
}
```

Fanout is a higher-level workload that conceptually behaves exactly like a Cell returning such a result.

The durable semantics are:

```text
execute
  ↓
Result
  ↓
atomically persist NewCells
  ↓
durable work
```

The scheduling semantics are separate:

```text
durable work
  ↓
eligibility (`NotBefore`)
  ↓
capacity-aware admission
  ↓
claim
  ↓
worker
```

Worker capacity may be used to optimise how much work is claimed, but it is never part of the correctness model.

The key architectural principle is:

> Materialise work for correctness; admit work for capacity.

This permits Fanout and future workload types to share the same durable execution machinery while allowing Cellar to optimise the path from newly-created work to available workers.
