# ADR: Ordered Multi-Step Cells

**Status:** Proposed
**Date:** 2026-08-21

## Context

Cellar currently models a Cell as a single invocation of a named Handler:

```go
type Cell struct {
    ID          CellID
    HandlerName HandlerName
    Payload     []byte

    State       CellState
    NotBefore   *time.Time
}
```

This works well for ordinary units of work, but does not naturally represent a higher-level sequence of handlers where:

* handlers must execute in a fixed order;
* each handler receives its own payload;
* execution may take significant time;
* the process may restart between steps;
* the current position in the sequence must therefore be durable;
* each step should retain the existing Cell execution semantics;
* handlers may still create ordinary new Cells and `ApplicationWork`; and
* failures and retries should use the existing Cellar result model where possible.

A previous approach considered introducing a separate Sequence orchestration object which wrapped a normal Cell and maintained sequence state externally.

That introduces an additional durable concept which must be coordinated with the Cell itself.

A simpler model is to make the Cell itself capable of containing multiple ordered steps.

A one-step Cell then becomes the normal case of the more general model.

## Decision

### 1. A Cell may contain one or more ordered steps

The Cell representation will evolve conceptually from:

```go
type Cell struct {
    ID          CellID
    HandlerName HandlerName
    Payload     []byte

    State       CellState
    NotBefore   *time.Time
}
```

to:

```go
type Cell struct {
    ID          CellID
    Steps       []CellStep

    State       CellState
    CurrentStep int
    NotBefore   *time.Time
}
```

where:

```go
type CellStep struct {
    HandlerName HandlerName
    Payload     []byte
}
```

The invariant is:

```text
len(Steps) > 0
```

and:

```text
0 <= CurrentStep <= len(Steps)
```

`CurrentStep == len(Steps)` represents a fully completed Cell and should normally correspond with a terminal `CellState`.

A normal Cell is simply a Cell containing one step:

```text
Steps:
    [0] email.send

CurrentStep:
    0
```

A Sequence is a Cell containing multiple steps:

```text
Steps:
    [0] hello.greet
    [1] hello.speak
    [2] hello.swear

CurrentStep:
    1
```

There is therefore no separate durable Sequence object.

A Sequence is a multi-step Cell.

### 2. The application-facing API has separate simple and sequence entry points

Go does not support function overloading, and attempting to make one `Add` method accept both ordinary work and arbitrary sequences would require an unnecessarily weakly typed API such as:

```go
Add(args ...any)
```

This is explicitly rejected.

The public API will instead retain a simple `Add` operation for ordinary work and provide a separate `AddSequence` operation.

Conceptually:

```go
func (c *Cellar) Add(
    handlerName HandlerName,
    payload any,
) (CellID, error)
```

and:

```go
func (c *Cellar) AddSequence(
    steps ...Step,
) (CellID, error)
```

where the application-facing `Step` is:

```go
type Step struct {
    HandlerName HandlerName
    Payload     any
}
```

Example ordinary work:

```go
cellID, err := c.Add(
    "email.send",
    EmailPayload{
        To:      "fred@example.com",
        Subject: "Hello",
    },
)
```

Example sequence:

```go
cellID, err := c.AddSequence(
    cellar.Step{
        HandlerName: "hello.greet",
        Payload:     greeting{Name: "Cellar"},
    },
    cellar.Step{
        HandlerName: "hello.speak",
        Payload:     greeting{Name: "World"},
    },
    cellar.Step{
        HandlerName: "hello.swear",
        Payload:     greeting{Name: "Darn"},
    },
)
```

`Add` is effectively syntactic sugar for creating a one-step Cell.

`AddSequence` must reject an empty step list.

The application-facing API uses `any` for payloads because different steps may have different payload types. Cellar is responsible for JSON serialisation and deserialisation.

The persisted representation uses `[]byte`.

### 3. Sequence steps use the existing Handler registration mechanism

A Sequence does not contain Handler implementations.

It contains Handler names:

```text
Sequence
    │
    ├── "hello.greet"
    ├── "hello.speak"
    └── "hello.swear"
```

At execution time Cellar resolves the current `HandlerName` through the existing Handler registry.

The application remains responsible for registering the appropriate Handlers during startup.

For example:

```text
Application startup

"hello.greet"  → Handler
"hello.speak"  → Handler
"hello.swear"  → Handler
```

This follows the existing Cellar registration model.

The database persists the workload instance, not the application's Handler implementation or registration configuration.

### 4. A Sequence step receives its own payload

Each `Step` contains its own payload:

```text
Step 0:
    Handler = "hello.greet"
    Payload = greeting{Name: "Cellar"}

Step 1:
    Handler = "hello.speak"
    Payload = greeting{Name: "World"}

Step 2:
    Handler = "hello.swear"
    Payload = greeting{Name: "Darn"}
```

Steps may therefore have completely different payload types.

Cellar does not need a common Go type for all step payloads.

