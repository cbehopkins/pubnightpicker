# CDD: Handler Registry and Execution Binding

## Purpose

The Handler Registry is the Cellar component responsible for binding persisted Cell execution data to executable application code.

A persisted Cell contains:

```go
type Cell struct {
    ID          CellID
    HandlerName HandlerName
    Payload     []byte

    State       CellState
    NotBefore   *time.Time
}
```

The Registry provides the mapping:

```text
Persisted Cell

    HandlerName

        |

        v

Codec + Handler

        |

        v

Executable work
```

The Registry is the boundary between Cellar's persistence model and Go's strongly typed Handler model.

---

# Responsibilities

The Handler Registry is responsible for:

* registering Handlers;
* associating Handlers with their Codecs;
* providing execution bindings by HandlerName;
* validating that persisted Cells reference known Handlers during startup.

The Registry is not responsible for:

* executing scheduling;
* executing Handlers;
* persisting Cells;
* creating Cells;
* interpreting Handler results;
* decoding payloads outside of execution.

---

# HandlerName

Handler names are persisted identifiers.

Conceptually:

```go
type HandlerName string
```

A HandlerName identifies the Handler required to execute a Cell.

Handler names are not business identifiers.

They must be treated as persistent data.

Changing a HandlerName is a data migration, not a code refactor.

---

# Registration

Handlers are registered during Cellar construction.

Conceptually:

```go
func Register[T any](
    name HandlerName,
    codec Codec[T],
    handler Handler[T],
) error
```

A registration contains:

```text
HandlerName
      |
      +--> Codec[T]
      |
      +--> Handler[T]
```

The Codec and Handler are registered as a pair because they must agree on the payload type.

A mismatched Codec and Handler is a programming error.

---

# Registry Lifecycle

The Registry has two lifecycle phases.

## Construction Phase

During construction:

```text
Register
Register
Register
```

is permitted.

---

## Execution Phase

Before Cellar starts executing Cells:

```text
Freeze()
```

is performed.

After freezing:

* registrations cannot change;
* Handlers cannot be replaced;
* new Handlers cannot be added.

The Registry becomes immutable.

Conceptually:

```text
BUILD

Register()

    |

    v

FREEZE

    |

    v

EXECUTION

Lookup()
```

This ensures that the runtime cannot change its execution model while processing work.

---

# Duplicate Registration

HandlerName values must be unique.

Attempting:

```go
Register(
    "SendEmail",
    codecA,
    handlerA,
)

Register(
    "SendEmail",
    codecB,
    handlerB,
)
```

is an error.

Cellar must not silently replace registrations.

Duplicate registrations indicate a runtime configuration error.

---

# Execution Binding

The Registry hides generic type erasure from the runtime.

Internally, registrations may use a type-erased executor.

Conceptually:

```go
type Executor interface {
    Execute(
        ctx context.Context,
        payload []byte,
    ) Result
}
```

A generic registration adapts:

```go
Handler[T]
```

into:

```go
Executor
```

by performing:

```text
[]byte

    |

    v

Codec[T].Unmarshal()

    |

    v

T

    |

    v

Handler[T].Handle()
```

The Worker interacts with the Executor abstraction, not the underlying generic Handler type.

---

# Startup Validation

Before Cellar begins execution, persisted Cells are validated against the Registry.

The purpose is to ensure:

> Every persisted Cell references executable code.

The startup sequence is:

```text
Initialise Store

        |

Build Registry

        |

Freeze Registry

        |

List active Cells

        |

Validate HandlerName references

        |

Recover incomplete execution

        |

Start Scheduler
```

---

# Store Requirements

The Store must provide access to active Cells:

```go
ListActive() ([]Cell, error)
```

This operation is used for:

* startup validation;
* debugging;
* administration.

The Store does not interpret HandlerName values.

---

# Validation Scope

Startup validation checks:

```text
Cell.HandlerName exists in Registry
```

Startup validation does not check:

* payload decoding;
* payload semantic validity;
* Handler execution success;
* external system availability.

Those are execution concerns.

---

# Missing Handlers

If an active Cell references an unknown HandlerName:

```text
Cell:
    HandlerName = "SendEmail"

Registry:
    no SendEmail registration
```

Cellar startup fails.

The system does not begin scheduling work with known-invalid execution state.

Future versions may introduce quarantine handling for invalid Cells.

---

# Handler Versioning

V0 does not provide Handler versioning.

The current model is:

```text
HandlerName
      |
      v
Current implementation
```

Therefore, changing Handler behaviour affects existing persisted Cells.

Future versions may introduce:

* versioned Handler names;
* explicit Handler versions;
* migration tooling.

---

# Non-goals

The Handler Registry does not provide:

* dynamic Handler loading;
* runtime registration;
* Handler discovery;
* payload migration;
* business validation;
* execution history.

---

# Design Principles

The Registry follows these principles:

## Immutable runtime configuration

The execution model should not change while Cells are running.

## Explicit binding

Persisted execution requirements must map clearly to executable code.

## Strong typing at the boundary

Application code remains strongly typed.

Persistence remains type-agnostic.

## Fail early

Invalid runtime configuration should prevent startup rather than fail unpredictably during execution.

---

# Future Considerations

Future versions may add:

* Handler versioning;
* payload migration;
* richer validation;
* administrative tooling;
* dynamic registration.

These should preserve the core responsibility:

> Bind persisted Cell execution requests to executable application code.
