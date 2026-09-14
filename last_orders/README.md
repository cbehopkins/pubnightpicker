# Last Orders Backend

Last Orders is a Firebase-backed application which responds to changes and external
events by producing Truths and executing durable work through
[Cellar](../cellar). See [docs/adr/0008-app-structure.md](docs/adr/0008-app-structure.md)
for the architecture this codebase follows.

## Module and references

- Module: `last_orders`
- Uses the local Cellar module via `replace cellar => ../cellar`

## Structure

- `cmd/last-orders/main.go`: process entry point.
- `internal/lastorders/app`: application composition root.
- `internal/lastorders/components`: reusable infrastructure
  (`firebaseidempotency`, `idempotency`, `recurrence`).
- `internal/lastorders/truths`: the catalogue of Truths (typed observations),
  their dispatch registries, and the durable envelope idempotency uses to
  carry a Truth to its Fanout. See [docs/adr/0009-truths.md](docs/adr/0009-truths.md)
  and [docs/adr/0010-truth-terminology.md](docs/adr/0010-truth-terminology.md).
- `internal/lastorders/database/listeners`: Firebase-specific listeners which
  convert database changes into Truths.
- `internal/lastorders/plugins`: connectivity between Truths and units of work.
- `internal/lastorders/basestore`: shared SQLite base store.

See `docs/adr/` for architecture decisions and `docs/cdd/` for component design
documents.

## Run

Install dependencies and run tests:

```powershell
go mod tidy
go test ./...
```

Run the application:

```powershell
go run ./cmd/last-orders -db-path=./last-orders.db
```

Set `FIRESTORE_EMULATOR_HOST` (and optionally `GOOGLE_CLOUD_PROJECT`) to enable
Firestore-backed listeners and idempotency instead of the in-memory stand-in.

See `docs/cellar-findings.md` for integration notes discovered while adopting Cellar.
