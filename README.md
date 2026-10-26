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
| [`e2e/`](e2e/) | End-to-end harness for the frontend and Python notification worker. | [E2E README](e2e/README.md) |
| [`schema/firestore/`](schema/firestore/) | Firestore schema notes and references. | [Firestore schema README](schema/firestore/README.md) |

The Go projects are separate modules. `last_orders` uses the local `cellar`, `durable_email`, and `email_clients` modules via Go `replace` directives. Read the nearest project `README.md` and `AGENTS.md` before making project-specific changes.

## Prerequisites

- Node.js 18+ and npm
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
go run ./cmd/last-orders '-http-addr=127.0.0.1:8081' -allow-auth-emulator
```

The frontend defaults to the API at `http://localhost:8081`. The emulator opt-in flag is for local development only; never use emulator settings with production credentials or services.

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
- Run the narrowest relevant checks for your change. The root [`run-tests.ps1`](run-tests.ps1) provides `-Node`, `-Typecheck`, `-Lint`, `-Unit`, `-Integration`, and `-E2E` options; project READMEs document Go commands and additional checks.
- Integration and E2E runners manage emulator lifecycles and seed test data. Check whether emulators are already in use before running them.
- Never commit `.env` files, credentials, tokens, emulator data exports, or local database files.

## Project documentation

- [React frontend and Firebase emulator workflow](react/README.md)
- [Last Orders Go backend](last_orders/README.md) and [authenticated API local setup](last_orders/docs/authenticated-api.md)
- [Python notifier and operations runbooks](firebase_sub/README.md)
- [Cellar durable execution library](cellar/README.md)
- [Durable email library](durable_email/README.md)
- [Email provider clients](email_clients/README.md)
- [Frontend and notifier E2E harness](e2e/README.md)
