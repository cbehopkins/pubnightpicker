# ADR-0005: Payload Encoding and Type Safety

## Status

Draft

## Context

Cells must survive process restarts.

Therefore, all information required to execute a Cell must be persisted in the Executor Store or be statically available in the runtime.

Each Cell contains:

- the identity of the Handler to execute;
- the payload required by that Handler.

The runtime must map persisted Cells back into strongly typed Go values.

## Decision

The Cell runtime persists payloads as opaque byte sequences.

Conceptually:

```go
type Cell struct {
    ID          CellID
    HandlerName string
    Payload     []byte

    State       CellState
    NotBefore   *time.Time
}
```

Cellar itself is agnostic to the encoding format.

Applications are responsible for selecting an appropriate codec.

### Codec interface

Conceptually:

```go
type Codec[T any] interface {
    Marshal(T) ([]byte, error)
    Unmarshal([]byte) (T, error)
}
```

### Handler registration

Handlers are registered together with their payload codec.

Conceptually:

```go
func Register[T any](
    name string,
    codec Codec[T],
    handler Handler[T],
)
```

### Handler interface

Conceptually:

```go
type Handler[T any] interface {
    Handle(
        ctx context.Context,
        payload T,
    ) Result
}
```

The runtime is responsible for:

- locating the registered Handler;
- locating the associated Codec;
- decoding the payload;
- invoking the Handler.

### Default codec

Cellar provides a JSON codec:

```go
cellar.JSONCodec[T]()
```

JSON is the recommended default because:

- it is human-readable;
- it is easy to debug;
- it is widely understood;
- performance is sufficient for the intended workload.

Applications may supply alternative codecs.

Examples:

- protobuf;
- gob;
- MessagePack;
- CBOR;
- custom binary formats.

### Storage model

Payloads are stored directly in the cells table.

A separate payload table is intentionally not part of the initial design.

A separate payload store may be introduced in the future if payloads become:

- large;
- shared;
- independently versioned.

Cross-cutting architectural philosophy is defined in ADR-0000.
