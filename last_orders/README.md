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

Go 1.27.3 or newer is required, including standard-library security fixes.
With `GOTOOLCHAIN=auto`, Go selects the required version from `go.mod`.

```powershell
go mod tidy
go test ./...
```

Install Staticcheck from a temporary module (the same versions are used in CI).
Run this in a separate PowerShell session:

```powershell
$toolDir = Join-Path ([System.IO.Path]::GetTempPath()) ("staticcheck-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $toolDir | Out-Null
try {
    $env:GOTOOLCHAIN = "go1.27.3"
    $env:GOWORK = "off"
    go -C $toolDir mod init staticcheck-build
    if ($LASTEXITCODE -ne 0) { throw "Failed to initialize Staticcheck build module" }
    go -C $toolDir get honnef.co/go/tools/cmd/staticcheck@v0.8.1 golang.org/x/tools@v0.51.0
    if ($LASTEXITCODE -ne 0) { throw "Failed to resolve Staticcheck dependencies" }
    go -C $toolDir install honnef.co/go/tools/cmd/staticcheck
    if ($LASTEXITCODE -ne 0) { throw "Failed to install Staticcheck" }
} finally {
    Remove-Item -LiteralPath $toolDir -Recurse -Force
}
staticcheck ./...
```

The temporary build pins Staticcheck v0.8.1 with x/tools v0.51.0 to support the
Go 1.27.3 export format; installing Staticcheck v0.8.1 directly with `@v0.8.1`
uses an older, incompatible x/tools version. The executable is installed into
`GOBIN` (or `GOPATH\bin` by default); no tooling module remains in the repository.

Run the application:

```powershell
go run ./cmd/last-orders -db-path=./last-orders.db
```

The application always connects to Firestore. For local development set
`FIRESTORE_EMULATOR_HOST` (and optionally `GOOGLE_CLOUD_PROJECT`). For production
leave `FIRESTORE_EMULATOR_HOST` unset. Credentials can be supplied in either of
these ways:

- Set `LAST_ORDERS_FIREBASE_CREDENTIALS_JSON` to the complete contents of a Firebase
  service-account JSON file, as a runtime secret environment variable.
- Leave that variable unset to use Google's Application Default Credentials,
  including `GOOGLE_APPLICATION_CREDENTIALS` pointing at a service-account JSON
  file (e.g. the same `cred.json` used by `firebase_sub`).

Setting both the JSON variable and a non-empty `GOOGLE_APPLICATION_CREDENTIALS`
is an error. An explicitly empty JSON variable, malformed JSON, a non-service-account
credential, missing required fields or an invalid RSA private key rejects startup;
the application does not fall back to other credentials. Errors do not include the
credential contents. The supplied credentials are shared by Firestore, API token
verification and optional admin Auth deletion. Emulator clients retain their
existing unauthenticated behaviour and Auth emulator safety checks.

`GOOGLE_CLOUD_PROJECT` overrides the Firestore project; otherwise production uses
the credential's project ID. Auth uses `FIREBASE_AUTH_PROJECT_ID` when set, then
`GOOGLE_CLOUD_PROJECT`, then the credential's project ID. Firestore emulator mode
retains its `last-orders-emulator` default project.

Production Firestore gRPC connections use IPv4 only by default to work around the
reported Nexaa IPv6 network issue. Set `LAST_ORDERS_FIRESTORE_FORCE_IPV4=false`
and restart the container to restore the SDK's normal IPv4/IPv6 selection when
testing the host's fix; no image rebuild is required. Set it to `true` or remove
the variable to restore the workaround. Empty or invalid boolean values reject
startup. Disabling the workaround permits IPv6 but does not guarantee it will be
selected. Credentials, TLS hostname verification and SDK retry behaviour are
unchanged; emulator connections retain their existing transport behaviour.

### Docker hosts with environment-only secrets

For hosts such as Nexaa where a secret file mount is not available on the chosen
tier, configure `LAST_ORDERS_FIREBASE_CREDENTIALS_JSON` in the service's runtime
environment with the full service-account JSON as its secret value. Leave
`GOOGLE_APPLICATION_CREDENTIALS`, `FIRESTORE_EMULATOR_HOST` and
`FIREBASE_AUTH_EMULATOR_HOST` unset for production, then redeploy/restart the service.

