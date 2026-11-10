# Pubnightpicker Monorepo

This repository contains two subprojects for the Pub Night Picker system:

- `react/`: Vite + React web application
- `firebase_sub/`: Python notifier/worker tooling (email + web push + housekeeping)

## Quick Start

1. Generate a VAPID key pair:

```bash
npx web-push generate-vapid-keys
```

2. Set frontend env in `react/.env`:

```properties
VITE_ENABLE_WEB_PUSH="true"
VITE_WEB_PUSH_PUBLIC_KEY="<Public Key>"
```

3. Set notifier env before starting `firebase_sub`:

```properties
ENABLE_WEB_PUSH=true
WEB_PUSH_VAPID_PRIVATE_KEY=<Private Key>
WEB_PUSH_VAPID_SUBJECT=mailto:<your-email@example.com>
PUBNIGHTPICKER_WEB_BASE_URL=http://localhost:3000
```

4. Run both apps:

```bash
cd react
npm run dev
```

```bash
cd firebase_sub
poetry install
poetry run python -m firebase_sub.cli.sub_events --loglevel info
```

5. In the web app Preferences page, enable Web Push Notifications.

## Project Structure

- `react/README.md`: Frontend setup, Firebase emulator usage, and deployment notes.
- `firebase_sub/README.md`: Python notifier setup, Docker runbook, and diagnostics.

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

The Python notifier has an emulator-backed integration test suite.  A single top-level
command manages the full lifecycle (start emulator → run pytest → stop emulator):

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
