# Web Push Verification Summary

Updated: 2026-04-16

## Scope verified

- Frontend push lifecycle wiring and preferences controls
- Firestore push endpoint ownership rules
- Python push sender integration, dedupe key behavior, and callback bindings

## Automated test evidence

### Frontend

Command:

- `npm run test -- src/hooks/useWebPushSettings.test.js src/hooks/useWebPushLifecycle.test.js src/App.test.js src/dbtools/push_endpoints.permissions.test.js --run`

Result:

- 4 test files passed
- 13 tests passed

Coverage highlights:

- lifecycle boot, foreground message handling, logout endpoint deactivation
- settings enable/disable state handling and errors
- app startup wiring for push lifecycle
- Firestore rules: self write allowed, cross-user write denied, admin read allowed

### Python unit tests (Poetry)

Command:

- `poetry run pytest tests -k "not integration"`

Result:

- 61 passed
- 5 deselected

Coverage highlights:

- push key contract (`open:{pollId}`, `complete:{pollId}:{pubId}:{restaurantId}:{restaurantTime}`)
- push payload generation for opened/completed/rescheduled semantics
- push callback bindings in notifier action manager
- retryable failure handling in push sender

### Python integration tests (Poetry + emulator)

Command:

- `poetry run pytest tests/integration/test_handlers_integration.py` with:
  - `FIRESTORE_EMULATOR_HOST=127.0.0.1:8080`
  - `GOOGLE_CLOUD_PROJECT=demo-firebase-sub-integration`

Result:

- 9 passed

Coverage highlights:

- open handler uses canonical `open:` action key and persists action doc updates
- complete handler uses canonical `complete:` action key and persists action doc updates
- active push endpoint query returns only active endpoint records
- active push endpoint query excludes users with disabled or missing `webPushEnabled`

### Python push sender cleanup behavior

Command:

- `poetry run pytest tests/test_send_push.py`

Result:

- 7 passed

Coverage highlights:

- malformed endpoint records (missing key material) are deactivated immediately
- malformed endpoint records are counted as invalid, not retried

## Event-type verification matrix

- poll_opened:
  - Covered by key contract tests, open payload tests, open action-key integration test
- poll_completed:
  - Covered by complete payload tests and complete action-key integration test
- poll_rescheduled:
  - Covered by reschedule semantics in unit tests (`previously_actioned=True` maps to `poll_rescheduled`)

## Acceptance criteria status (automated)

- Per-user push enable/disable lifecycle in frontend: satisfied by hook and wiring tests
- Ownership controls for push endpoint records: satisfied by Firestore rules tests
- Replay-safe keying semantics for open/complete/reschedule: satisfied by key and integration tests
- Existing email flow retained: existing email unit tests continue passing in non-integration Python suite

## Remaining manual checks

Manual browser-delivery checks are still recommended for final rollout confidence:

1. Poll opened sends once per active endpoint in a real browser session
2. Poll completed sends once per active endpoint in a real browser session
3. Reschedule with changed restaurant/time produces exactly one additional browser notification
4. Replay/restart does not duplicate notifications for identical keys
5. Disable push prevents future sends
