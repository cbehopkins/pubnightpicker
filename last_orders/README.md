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

Email uses the dummy client by default. Set `MAILTRAP_TOKEN` to enable Mailtrap,
or provide both `SWEEGO_TOKEN` and `SWEEGO_PROVIDER` to enable Sweego;
`SWEEGO_BASE_URL` is optional and defaults to `https://api.sweego.io`. Setting
both `MAILTRAP_TOKEN` and `SWEEGO_TOKEN` is an error.

## Runtime notification suppression

Admins can toggle **Silence notifications** on Diagnostics. The GUI writes
`config/diagnostics.SilenceNotifications`, a global boolean watched by Last Orders.
An absent document or field defaults to false. Invalid values or initial read
failures defer email and push submission; later watch failures retain the last known value
and retry. In-memory settings are thread-safe and selected once per attempt.

When enabled, Sweego uses dry-run, Mailtrap uses its sandbox and dummy sends remain
dummy. Suppressed emails include test emails and bypass the poll-email delivery
allowance. The separate 10-per-day test-email request throttle is unchanged.

Push suppression covers poll, chat and diagnostic pushes. Each eligible endpoint
is marked handled using the existing `Accepted` state atomically with delivery
cell completion, without calling the push service, consuming push tokens or
invalidating endpoints. Follow-up actions and test-push acknowledgements still
complete; `Accepted` and acknowledgements can therefore mean suppressed handling,
not actual delivery. Completed suppressed pushes are not replayed when the switch
is cleared. Existing live delivery and retry behaviour is unchanged.

Optional Mailtrap settings:

- `MAILTRAP_SANDBOX_ID`: positive integer sandbox inbox ID.
- `MAILTRAP_SANDBOX_TOKEN`: sandbox token; defaults to `MAILTRAP_TOKEN` when an
  inbox ID is configured. Credentials are never stored in Firestore.

Missing sandbox configuration, provider refusals and other suppressed-send
failures are logged and treated as handled without live fallback. Durable email
commits synthetic `suppressed:` acceptance records before the best-effort call,
so a crash may lose the sandbox send but cannot replay it live on restart. These
records and test-email acknowledgements do not prove delivery. Database failures
still prevent sending and are not ignored. Normal live recovery is unchanged.

The switch requires manual clearing and does not cancel already-selected live
sends. The old Python backend remains active. A GUI save confirms the
Firestore write, not observation by every backend instance. Deploy canonical
Firestore rules, updated Last Orders and then the GUI before relying on this
feature. No initial configuration document, migration or index is required.

## Delivery rate limits

Poll emails share `email.send`, defaulting to 100 recipient attempts per day.
All push notifications share `push.send`, defaulting to 1,000 endpoint attempts
per day. Configure positive integer capacities with
`LAST_ORDERS_EMAIL_DAILY_LIMIT` and `LAST_ORDERS_PUSH_DAILY_LIMIT`; explicitly
empty, zero, negative or non-integer values reject startup. A mailing-list address
counts as one recipient, not the number of subscribers behind it.

Tokens reset at midnight in the application's timezone. Bulk email acquisition
is all-or-nothing; failed acquisitions consume nothing. Each provider retry
requires new tokens for the recipients still pending. Poll emails wait until the
daily reset when insufficient tokens remain. A batch larger than the daily
capacity logs an error and retries in 24 hours, remaining queued until its
capacity issue is resolved. Batches are not split. Mailtrap's separate maximum of
500 recipients per submission still applies, even if the daily capacity is higher.

Push delivery waits until reset or notification expiry, whichever comes first.
Provider recovery queries are not limited. Diagnostics email retains its separate
10-per-day admission allowance and existing drop-without-ack behaviour; its
submission retries do not consume poll email tokens.

Cellar persists deferred work and scheduling in SQLite, but token counts are
local process state: a restart restores the allowances, and separate processes
have independent limits. These are operational guards, not global quota or
security enforcement. See [docs/cdd/0010-rate-limiting.md](docs/cdd/0010-rate-limiting.md).

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
