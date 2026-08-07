# ADR-0000: Project Goals and Philosophy

## Status

Draft

## Context

Cellar is a lightweight execution runtime for backend services.

It exists to provide durable, schedulable units of work without coupling business logic to infrastructure concerns.

Cellar is intended for small- to medium-scale systems where correctness, debuggability, and operational simplicity are valued more highly than raw throughput.

This document defines the goals and non-goals that guide the design of Cellar.

## Goals

### Simplicity over sophistication

Cellar should prefer simple designs that are easy to understand and reason about.

Additional complexity must justify its operational cost.

### Correctness over performance

Correctness and debuggability are more important than throughput.

Performance optimisations should only be introduced when they solve a demonstrated problem.

Cost efficiency, especially around Business Store operations, should be treated as a first-class design concern.

### Explicitness over magic

Behaviour should be visible in code and in persisted state.

Unexpected behaviour hidden inside the runtime should be avoided.

### Durable execution

Work must survive process restarts.

A process crash should not silently lose accepted work.

### Human debuggability

System state should be understandable using ordinary tools.

Developers should be able to inspect Cells and understand:

- what work exists;
- why it exists;
- what state it is in;
- when it will execute.

Human-readable formats are preferred.

### Separation of concerns

Business logic should be independent from execution infrastructure.

The runtime owns:

- persistence;
- scheduling;
- retries;
- recovery.

Applications own:

- business logic;
- payload definitions;
- payload serialisation format;
- retry policy.

Architectural and workflow complexity should be expressed in business-facing workflow design rather than hidden in runtime infrastructure.

The runtime should remain narrowly focused on Cells, scheduling, persistence, retries, and recovery.

### Single-process first

Cellar is initially designed for execution within a single trusted process.

The design should not prematurely optimise for distributed systems.

### Partition before distribution

Horizontal scaling should initially be achieved by partitioning responsibilities across services and backends.

Distributed coordination should only be introduced when partitioning is insufficient.

### Evolution without rewriting business logic

Changes to execution infrastructure should not require changes to business logic.

Future implementations may replace:

- SQLite;
- the scheduler;
- worker pools;
- transport mechanisms.

The concepts of Cell and Handler should remain stable.

## Non-goals

Cellar does not currently attempt to provide:

- exactly-once execution;
- distributed consensus;
- leases;
- workflow orchestration;
- automatic schema migration;
- automatic load balancing;
- competing consumers.

## Architectural assumptions

Cellar assumes:

- process restarts are normal;
- duplicate execution is acceptable;
- handlers are idempotent where necessary;
- Executor Store loss is an acceptable failure mode;
- payloads are small;
- workloads are relatively low volume.

## Guiding principle

Cellar is not a distributed system.

If Cellar becomes a distributed system, distributed-system techniques should be introduced at that time, rather than anticipated in advance.
