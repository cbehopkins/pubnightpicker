# ADR-0003: Services and Workflows

## Status

Draft

## Context

Services and Workflows exist to aid reasoning about the system architecture.

The runtime itself only understands Cells.

## Service

A Service is a logical grouping of backend functionality.

Examples:

- Poll Open Workflow
    - Poll Open Mailing List Email Service
    - Poll Open Personal Email Service
    - Poll Open Push Notification Service

Services are documentation, deployment and ownership concepts.

For example:

```text
PollOpened
    -> Poll Open Listener
    -> Poll Open Mailing List Service
```

Ownership matters because:

- only one backend owns a Listener
- services are partition boundaries
- services are scaling boundaries

Services are not runtime primitives.

## Workflow

A Workflow describes how multiple Services cooperate to satisfy a business requirement.

Example:

```text
PollOpened
    -> Poll Open Mailing List Email Service
    -> Poll Open Personal Email Service
    -> Poll Open Push Notification Service
```

The runtime never executes Workflows directly.

Workflows are represented by collections of Cells created by Listeners and Handlers.

Cross-cutting architectural philosophy is defined in ADR-0000.

## Open Questions

- Should Services map one-to-one with Listeners?
- Can multiple Services share a Listener?
- Should Services own Ledgers?
