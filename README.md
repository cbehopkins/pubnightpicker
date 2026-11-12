# Pub Night Picker

Pub Night Picker is a multi-project application for organising pub nights. The web client uses Firebase Authentication and Firestore. Backend services listen for application events, send notifications, and run durable background work.

This README is the contributor's starting point: it maps the repository and gets the web app running locally against Firebase emulators. Each project README has the detailed architecture and project-specific commands.

## Repository map

| Directory | What it contains | Start here |
| --- | --- | --- |
| [`react/`](react/) | Vite + React web application, Firebase rules and indexes, Hosting config, and local emulator data when available. | [Frontend README](react/README.md) |
| [`last_orders/`](last_orders/) | Go backend for Firebase listeners, application services, the authenticated frontend API, and durable work. | [Backend README](last_orders/README.md) |
| [`firebase_sub/`](firebase_sub/) | Python worker for notifications, housekeeping, and Firebase administration tools. | [Worker README](firebase_sub/README.md) |
| [`cellar/`](cellar/) | Go library for durable, scheduled, retryable background work. | [Cellar README](cellar/README.md) |
| [`durable_email/`](durable_email/) | Go library for durable email submission, recovery, and delivery tracking, built on Cellar. | [Durable email README](durable_email/README.md) |
| [`email_clients/`](email_clients/) | Go provider-neutral email interfaces and provider clients, including Sweego and Mailtrap. | [Email clients README](email_clients/README.md) |
| [`e2e/`](e2e/) | React browser journeys with fresh Firebase emulators and isolated Last Orders backend coverage. | [E2E README](e2e/README.md) |
| [`schema/firestore/`](schema/firestore/) | Firestore schema notes and references. | [Firestore schema README](schema/firestore/README.md) |

The Go projects are separate modules. `last_orders` uses the local `cellar`, `durable_email`, and `email_clients` modules via Go `replace` directives. Read the nearest project `README.md` and `AGENTS.md` before making project-specific changes.

The browser suite is documented in [the E2E README](e2e/README.md). Its default
command and `./run-tests.ps1 -E2E` run frontend-only and Last Orders groups in
separate fresh Firebase emulator lifetimes. Python supplies the bootstrap CLI,
not a notifier backend; Last Orders uses temporary SQLite state and dummy
delivery only.

## Prerequisites

- Node.js 18+ and npm for React development; Node.js 20+ for the Cucumber/Playwright E2E journey
- Firebase CLI (`npm install -g firebase-tools`)
- Java, required by the Firebase emulators
- Python 3.12+ and Poetry, for the Python worker and its tests
- Go 1.27.3+, for the Go modules; `GOTOOLCHAIN=auto` can select the version specified by each module's `go.mod`

## Local web development

The web app connects to the Auth and Firestore emulators automatically when opened on localhost. The emulator ports are Auth `9099`, Firestore `8080`, Hosting `5000`, and Emulator UI `4000`. Local development does not need production Firebase credentials.

### 1. Install frontend dependencies

From the repository root:

```powershell
cd react
npm ci
```

If you do not already have a local env file, copy the example:

```powershell
Copy-Item .env.example .env
```

The Vite Firebase settings in `react/.env` must use the same Firebase project ID as the emulator. Keep secrets and service-account credentials out of frontend env files. The frontend uses the emulators on localhost unless `VITE_USE_FIREBASE_EMULATORS="false"` is set; do not disable emulator mode for ordinary local development.

### 2. Start Firebase emulators

Open a terminal in `react/` and run:

```powershell
npx firebase-tools emulators:start --import=./db_dir
```

If you do not have a local `db_dir` snapshot yet, start with empty emulator data instead:

```powershell
npx firebase-tools emulators:start
```

Importing `react/db_dir` lets you use its existing test accounts and data. The emulators use the project configured by `react/.firebaserc` (`pubnightpicker` by default).

Keep this terminal running. The emulator data is local; do not point the app at production to work around missing local accounts. See the [frontend README](react/README.md) for emulator export/import details and authentication troubleshooting.

