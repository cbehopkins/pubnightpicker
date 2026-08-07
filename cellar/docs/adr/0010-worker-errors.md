# ADR-0010: Cell Execution Failure and Idempotency Model

## Status

Draft

## Context

Cellar executes units of work represented by Cells.

Execution may involve:

* local computation;
* persistence operations;
* calls to external systems;
* interactions with APIs outside Cellar's control.

Cellar supports recovery by allowing unfinished Cells to return to READY after process restart.

This means a Cell may execute more than once.

Additionally, failures may occur after an external system has accepted a request but before Cellar has recorded the result.

Therefore, Cell execution cannot provide exactly-once semantics across external systems.

---

## Decision

Cellar provides **reliable execution of retryable units of work**, not exactly-once execution.

Handlers must be designed to tolerate:

* duplicate execution;
* process interruption;
* uncertain external outcomes;
* retries.

A Cell represents a unit of work whose execution semantics are acceptable under these conditions.

---

## Responsibility Boundaries

Cellar is responsible for:

* persisting Cell state;
* scheduling execution;
* recovering incomplete work;
* invoking Handlers;
* applying Handler Results.

Cellar is not responsible for:

* understanding external systems;
* determining whether a retry is safe;
* deduplicating business operations;
* providing distributed transactions.

Handlers are responsible for understanding the semantics of the operations they perform.

---

## External Side Effects

A Handler may interact with external systems.

Example:

```text
Cell
 |
 v
Handler
 |
 v
External API call
```

The following situation is possible:

```text
External API accepts request

        |

Cellar crashes before recording completion
```

After recovery, Cellar may execute the Cell again.

The Handler must determine how to handle this situation.

Possible approaches include:

* using idempotency keys;
* checking external state before mutation;
* storing intermediate state;
* splitting work into multiple Cells.

---

## Cell Design Principle

Cells should represent idempotent units of work.

The boundary of a Cell should be chosen around recovery semantics, not merely around code structure.

A large operation containing multiple external mutations is discouraged.

For example:

```text
Update database
Send email
Record webhook
```

may be better represented as:

```text
Create delivery request Cell

        |

Send email Cell

        |

Process webhook Cell
```

where each Cell has clearer retry semantics.

---

## Context Cancellation

Cellar passes a context to Handlers.

Context cancellation is advisory.

Handlers may choose to complete a short-running operation after cancellation if doing so improves correctness.

Examples:

* waiting for an in-flight API request;
* completing a transaction;
* cleaning up resources.

Cellar does not enforce Handler cancellation deadlines.

---

## Failure Categories

Failures are divided into categories.

### Handler-level failures

Examples:

* external API unavailable;
* transient network failure;
* business validation failure.

Handlers determine the appropriate Result:

* retry;
* abandon;
* succeed.

---

### Cellar failures

Examples:

* scheduler bug;
* Store corruption;
* runtime panic.

These are Cellar implementation failures.

Cellar process termination is an acceptable response to runtime bugs.

---

## Non-goals

Cellar does not provide:

* exactly-once execution;
* distributed transactions;
* automatic external side-effect reconciliation;
* universal duplicate prevention.

---

## Consequences

### Positive

* Cellar remains simple and reliable.
* Application-specific failure handling remains with application code.
* External system complexity does not leak into the runtime.
* Recovery semantics remain predictable.

### Negative

* Handler authors must understand idempotency.
* Some integrations require additional application design.
* Duplicate execution remains possible.

## Cross references

* ADR-0002: Cell Lifecycle and Execution Model
* ADR-0005: Payload Encoding and Type Safety
* ADR-0008: Cell Lifecycle Notices
* ADR-0009: Cell Identity and Allocation
