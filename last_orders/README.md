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

Go 1.27.2 or newer is required, including standard-library security fixes.
With `GOTOOLCHAIN=auto`, Go selects the required version from `go.mod`.

```powershell
go mod tidy
go test ./...
```

Run Staticcheck using the pinned tooling module (also used in CI):

```powershell
go -C tools\staticcheck install honnef.co/go/tools/cmd/staticcheck
staticcheck ./...
```

The tooling module pins Staticcheck v0.8.1 with x/tools v0.51.0 to support the
Go 1.27.2 export format; installing Staticcheck v0.8.1 directly with `@v0.8.1`
uses an older, incompatible x/tools version. If your installed Go is older,
set `GOTOOLCHAIN=go1.27.2` when installing the checker so it is built with the
backend's toolchain.

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

## Admin auth deletion

Admin auth deletion is disabled unless `ENABLE_ADMIN_DELETE_REQUESTS=true`.
When enabled, the normal Firestore listener handles pending requests. Dry-run
defaults to true; set `ADMIN_DELETE_DRY_RUN=false` and pass
`-enable-real-auth-delete` to permit the backend to call Firebase Auth. Real
deletion is never enabled by the one-shot evaluation option alone.

For a one-shot dry-run evaluation, set the enablement variables and use an
isolated Cellar database:

```powershell
$env:ENABLE_ADMIN_DELETE_REQUESTS = "true"
$env:ADMIN_DELETE_DRY_RUN = "true"
go run ./cmd/last-orders -db-path=./admin-delete-test.db -admin-delete-evaluate-once -http-addr=
```

The command scans pending requests once, dispatches them through normal
idempotency and service Cells, waits for terminal outcomes to be persisted, and
then exits. If the kill switch is paused, it enqueues nothing and exits with
requests still pending. Use `-run-for` to bound a test run if a retryable
dependency failure prevents requests from reaching terminal status. For a
dry-run followed by real deletion, reuse the same request: when
`ADMIN_DELETE_DRY_RUN=false` and `-enable-real-auth-delete` are both set,
`dry_run_validated` requests are also selected in both one-shot and normal
listener modes. The backend rechecks all safety preconditions, uses a separate
real-delete idempotency phase, preserves the dry-run audit as `dryRunEvidence`,
and waits for the real outcome rather than stopping at the prior validation.
For a destructive emulator test, additionally configure `FIREBASE_AUTH_EMULATOR_HOST`,
set `ADMIN_DELETE_DRY_RUN=false`, and pass both `-allow-auth-emulator` and
`-enable-real-auth-delete`. See
[`docs/cdd/0003-admin-delete.md`](docs/cdd/0003-admin-delete.md) for the full
request, safety-gate, and persistence contract.

## Runtime notification suppression

Admins can toggle **Silence notifications** on Diagnostics. The GUI writes
`config/diagnostics.SilenceNotifications`, a global boolean watched by Last Orders.
An absent document or field defaults to false. Invalid values or initial read
failures defer email and push submission; later watch failures retain the last known value
and retry. In-memory settings are thread-safe and selected once per attempt.

Two optional booleans in the same document default to false:

- `NotifyPollActorWhenSilenced`: permits live poll-opening email/push to the human
  creator and initial completion email/push to the human completer only.
- `KeepChatNotificationsWhenSilenced`: permits normal global/event-chat pushes.

They are ignored when silence is off. Actor delivery respects existing personal
preferences, addresses and active endpoints; chat preserves membership, mute and
author-exclusion rules. Mailing-list emails and diagnostic tests remain silenced.
Live exceptions consume normal delivery quota and retain provider failure/recovery
behaviour. Actors have distinct personal email operations, preventing duplicate
personal delivery when the setting changes. Opening and completion actors may
be different users, and all eligible devices belonging to the actor are included.

React writes `createdByUid` with initial poll creation and `completedByUid` with
human completion. Rules bind these to the authenticated actor and keep creator
attribution immutable. Rescheduling and automatic completion clear completion
attribution. Legacy/automatic polls with no human actor and rescheduled completion
notifications have no actor exception. Attribution is carried in the durable
Truth, rather than inferred from the best-effort audit trail.

When enabled, Sweego uses dry-run, Mailtrap uses its sandbox and dummy sends remain
dummy. Suppressed emails include test emails and bypass the poll-email delivery
allowance. The separate 10-per-day test-email request throttle is unchanged.

Push suppression covers notifications not permitted by those exceptions. Each eligible endpoint
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
feature. Deploy rules first, the metadata-aware backend second and the actor-writing
GUI last. Enable exceptions only after all active Last Orders instances support
them. Durable email automatically adds a JSON metadata column to existing SQLite
databases; old requests default to no exception. No Firestore backfill, initial
configuration document or new index is required. Completed suppressed work is
never replayed by enabling an exception.

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