### 3. Start the frontend

In another terminal:

```powershell
cd react
npm run dev
```

Open `http://localhost:3000`. Sign in using an account in the imported emulator data, or create an account in the Auth emulator. Some admin screens require the local account to have the appropriate role in emulator Firestore.

## Optional backends

You only need to start a backend when working on its behavior. For regular UI work, the Firebase emulators and Vite app are enough.

### Last Orders Go API and listeners

This backend provides the API used by the frontend diagnostics page and runs Firebase listeners and durable work. Follow the [Last Orders local development guide](last_orders/docs/authenticated-api.md) for the full emulator setup. With the Auth and Firestore emulators already running, open a terminal in `last_orders/` and set its emulator configuration:

```powershell
$env:GOOGLE_CLOUD_PROJECT = "pubnightpicker"
$env:FIREBASE_AUTH_PROJECT_ID = "pubnightpicker"
$env:FIRESTORE_EMULATOR_HOST = "127.0.0.1:8080"
$env:FIREBASE_AUTH_EMULATOR_HOST = "127.0.0.1:9099"
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$env:LAST_ORDERS_CURSOR_HMAC_KEY = [Convert]::ToBase64String($bytes).TrimEnd([char]'=').Replace('+', '-').Replace('/', '_')
$rng.Dispose()
go run ./cmd/last-orders '-http-addr=127.0.0.1:8081' -allow-auth-emulator
```

The cursor key signs opaque Venue pagination and stream cursors; it is not an API
authentication credential. Keep the generated value stable while testing across
backend restarts, and never commit or reuse it as a production secret. The
frontend defaults to the API at `http://localhost:8081`. The emulator opt-in flag
is for local development only; never use emulator settings with production
credentials or services.

### Python notification worker

The Python worker is usually run and tested separately. Install its dependencies and point it at the running local emulator from `firebase_sub/`:

```powershell
poetry install
$env:FIRESTORE_EMULATOR_HOST = "127.0.0.1:8080"
$env:GOOGLE_CLOUD_PROJECT = "pubnightpicker"
poetry run python -m firebase_sub.cli.sub_events --dummy-email --dummy-push --loglevel info
```

The dummy modes prevent real email and push delivery. See the [worker README](firebase_sub/README.md) for other runtime options and runbooks. Do not start the worker against a live Firebase project with real delivery enabled.

## Contributing

- Keep changes focused in the relevant project; update its README or design docs when behavior or setup changes.
- Check for a nearer `AGENTS.md` and follow the project's formatting and architecture conventions.
- Run the narrowest relevant checks for your change. The root [`run-tests.ps1`](run-tests.ps1) provides `-Node`, `-Typecheck`, `-Lint`, `-Unit`, `-Integration`, `-E2E`, and `-Go` options. `-Go` runs `go test ./...` in `cellar`, `durable_email`, `email_clients`, and `last_orders`; `-VerboseOutput` adds `-v`. With no options, the runner runs integration and Go tests; `-All` includes Go tests too. Project READMEs document additional checks.
- Integration and E2E runners manage emulator lifecycles and seed test data. Check whether emulators are already in use before running them.
- Never commit `.env` files, credentials, tokens, emulator data exports, or local database files.

## Production Releases

Merging to `main` no longer deploys the frontend or publishes the Last Orders
image. Same-repository PRs continue to build and deploy Firebase Hosting previews.
Backend PR/main CI continues to run independently of image publication.

Push a release tag on a commit that belongs to `main`:

| Tag | Result |
|-----|--------|
| `release-v1.2.3` | Deploy production frontend, Firestore rules/indexes, and publish Last Orders image |
| `frontend-v1.2.3` | Deploy production frontend and Firestore rules/indexes only |
| `last_orders-v1.2.3` | Publish Last Orders image only |
| `v1.2.3` | Legacy backend image alias; also triggers the separate Python notifier image workflow |

