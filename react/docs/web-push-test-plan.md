# Web Push Test Plan

Status: draft for implementation
Updated: 2026-04-16

## Goals

Prove that web push notifications work across the React app and Python notifier without regressing existing email behavior.

Core behaviors to prove:
- poll opened sends once per active endpoint
- poll completed sends once per active endpoint
- poll rescheduled sends exactly one additional notification when the dedupe key changes
- replay/reconnect does not duplicate identical keys
- disabling push prevents future sends
- users cannot manage other users' push endpoints

## Frontend Unit Tests

Target files:
- `src/push/webPush.js`
- `src/hooks/useWebPushLifecycle.js`
- `src/hooks/useWebPushSettings.js`
- `src/components/pages/PreferencesForm.js`

### `src/push/webPush.test.js`
- reports unsupported status when browser APIs are unavailable
- enable flow requests permission and creates/updates `users/{uid}/push_endpoints/{endpointId}`
- enable flow sets `users/{uid}.webPushEnabled = true`
- enable flow fails cleanly when feature flag is disabled
- enable flow fails cleanly when permission is denied
- disable flow marks endpoint inactive and unsubscribes browser subscription
- disable flow sets `users/{uid}.webPushEnabled = false`
- touch flow refreshes `lastSeenAt` and reactivates endpoint
- touch flow falls back to upsert when endpoint doc is missing

### `src/hooks/useWebPushLifecycle.test.js`
- registers service worker on supported, feature-enabled browsers
- touches current endpoint on login/startup when uid exists
- deactivates previous endpoint on logout
- ignores service worker messages with unrelated payloads
- emits foreground notification info on `push-received`

### `src/hooks/useWebPushSettings.test.js`
- enable sets local enabled state on success
- enable exposes user-facing error on failure
- disable clears local enabled state on success
- disable exposes user-facing error on failure

### `src/components/pages/Preferences.test.js` or `PreferencesForm.test.js`
- renders push controls when feature flag is enabled
- shows unsupported message when browser lacks required APIs
- shows permission status and busy state during enable/disable
- clicking enable/disable delegates to hook callbacks

## Service Worker Tests

Optional light-weight test approach without Playwright:
- unit test extracted notification-click helper logic if refactored
- manual browser verification for actual service worker push event handling

Manual service worker checks:
- push payload shows notification with expected title/body/tag
- click focuses existing app tab when already open on matching route
- click opens deep link when no tab is open

## Firestore Rules Tests

Target file:
- `firestore.rules`

Recommended harness:
- `@firebase/rules-unit-testing` with Firestore emulator

### `src/dbtools/push_endpoints.permissions.test.js`
- self create allowed at `users/{uid}/push_endpoints/{endpointId}`
- self update allowed
- self delete allowed
- cross-user create denied
- cross-user update denied
- cross-user delete denied
- self read allowed
- cross-user read denied
- notifier/admin read path allowed as designed
- existing `users` and `user-public` access patterns remain unchanged

## Python Unit Tests

Target files:
- notifier push contract module
- push sender adapter
- action binding and action tracking integration

### `firebase_sub/tests/test_push_contract.py`
- open key builder returns `open:{pollId}`
- complete key builder returns `complete:{pollId}:{pubId}:{restaurantId}:{restaurantTime}`
- missing restaurant/time normalize to empty strings

### `firebase_sub/tests/test_push_bindings.py`
- open notifier binds `ActionType.PUSH`
- complete notifier binds `ActionType.PUSH` alongside existing email actions

### `firebase_sub/tests/test_push_sender.py`
- builds `poll_opened` payload correctly
- builds `poll_completed` payload correctly
- uses `poll_rescheduled` when `previously_actioned=True`
- deactivates invalid endpoints on provider invalid response
- raises retry exception on retryable provider failure
- does not mark false full success on partial failures

## Python Integration Tests

Target file:
- `firebase_sub/tests/integration/test_handlers_integration.py`

Add coverage for:
- open handler persists push action key when callback succeeds
- complete handler persists push action key when callback succeeds
- replay of same key does not invoke callback again
- changed restaurant/time produces a new key and one extra callback invocation
- stale endpoint records are deactivated when sender reports invalid subscription
- existing email action docs still work unchanged

## Manual End-to-End Checks

Use local frontend + Firestore/Auth emulator + Python notifier process.

### Scenario 1: Poll opened
- enable push for test user
- create/open poll
- verify one push notification arrives
- verify notifier action doc records push open key
- trigger listener replay/restart and verify no duplicate

### Scenario 2: Poll completed
- complete poll
- verify one push notification arrives
- verify action doc records current complete key
- replay listener and verify no duplicate

### Scenario 3: Poll rescheduled
- change selected venue or restaurant/time
- verify exactly one additional push notification arrives
- verify new complete key exists in action tracking

### Scenario 4: Disable push
- disable push in preferences
- trigger subsequent open/complete event
- verify no push arrives for disabled endpoint

## Acceptance Evidence

Collect evidence as:
- unit/integration test pass output
- screenshots of preferences enable/disable states
- screenshots or logs of delivered browser notifications
- Firestore snapshots for endpoint docs and action docs
- notifier logs showing delivery/deactivation/failure counts