Each Handler registration remains responsible for decoding and validating the payload appropriate to that Handler.

JSON remains the internal serialisation format.

### 5. Steps execute strictly in order

A Cell with:

```text
Steps:
    A
    B
    C
```

must execute `A → B → C`.

There is no branching.

There is no parallel execution within a Cell.

There is no Fanout semantics within a Sequence.

A Sequence step may itself create ordinary `NewCells`, including Cells which happen to be multi-step Cells, but those Cells are independent work and are not part of the current Sequence.

This preserves the distinction:

* Sequence creates multiple steps within one unit of work.
* Fanout creates multiple independent units of work.

### 6. `CurrentStep` is the durable Sequence state

The Cell's `CurrentStep` identifies the next Handler to execute.

For example:

```text
Steps:
    0 → validate
    1 → reserve
    2 → charge
    3 → notify

CurrentStep = 2
```

means that `charge` is the next Handler to execute.

There is no separate Sequence state machine.

The Cell itself is the durable state machine.

This means process restart requires no special Sequence reconstruction.

If the process dies while Step 2 is executing and the durable Cell still contains:

```text
CurrentStep = 2
```

then the Cell is simply retried from Step 2, using the normal Cell retry/recovery machinery.

### 7. Successful step completion advances `CurrentStep`

When the current Handler returns a successful `Complete` result:

```text
CurrentStep = CurrentStep + 1
```

If additional steps remain, the Cell remains available for execution.

If the final step completes:

```text
CurrentStep == len(Steps)
```

and the Cell becomes complete.

The Cell ID remains unchanged throughout the entire Sequence.

### 8. Result application and sequence advancement must be atomic

This is a critical durability invariant.

A Handler may return:

```go
Complete{
    NewCells:        ...,
    ApplicationWork: ...,
}
```

When executing as a Sequence step, Cellar must atomically:

* apply the `Result`;
* persist any `NewCells`;
* persist any `ApplicationWork`;
* advance `CurrentStep`; and
* update the Cell's terminal state if the final step completed.

Conceptually:

```text
BEGIN

    apply Result.NewCells
    apply Result.ApplicationWork

    CurrentStep++

    if CurrentStep == len(Steps):
        State = COMPLETE

COMMIT
```

The sequence step must never be durably advanced independently of the successful application of its result.

Likewise, child work must never be committed independently of the corresponding sequence transition.

This prevents both forms of failure:

```text
Child work persisted
CurrentStep not advanced
```

and:

```text
CurrentStep advanced
Child work not persisted
```

The existing Cell/result persistence mechanism should be extended as necessary to provide this atomicity.

### 9. Ordinary `Retry` retries the current step

A normal `Retry` result means: execute the current step again.

`CurrentStep` is therefore unchanged.

Example:

```text
CurrentStep = 2

Handler returns Retry

CurrentStep = 2
```

The normal Cell retry scheduling semantics apply.

A retry may specify a future `NotBefore`.

### 10. `RetrySequence` retries the entire Sequence

A Sequence requires a distinct operation for restarting from its beginning.

A new result type should therefore be introduced conceptually as:

```go
type RetrySequence struct {
    Delay time.Duration
}
```

The precise result API may differ, but the semantics are:

```text
CurrentStep = 0
NotBefore   = now + Delay
```

and the update must be atomic.

Example:

```go
return cellar.RetrySequence{
    Delay: 100 * time.Second,
}
```

means:

Restart the entire Sequence from its first step, but do not attempt it until at least 100 seconds from now.

This is particularly useful when an external system is eventually consistent.

For example:

```text
Step 3
    ↓
look for external log entry
    ↓
entry not present
    ↓
RetrySequence{Delay: 100s}
```

The Sequence will then restart at Step 0 after the delay.

`RetrySequence` is deliberately distinct from ordinary `Retry`:

```text
Retry
    → CurrentStep unchanged

RetrySequence
    → CurrentStep = 0
```

### 11. `NotBefore` is a durable eligibility boundary

`NotBefore` represents the earliest time at which the Cell may execute.

It is not consumed when the Cell becomes eligible.

For example:

```text
NotBefore = 10:00
```

At 10:00 the Cell becomes eligible, but `NotBefore` remains 10:00.

There is no need to clear it because time does not run backwards.

A subsequent retry may replace it with a later value.

For example:

```text
Step 1
    ↓
Retry{NotBefore: 10:30}
```

results in:

```text
NotBefore = 10:30
```

For a `RetrySequence{Delay: 100s}`, Cellar calculates and persists the corresponding future `NotBefore`.

A successful transition from one Sequence step to the next does not require clearing `NotBefore`.

### 12. `Kill` terminates the Cell

A `Kill` result terminates the current Cell.

No subsequent Sequence steps are executed.

The exact existing Kill semantics remain authoritative.

Unless explicitly defined otherwise by the existing `Result` contract, a terminal `Kill` should not advance `CurrentStep` or continue the Sequence.