Versions must contain three non-negative integers without leading zeroes,
prerelease suffixes or build metadata. Matching prefixes with invalid versions
fail validation before deployment/publication. There is no manual workflow
dispatch bypass for these two release workflows. The Python notifier workflow
is unchanged and can still publish images independently.

For example, after all required PR/main checks have passed for the chosen commit:

```bash
git fetch origin main
git tag -a release-v1.2.3 origin/main -m "Release 1.2.3"
git push origin release-v1.2.3
```

Frontend releases run the existing build/typecheck gates; backend image releases
run vet and tests for all four Go modules. Tag validation checks ancestry, not
whether every independent CI workflow has passed. Do not tag an unverified commit.
Protect `main` with required CI checks, and use GitHub tag rulesets to restrict
creation, updates and deletion of release tags. Never move or reuse a release tag.
Configure the GitHub `production` environment with required reviewers and tag
deployment restrictions if approval is required. Frontend production deployments
are serialised and are not cancelled by subsequent releases.

A shared tag starts two independent workflows, not an atomic deployment. The
frontend can become live before the backend image is ready or activated. Use it
only for backwards-compatible changes. For breaking changes, prepare Firestore
indexes first and wait for them to become ready, then publish and activate a
backwards-compatible backend using a component tag before releasing its frontend.
Rules deployed with the frontend must also support the previous frontend while
existing browser sessions remain open.

### Scaleway Container Registry

Last Orders release images are now published to Scaleway Container Registry,
not GHCR. Existing GHCR images and the Python notifier image workflow are unchanged.
Before pushing a backend release tag:

1. Create a private Scaleway Container Registry namespace in the deployment region.
2. Set the GitHub Actions repository variable `SCALEWAY_REGISTRY_ENDPOINT` to the
	namespace endpoint, for example `rg.fr-par.scw.cloud/pubnightpicker`, without
	`https://` or a trailing slash. Copy the actual endpoint from Scaleway.
3. Create a dedicated Scaleway IAM application with permission to push images to
	that registry, scoped as narrowly as possible. Store its API secret key in the
	GitHub Actions secret `SCALEWAY_API_KEY`, not in source control or chat. Docker
	login uses the literal username `nologin`; the access key ID is not needed.

The workflow rejects missing or malformed configuration before building. Images
retain the release tag and long commit-SHA tag; deployment should pin the digest.
Publishing an image does not change the running Serverless Container.

### Backend Version Diagnostics

The admin diagnostics page shows the frontend build, the intended backend and
the backend responding to the public `GET /api/version` endpoint. A match means
the release tag and source commit agree; it does not prove that the image
digest, health or every instance in a rolling deployment matches. The page
distinguishes an unreachable backend from a connected development build.
Local development reports the Git commit and modified state when available:
the frontend captures them when Vite starts, and the backend when it is built.
These identify the base commit, not the contents of uncommitted changes. A
missing local deployment expectation is normal and the comparison is unknown.

The intended backend is stored in the `config/deployment` Firestore document
for the Firebase project. Before activating or rolling back a published image,
fetch the release tag and use the updater from the repository root:

```powershell
git fetch origin --tags
node react/scripts/set-deployment-expectation.mjs --project pubnightpicker --tag last_orders-v1.2.3
node react/scripts/set-deployment-expectation.mjs --project pubnightpicker --tag last_orders-v1.2.3 --apply
```

The first command is a dry run. The second requires Google Application Default
Credentials with Firestore write access; set `GOOGLE_APPLICATION_CREDENTIALS`
or use an authenticated local ADC account. The updater verifies that the local
and origin tags resolve to the same commit, but does not check the registry or
activate the image. Select the corresponding published image by digest in
Scaleway, then refresh diagnostics. Write the new expectation before activation
so a failed deployment remains visible as a mismatch. Future deployment
automation should perform this step as part of activation rather than on image
publication. The endpoint and source identifiers are public; Firestore reads
and the diagnostics page are restricted to admins.

### Backend Deployment Considerations

