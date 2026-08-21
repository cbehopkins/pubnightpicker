# ADR: Cellar Runtime Ownership and Handler Registration API

**Status:** Accepted
**Date:** 2026-08-21

## Context

The current Cellar integration exposes several parts of Cellar's internal execution machinery to the application.

An application currently has to:

1. Create a `cellar.Registry`.
2. Register handlers into the registry.
3. Freeze the registry.
4. Construct a `cellar.Worker`.
5. Construct a result applier.
6. Construct a dispatcher connecting the worker and store.
7. Construct a `cellar.Scheduler`.
8. Start and manage the resulting execution machinery.

The application therefore has knowledge of Cellar's internal composition and lifecycle.

The current handler registration API also exposes JSON serialization through the `runtime` package:

```go
runtime.RegisterJSON(registry, name, handler)
```

This requires the application to know that a registry exists and that JSON registration is a separate runtime concern.

This is unnecessary complexity for normal Cellar users.

Cellar's purpose is to persist, reconstruct, schedule, and execute Cells. The application should primarily need to provide the handlers that Cellar can execute and configure the Cellar runtime.

### Handler identity

A Cell currently identifies its handler using a human-readable `HandlerName`, for example:

```text
email.send.push
```

Using a human-readable string is valuable because a persisted Cell can be inspected and its intended operation understood directly.

However, requiring applications to invent globally unique strings creates a maintenance burden and creates the possibility of collisions.

This ADR does **not** attempt to solve generated handler identities. That is a future extension. The current human-readable handler name remains the authoritative identifier for the time being.

A future design may introduce an authoritative generated `HandlerRef`/ID while retaining an optional human-readable name for inspection and debugging.

### Serialization

Cell payloads currently use JSON. JSON is deliberately chosen because it is:

* simple;
* widely understood;
* easy to inspect;
* sufficient for the current requirements; and
* a natural representation for persisted Cell payloads.

There is currently no demonstrated requirement for applications to select or implement alternative serialization formats.

Introducing a generic codec abstraction into the application-facing API would therefore add complexity without providing current value.

## Decision

Cellar will own the complete Cell execution runtime.

The application-facing API will be deliberately small. Applications will:

* create/configure a Cellar runtime;
* register their handlers with Cellar;
* start the Cellar runtime.

Cellar will internally own:

* the handler registry;
* handler registration lifecycle;
* handler resolution;
* JSON serialization/deserialization;
* registry freezing;
* workers;
* dispatching;
* result application;
* scheduling.

These implementation components may continue to exist internally, but normal applications will not be required to construct or wire them together.

### Application-facing API

The intended shape is:

```go
c := cellar.New(cellarStore, cellar.Config{
    PollDelay: cfg.PollDelay,
})

c.Register(
    handlers.HandlerExampleIncrement,
    handlers.IncrementHandler{
        Counter: counterStore,
        Logger:  cfg.Logger,
    },
)

c.Register(
    handlers.HandlerExampleFanout,
    handlers.FanoutHandler{
        Logger: cfg.Logger,
    },
)

c.Register(
    handlers.HandlerNewPoll,
    handlers.NewPollHandler{
        Logger: cfg.Logger,
    },
)

c.Register(
    handlers.HandlerCompletedPoll,
    handlers.CompletedPollHandler{
        Logger: cfg.Logger,
    },
)

_, err := c.Add(handlers.HandlerExampleIncrement, incrementPayload)
if err != nil {
    return err
}

if err := c.Start(ctx); err != nil {
    return err
}
```

`Add` accepts a typed payload and stores its JSON representation in `Cell.Payload`.
Callers that already hold encoded or reconstructed Cells may continue to use the Store
API directly.

In particular, application code should **not** need to contain:

```go
registry := cellar.NewMemoryRegistry()
registry.Freeze()

worker := cellar.NewWorker(...)
scheduler := cellar.NewScheduler(...)
```

### Handler registration

Registration becomes a Cellar operation:

```go
c.Register(name, handler)
```

rather than an operation against an application-held `Registry`.

The application provides the implementation of the handler. Cellar owns the registration, identity, lookup, and execution machinery.

For the current implementation, the supplied handler's payload type will be serialized and deserialized using JSON.

The application should therefore not need to explicitly select a codec or call a `RegisterJSON` function.

Conceptually:

```text
Application handler
        │
        │ Register(name, handler)
        ▼
      Cellar
        │
        ├── handler identity
        ├── JSON serialization
        ├── registry
        └── execution
```

### JSON serialization

