# Web Push Local Testing

## Frontend env (`react/.env`)
Create with `npx web-push generate-vapid-keys`

Set these values for local testing:

- `VITE_ENABLE_WEB_PUSH="true"`
- `VITE_WEB_PUSH_PUBLIC_KEY="<your-vapid-public-key>"`
- `VITE_USE_FIREBASE_EMULATORS="true"` (recommended for local rules/data testing)

Optional:

- `VITE_APP_VERSION="local-dev"`

## Notifier env (`firebase_sub` process)

Set these values before starting the Python notifier:

- `ENABLE_WEB_PUSH=true`
- `WEB_PUSH_VAPID_PRIVATE_KEY=<your-vapid-private-key>`
- `WEB_PUSH_VAPID_SUBJECT=mailto:<contact-email>`
- `PUBNIGHTPICKER_WEB_BASE_URL=http://localhost:3000` (or deployed URL)

## Start commands

### React

From `react/`:

```bash
npm run dev
```

### Python notifier (Poetry)

From `firebase_sub/`:

```bash
poetry install
poetry run python -m firebase_sub.cli.sub_events --loglevel info
```

## Verify lifecycle quickly

1. Open Preferences and click Enable Push.
2. Confirm a doc appears at `users/{uid}/push_endpoints/{endpointId}` with `active=true`.
3. Trigger a poll-open event and confirm a browser notification arrives once.
4. Complete a poll and confirm a completion notification arrives once.
5. Reschedule (change selected venue or restaurant/time) and confirm one additional notification.
6. Disable Push in Preferences and verify `active=false` and no further notifications.

## Endpoint cleanup behavior

The notifier automatically deactivates bad or stale endpoint docs to avoid repeated failed notifications:

- Missing subscription keys (`p256dh` or `auth`): endpoint is immediately marked inactive.
- Stale subscriptions (`404`/`410` from push service): endpoint is marked inactive.
- Non-retryable auth/permission failures (`400`/`401`/`403`): endpoint is marked inactive.

Retryable delivery failures still surface as retryable errors and are not auto-disabled.

## Test commands

### Frontend push + rules tests

From `react/`:

```bash
npm run test -- src/hooks/useWebPushSettings.test.js src/hooks/useWebPushLifecycle.test.js src/App.test.js src/dbtools/push_endpoints.permissions.test.js --run
```

### Python notifier tests (Poetry)

From `firebase_sub/`:

```bash
poetry run pytest tests -k "not integration"
```
