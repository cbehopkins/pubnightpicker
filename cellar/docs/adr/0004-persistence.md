# ADR-0004: Persistence Guarantees

## Status

Draft

## Context

The system intentionally optimises for simplicity and low Firestore usage rather than perfect durability.

## Design Decisions

- Creation of a Cell transfers responsibility from the Listener to the runtime.
- Executor state survives process restarts.
- Executor state is not required to survive deployments.
- Loss of executor state after acceptance may result in permanent loss of work.
- This trade-off is considered acceptable.

## Non-goals

The system does not guarantee:

- exactly-once execution
- at-least-once execution
- recovery after executor-store loss
- workflow continuity across deployments

## Rationale

The cost of occasional lost notifications is considered lower than:

- additional Firestore traffic
- increased architectural complexity
- distributed coordination
- stronger durability guarantees

## Recovery Guarantees

The runtime guarantees recovery from process termination.

The runtime does not guarantee recovery from Executor Store corruption or loss.

Recovery may result in duplicate execution.

Duplicate execution is considered an acceptable trade-off.
