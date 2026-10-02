# ADR-0007: Offline Cell Administration

## Status

Draft

## Context

Cellar owns the lifecycle of executing Cells.

During normal operation, Cell state transitions are controlled by the runtime:

READY -> CLAIMED -> DELETED

and:

CLAIMED -> READY

during recovery.

Operational debugging sometimes requires examining or modifying persisted Cells directly.

Examples include:

- inspecting persisted Cells;
- correcting corrupted payloads;
- removing unwanted Cells;
- adjusting scheduling information;
- recovering from operator mistakes or application bugs.

Providing these capabilities through a live runtime API introduces additional complexity:

- coordination with schedulers;
- coordination with workers;
- concurrent modification;
- transaction semantics;
- authentication and authorization;
- audit requirements.

These concerns are intentionally deferred.

## Decision

Cell administration in V0 is an offline operation.

Administrative tools operate directly against the Cell Store while Cellar is not running.

Administrative operations are not part of the Cell lifecycle model and may modify persisted state in ways that would not be possible during normal execution.

Such modifications are considered privileged interventions.

Operators are responsible for maintaining consistency when performing administrative changes.

## Consequences

### Positive

- The runtime remains simple.
- Scheduler and worker correctness are unaffected by administrative operations.
- Debugging tools can be powerful without requiring a runtime control plane.
- The Store remains inspectable and recoverable.

### Negative

- Administrative tooling cannot safely modify a running Cellar instance.
- Operators must stop Cellar before making changes.
- There is no built-in audit trail for administrative modifications.

## Future Work

A future version may introduce live administration.

Such a system would require explicit support for:

- pausing scheduling;
- coordinating with workers;
- safe mutation of running state;
- authentication;
- authorization;
- auditing.

This ADR intentionally does not define that system.