Backend activation remains manual. The current host is Scaleway Serverless
Containers, whose local filesystem is ephemeral. A `/data` directory in an image
is not a persistent volume. For Firebase-backed Truths, Firebase is the durable
deduplication authority; the local idempotency store is a cache and processing
marker. After local state is lost, an existing Firebase key prevents the Truth
from being emitted again. Losing this cache alone does not replay completed work.

Losing SQLite can instead discard queued work, timers and recovery state. A Truth
already claimed in Firebase can remain suppressed even if its unfinished local
work has been lost. Rebuilding Firestore projections does not restore that work.
At the current scale, this recovery-state loss is an accepted operational risk;
persistent storage is not a prerequisite for deployment automation. Platform
replacement can also lose local state outside planned quiet-period restarts.

Scaleway also uses rolling deployments, during which old and new workers can
overlap. A maximum scale of one is not proof of no overlap across revisions.
The existing `/healthz` probe checks startup and HTTP responsiveness, not database
durability or exclusive worker ownership.

Before automating activation, verify a stop-before-start procedure through the
Scaleway API, retaining an approval gate to schedule quiet-period releases. If
durability requirements increase, options include a single Scaleway Instance with
persistent block storage and one Docker worker, or a separately designed change
to the application's storage/worker coordination suitable for serverless hosting.
Do not substitute an object-storage or network-filesystem mount for SQLite WAL
storage without validating its locking and filesystem semantics.

After those prerequisites are resolved, select the published
`<SCALEWAY_REGISTRY_ENDPOINT>/last_orders:<release-tag>` image (prefer its digest),
verify the old worker has stopped before starting its replacement, and retain the
existing `GET /healthz` probe plus a functional smoke test. Record the previous
image digest and back up durable state where available before deployment. Do not
enable database reset during a routine release.

