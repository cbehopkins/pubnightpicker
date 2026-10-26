# Repository guidance

This is a multi-project repository. Before changing code, read the nearest project README and any nested `AGENTS.md`; keep changes within the relevant project unless the task requires cross-project work.

## Projects and local checks

- `react/`: Vite + React. Start the frontend with `npm run dev` from `react/` (Vite is configured for `http://localhost:3000`). Use `npm test -- --run`, `npm run typecheck`, and `npm run lint` for the corresponding checks.
- `firebase_sub/`: Python 3.12+ package managed with Poetry. Install dependencies with `poetry install`; run unit tests with `poetry run pytest -m "not integration"`. The repo-level `run_integration_tests.py` starts and stops Firestore/Auth emulators and runs integration tests.
- Go modules: `cellar/`, `durable_email/`, `email_clients/`, and `last_orders/`. Work from the relevant module directory; use `go test ./...` or `go build ./...` as appropriate. `GOTOOLCHAIN=auto` is enabled; Go can select/download the version required by a module's `go.mod` (verified here: `last_orders` selected Go 1.27.3 even though the installed base Go is 1.24.2).
- `e2e/`: Node frontend client plus Python orchestrator; see `e2e/README.md`. The E2E runner manages emulator processes and seeds smoke-test data.

The PowerShell convenience runner is `./run-tests.ps1`; it supports `-Unit`, `-Integration`, `-E2E`, `-Node`, `-Tox`, `-Black`, `-Typecheck`, `-Lint`, and `-All`. Prefer the narrowest relevant check. Do not run test suites unless the user asks for testing or verification.

## Firebase and data safety

- Use Firebase emulators and demo project IDs for local development and tests. The React emulator database import is `react/db_dir`; do not overwrite or export emulator data unless the task calls for it.
- Integration/E2E runners start and stop emulators; the integration runner also force-stops processes listening on its known emulator ports. Check for existing emulator work before invoking those runners.
- Never point local scripts at production Firebase or use real credentials for tests. Do not print, commit, or copy `.env`, `.env.test`, service-account files, tokens, or other secrets. Treat frontend Firebase configuration as public app configuration, but keep credentials and private keys out of frontend env files.
- `npm run check:firestore-indexes` queries the configured live Firebase project; only run it when the user explicitly asks for a live index check and the target project is clear.
- Avoid deploy, restore, backup, database mutation, real email/push delivery, and admin deletion operations unless the user explicitly requests that specific operation.

## Local development services

- Frontend: `cd react; npm run dev`.
- React Firebase emulators: from `react/`, `npx firebase-tools emulators:start --import=./db_dir`; Firestore uses port 8080 and Auth uses 9099 per `react/firebase.json`. Emulator UI uses its Firebase CLI default port.
- The separate notifier integration/E2E configuration uses Firestore 8180 and Auth 9199. Consult its runner/config before starting it; these ports and the React emulator ports are distinct.
- Local development uses env files. Inspect variable names or templates when needed, but do not expose secret values in output.

## Repository notes

- `run_integration_tests.py` copies canonical Firestore rules and indexes from `react/` into `firebase_sub/` before starting integration tests.
- Root README and project docs are the source of truth for setup details; update the relevant docs when a change alters a workflow or behavior.
