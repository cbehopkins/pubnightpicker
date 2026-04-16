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