Paste the JSON itself, without surrounding shell quotes or base64 encoding. It may
be formatted across lines or compacted to one line using a JSON-aware tool. Keep
the standard `\n` escapes inside the `private_key` JSON string; do not replace them
with literal line breaks inside that string or double-escape them.

The application reads credentials in memory: no credential file, shell entrypoint
or Dockerfile change is required. Keep the SQLite database on the persistent
`/data` volume independently. Never put credentials in the image, Docker build
arguments, frontend variables, committed configuration or logs. Runtime environment
secrets remain accessible to anyone with sufficient container/platform privileges;
restrict access and rotate the service-account key if exposed.

### Release deployment

The `last_orders image` GitHub Actions workflow builds and pushes Last Orders
images to GHCR. Pushing a tag such as `last_orders-v1.2.3` also deploys that
version to the configured Scaleway Serverless Container after the image push
succeeds. The deploy job updates the container's image reference and requests a
redeploy; it does not run for branch builds or other tags.

Configure these GitHub repository settings for the deploy job:

- Secret `SCW_SECRET_KEY`: a Scaleway API key with permission to update and
  redeploy the target Serverless Container.
- Variable `SCW_CONTAINERS_REGION`: the container region, such as `fr-par`.
- Variable `SCW_CONTAINER_ID`: the target container UUID.

The image is addressed as
`ghcr.io/<owner>/<repository>/last_orders:last_orders-v1.2.3`. Ensure the
Scaleway container can pull that GHCR image. The container ID and region are
configuration, not secrets; keep the API key in the GitHub secret only.

Email uses the dummy client by default. Set `MAILTRAP_TOKEN` to enable Mailtrap,
or provide both `SWEEGO_TOKEN` and `SWEEGO_PROVIDER` to enable Sweego;
`SWEEGO_BASE_URL` is optional and defaults to `https://api.sweego.io`. Setting
both `MAILTRAP_TOKEN` and `SWEEGO_TOKEN` is an error.

### Reset a corrupted local database

For an explicitly destructive reset, set `LAST_ORDERS_RESET_DB=true` or pass
`-reset-db`. Before opening SQLite, the process deletes the configured `-db-path`
and its `-wal`, `-shm` and `-journal` sidecars. Missing files are harmless;
deletion errors stop startup. The reset is disabled by default and never happens
automatically in response to corruption. Firestore data is not deleted.

This discards all local state, including queued durable work, timers, delivery
recovery records, idempotency state and cached projections. Firestore projections
can rebuild, but pending work is not guaranteed to be recovered and external
actions may be replayed when local deduplication/recovery state is lost.

Stop every replica using this database before resetting it. Do not use a rolling
restart against a shared database that another replica still has open. If the
platform cannot provide exclusive access, ask support to stop the other replicas
or reset the volume during maintenance. This option does not solve shared-volume
SQLite corruption or coordinate replicas.

Remove the flag or environment variable immediately after recovery: while enabled,
it deletes the database on every process start, including health-check restarts.
No shell or filesystem access inside the container is required.

### Container health checks

Configure the cloud service's HTTP probe to use `GET /healthz` on the application
port (default `8080`). It requires no authentication and returns `200 OK` with
the plain-text body `ok` and `Cache-Control: no-store`. `HEAD` is also supported.
Successful probes are not logged and do not create application work or query
Firestore, SQLite, email or push providers.

HTTP serving begins only after the initial notification and venue projections
are ready and the application listeners have started. A successful probe confirms
that startup has passed those gates and HTTP is responsive, not that background
work is progressing or external dependencies are currently available. The route
is unavailable when HTTP is disabled with an empty `-http-addr`.

Local projection persistence failures stop the application with an error, both
during startup and after readiness. They are not retried indefinitely as upstream
watch interruptions are. This does not make network-backed SQLite WAL storage safe.

Start with a 30-second probe interval, a 3-second timeout, three consecutive
failures before marking the container unhealthy, and a 60-second startup grace
period. Adjust the grace period to observed startup times. The image is
distroless and has no shell or `curl`; use a platform-managed HTTP probe rather
than a shell-based Docker `HEALTHCHECK`.

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
