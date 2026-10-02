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

The application always connects to Firestore. For local development set
`FIRESTORE_EMULATOR_HOST` (and optionally `GOOGLE_CLOUD_PROJECT`). For production
leave `FIRESTORE_EMULATOR_HOST` unset and point `GOOGLE_APPLICATION_CREDENTIALS`
at a service-account JSON (e.g. the same `cred.json` used by `firebase_sub`); the
project ID is read from that file unless `GOOGLE_CLOUD_PROJECT` is set.

Email uses the dummy client by default. To enable Sweego, provide both
`SWEEGO_TOKEN` and `SWEEGO_PROVIDER`; `SWEEGO_BASE_URL` is optional and defaults
to `https://api.sweego.io`.

## Authenticated frontend API

`POST /api/ping` accepts a Firebase ID token, logs the verified UID and responds
synchronously without Firestore transport or Cellar work. Configure
`FIREBASE_AUTH_PROJECT_ID` to match the frontend and `LAST_ORDERS_ALLOWED_ORIGINS`
with the deployed frontend's exact HTTPS origin. Auth initialisation is skipped
when `-http-addr` is empty.

Optionally set `LAST_ORDERS_ALLOWED_PREVIEW_SITES=pubnightpicker` to allow HTTPS
Firebase Hosting preview origins such as `pubnightpicker--pr141-abc.web.app`.
This does not allow unrelated Hosting sites or bypass Firebase authentication.
Permanent origins remain configured through `LAST_ORDERS_ALLOWED_ORIGINS`.

For local emulator development use HTTP port 8081 and explicitly opt in with
`-allow-auth-emulator`; Firestore already uses 8080. Auth emulator acceptance is
not permitted implicitly. The existing `/log` route remains unauthenticated.

See [docs/authenticated-api.md](docs/authenticated-api.md) for the API contract,
local commands, security boundaries and deployment requirements. This API is
implemented only in Last Orders, not the Python backend.

See `docs/cellar-findings.md` for integration notes discovered while adopting Cellar.