### 13. Sequence steps do not have Cellar-managed return values

Handlers within a Sequence do not pass arbitrary return values directly to subsequent steps.

For example, Cellar will not initially support:

```text
Step A → value → Step B
```

Instead, handlers should communicate through application state.

For example:

```text
Step A
    ↓
update durable application state

Step B
    ↓
read durable application state
```

This deliberately avoids introducing a second data-flow or serialisation system into Cellar.

If a future requirement demonstrates a compelling need for step return values, that can be addressed separately.

### 14. Steps may create ordinary work

A Sequence step is otherwise an ordinary Handler invocation.

It may return:

```go
Complete{
    NewCells:        ...,
    ApplicationWork: ...,
}
```

Those new Cells are independent work.

They are not implicitly added to the Sequence.

For example:

```text
Sequence
    Step A
      │
      ├── advances Sequence to Step B
      │
      └── creates Cell X
```


Cell X is independent of the Sequence.

If X is itself a multi-step Cell, that is simply another Cell.

### 15. Missing Handler registration is an execution failure

If the current `HandlerName` cannot be resolved through the application's registry, Cellar must not silently advance the Sequence.

The existing Handler resolution/error semantics apply.

The application is responsible for ensuring that required Handler registrations exist before Cellar starts processing work.

### 16. A Sequence may create another Sequence

There is no special restriction against nesting or composition through generated work.

A Handler in one Sequence may create a new Cell using `AddSequence`.

The resulting Cell is independent work:

```text
Sequence A
    │
    └── Handler
          │
          └── creates Sequence B
```

No additional orchestration relationship is required.

### 17. Inspection should expose Sequence state

Cell inspection should make the current position visible.

Conceptually:

```text
Cell: abc123
State: AVAILABLE
Current Step: 2 / 4

Steps:
  0: order.validate
  1: order.reserve
  2: order.charge       ← current
  3: order.notify
```

The current Handler and its decoded payload should remain inspectable using the existing inspection mechanisms.

This makes the multi-step Cell useful for debugging without introducing a separate Sequence object.

## Consequences

### Positive consequences

**Sequence becomes a fundamental Cell capability**

No separate orchestration abstraction is required.

**Restart is simple**

The durable `CurrentStep` is sufficient to resume the Sequence.

**Existing Handler infrastructure is reused**

No new Handler registration model is required.

**Existing Result semantics are reused**

Handlers continue to return `Complete`, `Retry`, `Kill`, etc.

**One-step and multi-step work share the same machinery**

A normal Cell is simply a one-step Cell.

**Sequence and Fanout remain conceptually distinct**

A Sequence produces ordered steps within one Cell.

A Fanout produces multiple independent Cells.

**Application code remains clear**

Ordinary work:

```go
c.Add("email.send", payload)
```

Sequence:

```go
c.AddSequence(
    cellar.Step{...},
    cellar.Step{...},
    cellar.Step{...},
)
```

The call site clearly communicates the intent.

### Negative consequences

**Cell representation becomes more complex**

Every Cell now contains a list of steps rather than one Handler/payload pair.

**Persistence changes are required**

The Cell store and serialisation format must support:

* multiple steps;
* `CurrentStep`;
* atomic step advancement; and
* atomic application of step results.

**Result application becomes more sophisticated**

The result applier must understand that `Complete` may mean:

* complete the Cell; or
* advance it to its next step.

**Existing code assuming one `HandlerName` must be updated**

All Cell consumers must stop assuming:

```go
cell.HandlerName
cell.Payload
```

and instead operate on:

```go
cell.Steps[cell.CurrentStep]
```

where appropriate.

## Non-goals

This ADR does not introduce:

* branching;
* conditional steps;
* parallel steps;
* fanout within a Sequence;
* arbitrary step return values;
* inter-step Cellar-managed data flow;
* durable Sequence definitions separate from Cells;
* workflow versioning; or
``* dynamic modification of a Sequence after it has been created.

## Summary

Cellar will treat a Cell as an ordered collection of Handler invocations with a durable execution cursor.

```go
type Cell struct {
    ID           CellID
    Steps        []CellStep
    State        CellState
    CurrentStep  int
    NotBefore    *time.Time
}
```

A normal Cell contains one Step.

A Sequence contains multiple Steps.

The current Handler is:

```go
cell.Steps[cell.CurrentStep]
```

Successful execution advances `CurrentStep`.

A process restart naturally resumes from the persisted `CurrentStep`.

The result of each step and the corresponding state transition must be applied atomically.

The public API remains deliberately simple:

```go
c.Add(handlerName, payload)
```

for ordinary work, and:

```go
c.AddSequence(
    cellar.Step{...},
    cellar.Step{...},
    cellar.Step{...},
)
```

for ordered work.

The fundamental architectural principle is:

> A Sequence is not a separate orchestration system. It is simply a Cell with more than one ordered step.
