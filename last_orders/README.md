# Last Orders Backend Integration Prototype

This folder contains a disposable Go prototype used to validate architecture plumbing, not production Last Orders domain logic.

It demonstrates:

1. Firebase listener observation in read-only mode.
2. Scheduler -> Cellar -> Handler execution.
3. Handler-created future work.
4. Firebase event -> Cellar cell -> Handler.
5. Listener idempotency behaviour for duplicate deliveries.

## Scope and constraints

- No writes to historic Firebase application data.
- No Poll business processing implementation.
- No API/auth/UI work.
- Minimal abstractions focused on integration boundaries.

## Module and references

- Module: `last_orders`
- Uses local Cellar module via `replace cellar => ../cellar`
- Mirrors Cellar's public interfaces and handler model.

## Structure

- `cmd/last-orders-proto/main.go`: phase runner entry point.
- `internal/proto/app/app.go`: phase orchestration.
- `internal/proto/cellarruntime/registration.go`: typed registration helper for Cellar handlers.
- `internal/proto/firebase`: Firestore and simulated listener sources.
- `internal/proto/handlers`: test handlers with observable logging/metrics.
- `internal/proto/idempotency`: idempotency strategies (`none`, `memory`, `firestore`).

## Configuration

### Required for Firebase mode

Set either command flags or env:

- `-firebase=true`
- `-firebase-project=<project>` or `GOOGLE_CLOUD_PROJECT`
- `-firebase-credentials=<path>` or `GOOGLE_APPLICATION_CREDENTIALS` (optional when ADC is available)

Listener defaults:

- collection: `polls`
- event mapping:
  - `poll_created`: `polls where completed == false` on ADDED
  - `poll_modified`: `polls where completed == false` on MODIFIED
  - `poll_completed`: `polls where completed == true` on ADDED/MODIFIED
  - `poll_deleted`: `polls` on REMOVED

Storage defaults:

- in-memory store when `-sqlite-path` is empty
- durable SQLite store when `-sqlite-path=<path>` is provided

## Run

Install dependencies and run tests:

```powershell
go mod tidy
go test ./...
```

## Phase reference and examples

Phase 1: Firebase listener observation only.

```powershell
go run ./cmd/last-orders-proto -phase=1 -firebase=true -firebase-project=<project> -run-for=0
```

Phase 2: Housekeeping scheduler creates cells, Cellar executes handlers.

```powershell
go run ./cmd/last-orders-proto -phase=2 -run-for=20s -housekeeping-every=5s -housekeeping-lead=2s
```

Phase 3: Handler schedules future timed work.

```powershell
go run ./cmd/last-orders-proto -phase=3 -run-for=25s -chain-delay=10s
```

Phase 4: Firebase event to listener to Cellar to handler (no housekeeping loop).

```powershell
go run ./cmd/last-orders-proto -phase=4 -firebase=true -firebase-project=<project> -run-for=60s
```

Phase 5: Phase 4 flow plus listener idempotency (no housekeeping loop).

```powershell
go run ./cmd/last-orders-proto -phase=5 -firebase=true -firebase-project=<project> -idempotency=memory -run-for=60s
```

Phase 5 with listener-owned idempotency persisted in Firestore namespace:

```powershell
go run ./cmd/last-orders-proto -phase=5 -firebase=true -firebase-project=<project> -idempotency=firestore -run-for=60s
```

## Additional run examples

Run with durable Cellar storage (useful for restart experiments):

```powershell
go run ./cmd/last-orders-proto -phase=3 -sqlite-path=./last-orders-cells.db -run-for=25s
```

`idempotency=firestore` writes only to `listener_state/last_orders/events/*`.

## Observability

JSON structured logs show:

- `firebase event observed`
- `listener accepted firebase event`
- `cell created from firebase event`
- `cell claimed`
- `... handler executed`
- `cell executed`

Timed work includes scheduled and observed execution timestamps/skew.

## Failure tests included

`go test ./...` includes integration-oriented tests for:

1. Duplicate Firebase delivery before idempotency (duplicate downstream work expected).
2. Duplicate Firebase delivery with idempotency (duplicate filtered).
3. Handler retry semantics (`FailOnceHandler` returns `Retry` first, then `Complete`).
4. Restart around scheduled work (construct a new runtime over the same in-memory store instance).

## What this prototype proved

1. Firebase events can be observed and converted to a stable internal event shape.
2. Cellar cells can be scheduled, claimed, dispatched and completed by handlers.
3. Handlers can create future timed cells.
4. Firebase listener events can create Cellar work and trigger handlers end-to-end.
5. Listener-owned idempotency namespace is feasible and independent from historic `firebase_sub` schemas.

## Limitations and open questions

1. Tests still default to in-memory store for speed; durable restart semantics should be validated with `-sqlite-path` runs.
2. Firestore idempotency write mode is implemented but optional; default path remains read-only/no-write modes.

See `docs/cellar-findings.md` for additional integration notes.
