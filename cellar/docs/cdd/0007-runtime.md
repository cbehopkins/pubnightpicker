# CDD: Cellar Runtime

## Purpose

The Runtime is the coordinating component of Cellar.

It owns the lifecycle of the Cellar process and coordinates the interaction between:

* Store;
* Scheduler;
* Workers;
* Handler Registry;
* Notice subsystem.

The Runtime is the composition and coordination layer.

It does not execute application logic.

---

# Responsibilities

The Runtime is responsible for:

* constructing and connecting Cellar components;
* registering typed application Handlers by stable HandlerName;
* adapting typed Handler payloads to and from JSON;
* controlling startup ordering;
* controlling shutdown ordering;
* providing execution context;
* interpreting Handler Results;
* applying execution outcomes through the Store;
* handling fatal runtime failures.

The Runtime is not responsible for:

* scheduling Cells;
* executing Handlers;
* persistence implementation;
* business retry decisions.

---

# Relationship to Other Components

The high-level architecture is:

```text
                         Cellar Runtime

        +--------------------+--------------------+

        |                    |                    |

        v                    v                    v


     Scheduler            Workers             Registry


        |                    |

        v                    v


      Store              Handlers


        |

        v

   Persistence
```

The Runtime owns the connections between components.

---

# Startup Lifecycle

Cellar startup proceeds in a defined order.

Conceptually:

```text
Runtime.Start()

    |

    v

Validate Registry

    |

    v

Store.Recover()

    |

    v

Start Scheduler

    |

    v

Start Workers
```

The ordering is intentional.

Recovery must complete before scheduling begins.

Otherwise previously claimed Cells may not become available for execution.

---

# Registry Validation

During startup, the Runtime validates persisted Cells.

Conceptually:

```text
For each active Cell:

    Does HandlerName exist?

        yes -> continue

        no -> startup failure
```

Payload validation is not performed during startup.

Payload validity is determined during execution.

Registration becomes immutable when startup succeeds. Attempts to register another
Handler after that transition fail through the Runtime API; applications never freeze
the Registry directly.

---

# Payload Encoding

The Runtime JSON-encodes typed values passed to `Add` and persists the bytes in
`Cell.Payload`. During execution, the Handler registration JSON-decodes those bytes
into the Handler's declared payload type before invocation.

JSON is the fixed payload contract. Codec selection is not an application concern.

---

# Result Application

The Runtime is responsible for interpreting Handler Results.

The execution path is:

```text
Worker

    |

    v

Result

    |

    v

Runtime

    |

    v

Store operation
```

Example:

```go
switch result := result.(type) {

case Complete:
    store.Complete(
        cell.ID,
        result.NewCells,
    )

case Retry:
    store.Retry(
        cell.ID,
        result.NotBefore,
    )
}
```

The Runtime understands Cellar semantics.

It does not understand application semantics.

---

# Shutdown Lifecycle

The Runtime owns orderly shutdown.

Conceptually:

```text
Signal received

        |

        v

Cancel root context

        |

        v

Stop Scheduler

        |

        v

Workers stop accepting new Cells

        |

        v

Existing Handlers receive cancellation

        |

        v

Store.Close()
```

Cellar does not forcibly terminate Handler execution.

Handlers decide how to respond to cancellation.

---

# Context Management

The Runtime creates the root execution context.

This context is propagated:

```text
Runtime

    |

    v

Worker

    |

    v

Handler
```

Cancellation may occur due to:

* shutdown;
* future operational controls.

Cancellation is advisory.

---

# Fatal Error Handling

V0 treats certain failures as fatal runtime errors.

Examples:

* Store lifecycle failures;
* invalid runtime invariants;
* unhandled Handler panics.

The Runtime does not attempt to continue with uncertain state.

The process exits and normal recovery occurs on restart.

---

# Non-goals

The Runtime does not provide:

* business workflow management;
* external transaction coordination;
* Handler isolation;
* retry policy;
* scheduling algorithms.

---

# Future Considerations

Future versions may introduce:

* runtime maintenance mode;
* live debugging controls;
* improved failure isolation;
* dynamic worker management;
* richer operational controls.

These should not change the core responsibility:

> The Runtime coordinates Cellar components and translates execution outcomes into persisted lifecycle transitions.
