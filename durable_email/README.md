# Durable email delivery

This module will provide durable email submission and recovery using Cellar.
The current bootstrap establishes only the SQLite schema and a testbench proving
that it can share a caller-owned database with Cellar.

## Current package

The root `durableemail` package exposes `NewStore(*sql.DB)`. The caller owns the
database connection and its lifecycle. Initialisation creates:

- `email_requests`, containing immutable operation-wide request data;
- `email_progress`, containing recipient-specific request data and progress.

No email client, handler, sequence, recovery, or webhook behaviour is included
yet. The normative design is in `docs/overview.md`.

## Integration shape

The eventual `last_orders` wrapper will pass its shared `baseStore.DB()` to
`durableemail.NewStore` and register durable email handlers with the existing
Cellar runtime. The reusable module does not import application-internal store
types.

## Commands

```text
go test ./...
go build ./...
```
