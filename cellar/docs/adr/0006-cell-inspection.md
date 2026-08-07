# ADR-0006: Cell Inspection and Debugging Model

## Status

Draft

## Context

Cells are persisted execution primitives.

The runtime stores:

```go
type Cell struct {
    ID          CellID
    HandlerName HandlerName
    Payload     []byte

    State       CellState
    NotBefore   *time.Time
}
```

The Cell runtime intentionally treats Payload as opaque bytes.

The runtime is responsible for:

- scheduling;
- persistence;
- recovery;
- handler lookup;
- payload decoding;
- execution.

Applications are responsible for:

- defining payload types;
- providing codecs;
- implementing handlers.

The execution model is therefore:

```text
Cell
 |
 | HandlerName
 |
 v
Handler Registration
 |
 +----------------+
 |                |
 v                v
Codec[T]       Handler[T]
 |
 |
 v
T
```

While this separation is desirable for execution, it creates an operational challenge.

Operators and developers need to answer questions such as:

- What Cells currently exist?
- Which handler owns this Cell?
- Can the payload still be decoded?
- What does this payload represent?
- Why did execution fail?

The runtime cannot answer these questions from the Cell alone because payloads are intentionally opaque.

Cell inspection capabilities may be consumed by offline administrative tools operating directly against the Store. Live mutation of running Cellar state is explicitly out of scope.

## Decision

Cellar provides an optional inspection capability separate from execution.

Inspection is implemented by the same runtime component that performs execution, but it is exposed through a separate interface.

Conceptually:

```go
type Executor interface {
    Run(ctx context.Context) error
}

type Inspector interface {
    Inspect(id CellID) Inspection
}
```

A runtime may implement both interfaces:

```go
type Cellar struct {
    Executor
    Inspector
}
```

However, execution does not depend on inspection.

A deployment may use Cellar without enabling or exposing debugging facilities.

## Registration model

Handlers are registered together with their payload codec.

Conceptually:

```go
func Register[T any](
    name HandlerName,
    codec Codec[T],
    handler Handler[T],
)
```

The runtime stores registrations behind a non-generic interface.

The public API remains strongly typed.

The internal registry performs generic erasure at registration time.

Conceptually:

```go
type registration interface {

    Execute(
        ctx context.Context,
        cell Cell,
    ) Result

    Inspect(
        cell Cell,
    ) Inspection
}
```

A typed implementation provides the bridge:

```go
type typedRegistration[T any] struct {
    codec   Codec[T]
    handler Handler[T]
}
```

The same codec used during execution is used during inspection.

## Inspection semantics

Inspection begins with a persisted Cell.

The runtime:

- loads the Cell;
- finds the registered handler by HandlerName;
- obtains the associated codec;
- attempts to decode Payload;
- returns an Inspection result.

Conceptually:

```go
type Inspection struct {
    Cell Cell

    Payload any

    DecodeError error
}
```

A successful inspection contains the decoded application type.

A failed inspection contains the original Cell and the decoding failure.

The runtime must not discard or hide undecodable payloads.

## Payload description

Cellar does not require payload types to implement debugging methods.

Human-readable representation is an optional capability.

Conceptually:

```go
type DebugSupport[T any] interface {
    Describe(T) string
}
```

Applications may provide custom descriptions where required.

Examples:

- sensitive payloads requiring redaction;
- domain-specific formatting;
- summaries of large payloads.

Without custom debug support, Cellar may use a default representation.

Possible fallback order:

- application supplied description;
- fmt.Stringer;
- codec-specific representation;
- generic Go representation.

## Validation

Inspection provides validation implicitly through decoding.

A Cell whose payload cannot be decoded is not executable by the current runtime.

Therefore:

```text
Decode success
    =>
Cell payload is compatible with registered handler

Decode failure
    =>
Cell requires investigation
```

Additional validation rules are application-specific and are not part of the Cellar runtime.

## Non-goals

This ADR does not define:

- a CLI debugger;
- a web UI;
- Cell mutation operations;
- retry administration;
- Cell deletion tooling;
- payload migration;
- workflow visualisation.

These may be introduced separately.

## Consequences

### Positive

- Execution remains independent from debugging.
- Cell structure remains minimal.
- Existing codecs become the single source of truth for payload interpretation.
- Debugging cannot accidentally diverge from execution semantics.
- Applications can opt into richer inspection without increasing Handler complexity.

### Negative

- Generic registration requires internal type erasure.
- Debugging quality depends on codec and application support.
- Undecodable historical Cells require operational investigation.

## Cross references

- ADR-0002: Cell Lifecycle and Execution Model
- ADR-0005: Payload Encoding and Type Safety