JSON becomes part of Cellar's current payload contract.

A Cell's payload is the JSON representation of the value accepted by its registered handler.

Cellar will internally perform:

```text
handler argument
      ↓
    JSON
      ↓
Cell.Payload
```

and:

```text
Cell.Payload
      ↓
    JSON
      ↓
handler argument
```

The generic `Codec` abstraction should not be part of the normal application-facing API unless a concrete future requirement justifies exposing alternative serialization mechanisms.

The implementation may retain internal codec-like structures where useful, but those are implementation details.

### Registry lifecycle

The registry is an internal Cellar component.

Applications do not create, freeze, or otherwise manage the registry.

The lifecycle becomes conceptually:

```text
New
 ↓
Register*
 ↓
Start
 ↓
Running
 ↓
Stop/Close
```

`Start` represents the transition from registration to execution. Any required registry freeze or equivalent preparation is performed internally by Cellar.

There is therefore no application-facing `Registry.Freeze()` requirement.

### Execution machinery

`Worker`, `Scheduler`, dispatcher implementations, result appliers, and similar components are internal Cellar implementation details.

Cellar is responsible for constructing and wiring these components.

This does not preclude refactoring the internal implementation later; the application-facing API should not depend upon their existence.

## Consequences

### Positive

**Simpler applications**

The application expresses its intent directly:

```go
cellar.Register("email.send.push", emailHandler)
```

rather than assembling Cellar's execution machinery.

**Clear ownership**

Cellar owns the complete lifecycle of a Cell:

```text
registration → persistence → reconstruction → resolution → execution → result application
```

The application supplies the business logic but does not participate in the execution pipeline.

**Smaller API surface**

Applications do not need to understand `Registry`, `Worker`, `Scheduler`, `Dispatcher`, or `ResultApplier`.

**JSON remains inspectable**

Persisted Cells remain straightforward to inspect. A human can understand both the handler being invoked and the JSON payload without knowing about a framework-specific serialization system.

**Future handler IDs remain possible**

The current API can later evolve from:

```text
HandlerName = "email.send.push"
```

to a model such as:

```text
HandlerRef
    ID   = generated authoritative identifier
    Name = "email.send.push"
```

without moving handler resolution out of Cellar.

### Negative

**Less immediate configurability**

Applications cannot directly replace Cellar's scheduler, worker, dispatcher, or serialization mechanism through the normal API.

This is intentional. Such flexibility is not currently required and would increase the complexity of the public contract.

**JSON becomes a Cellar-level decision**

Applications cannot independently select another payload encoding.

This is also intentional. Alternative serialization should only be introduced when there is a demonstrated requirement for it.

**More responsibility inside Cellar**

Cellar becomes responsible for constructing and managing its complete execution pipeline. This is appropriate because that pipeline is part of Cellar's core responsibility.

## Future considerations

### Generated handler identities

A future version may separate authoritative identity from human-readable naming.

For example:

```text
Handler ID:   7f3a91c2...
Handler Name: email.send.push
```

The generated ID would be authoritative and collision-resistant, while the name would remain available for debugging and inspection.

The current API should not prevent this evolution.

### Alternative serialization

If a concrete requirement emerges for formats such as protobuf, the serialization boundary can be reconsidered.

Such a change should be driven by an actual requirement rather than exposing a generic codec abstraction prematurely.

### Implemented API

The initial facade uses `New`, `Register`, `Add`, `Start`, and `Stop`. `Start` blocks
until its context is cancelled, `Stop` is called, or a fatal store or execution error
occurs. Registration closes when `Start` succeeds.

`Register` and `Add` are generic methods and therefore require Go 1.27 or later.
The payload type is inferred from the supplied handler or value, allowing Cellar to
construct a typed JSON registration without reflection.

## Decision summary

**Cellar owns the runtime.**

Applications provide handlers and configuration; they do not construct or wire Cellar's internal execution components.

**Cellar owns serialization.**

JSON is the current and intentionally simple Cell payload format. Applications do not need to select or implement codecs.

**Cellar owns handler registration and resolution.**

The current human-readable handler name remains the authoritative handler identity. A generated authoritative ID may be introduced later without changing this ownership model.

The desired application experience is therefore:

```go
cellar := cellar.New(store, config)

cellar.Register("email.send.push", emailSendPush)
cellar.Register("poll.new", newPoll)
cellar.Register("poll.completed", completedPoll)

cellar.Start(ctx)
```

Everything required to turn those registrations into scheduled Cell execution is Cellar's responsibility.