See Scaleway's [ephemeral storage and rolling update documentation](https://www.scaleway.com/en/docs/serverless-containers/concepts/)
and [GitHub Actions registry integration guide](https://www.scaleway.com/en/docs/tutorials/use-container-registry-github-actions/).

For rollback, redeploy the recorded backend digest or restore the previous Firebase
Hosting release through Firebase's release history. Hosting rollback does not
restore Firestore rules/indexes or backend state; manage those separately and
check database compatibility before rolling back a backend image.

PR previews still use the configured Firebase project and backend URL; they are
not isolated staging environments. Do not use them for destructive production-data
testing. A separate staging project/backend needs separate resources and credentials.

## Web Push Notifications (Poll Lifecycle)

Web push is implemented across both subprojects:

- Frontend handles permission prompts, service worker lifecycle, and endpoint registration.
- Python notifier decides when to send for poll opened, poll completed, and poll rescheduled.
- Existing email notifications remain in place.

### 1. Generate VAPID Keys

Use a standard web-push key generator (for example):

```bash
npx web-push generate-vapid-keys
```

This outputs:

- Public Key
- Private Key

### 2. Configure Frontend Env

In `react/.env` set:

```properties
VITE_ENABLE_WEB_PUSH="true"
VITE_WEB_PUSH_PUBLIC_KEY="<Public Key>"
```

Optional:

```properties
VITE_APP_VERSION="local-dev"
```

Restart the frontend dev server after env changes.

For production hosting, build the frontend with the production value of `VITE_WEB_PUSH_PUBLIC_KEY` (Vite embeds this at build time). Firebase Hosting serves the built assets, so deploy a build created with the correct environment variables.

### 3. Configure Python Notifier Env

When running the notifier (`firebase_sub`), set:

```properties
ENABLE_WEB_PUSH=true
WEB_PUSH_VAPID_PRIVATE_KEY=<Private Key>
WEB_PUSH_VAPID_SUBJECT=mailto:<your-email@example.com>
PUBNIGHTPICKER_WEB_BASE_URL=http://localhost:3000
```

### 4. Run Locally

Frontend:

```bash
cd react
npm run dev
```

Notifier (Poetry):

```bash
cd firebase_sub
poetry install
poetry run python -m firebase_sub.cli.sub_events --loglevel info
```

### 5. Verify in App

- Open Preferences in the web app.
- Enable Web Push Notifications.
- Trigger poll-open, poll-complete, and reschedule flows.

For detailed verification commands and expected behavior, see:

- `react/docs/web-push-test-plan.md`
- `react/docs/web-push-verification-summary.md`
- `react/docs/web-push-local-testing.md`

## Integration Tests

The Python notifier has an emulator-backed integration test suite. A single top-level
command manages the full lifecycle (start emulator, run pytest, stop emulator):

```bash
python run_integration_tests.py
```

**Prerequisites (one-time)**

| Tool | Install |
|------|---------|
| Java (JDK 11+) | https://adoptium.net |
| firebase-tools | `npm install -g firebase-tools` |
| Python deps | `cd firebase_sub && poetry install` |

Optional flags:

```bash
# Also run unit tests before integration tests
python run_integration_tests.py --unit

# Forward extra arguments to pytest (e.g. run only bootstrap tests)
python run_integration_tests.py -k bootstrap
```

To run integration tests without the top-level runner (requires emulator already started):

```bash
cd firebase_sub
tox -e integration
```

See `firebase_sub/README.md` for bootstrap CLI docs (create-admin, seed-smoke, grant-role).

## Dependency maintenance

Use the separate [dependency maintenance script](update-dependencies.ps1) from the repository root:

```powershell
./update-dependencies.ps1                            # Report available updates
./update-dependencies.ps1 -Go                         # Report Go updates only
./update-dependencies.ps1 -Mode Update -Apply          # Upgrade within existing constraints
./update-dependencies.ps1 -Mode Tidy -Apply            # Tidy without requesting upgrades
```

Select any combination of `-Go`, `-Node`, and `-Python`; omitting selectors includes all projects. Modifying modes require `-Apply` and a clean Git working tree, including untracked files. Failures are reported per project and produce a non-zero exit code; successful changes are not rolled back.

| Mode | Go modules | npm projects (`react`, `e2e`) | Poetry (`firebase_sub`) |
| --- | --- | --- | --- |
| `Check` (default) | `go list -mod=readonly -m -u all` | `npm outdated` | `poetry show --outdated` |
| `Update` | `go get -u ./...` | `npm update --ignore-scripts` | `poetry update` |
| `Tidy` | `go mod tidy` | `npm prune --ignore-scripts` | `poetry sync --no-root` |

Go updates stay within existing module paths; npm and Poetry updates honour manifest constraints. Major-version migrations and globally installed or transient `npx` tools are not upgraded automatically. Check mode contacts registries and may populate tool caches (Go may also add missing checksum entries), but does not request manifest or lockfile upgrades.

The script runs npm commands through `npm exec --yes --package=npm@11.6.2 -- npm ...`, using a pinned resolver without replacing the globally installed npm. This avoids the `edgesOut` peer-resolution crash reproduced with npm 10.8.2 and 10.9.4. The first run may download npm into its cache. npm 11.6.2 requires Node `^20.17.0 || >=22.9.0`; individual dependencies may require a newer Node version.

Tidy reconciles Go dependency metadata, removes extraneous npm packages, and synchronises the Python environment with its existing lockfile; these are not equivalent operations. Updates do not automatically run tidy or tests. npm lifecycle scripts are disabled during maintenance; the updater explicitly installs the matching Playwright Chromium browser after E2E updates. Other dependencies that need installation scripts may need an explicit rebuild afterwards. Review manifest and lockfile changes, then run the relevant checks using [the test runner](run-tests.ps1).

## Project documentation

- [React frontend and Firebase emulator workflow](react/README.md)
- [Last Orders Go backend](last_orders/README.md) and [authenticated API local setup](last_orders/docs/authenticated-api.md)
- [Python notifier and operations runbooks](firebase_sub/README.md)
- [Cellar durable execution library](cellar/README.md)
- [Durable email library](durable_email/README.md)
- [Email provider clients](email_clients/README.md)
- [Frontend E2E browser journeys](e2e/README.md)
