# Web Push Notification Contract

Status: active
Updated: 2026-04-16

## Scope

This contract defines web push behavior across the React app and the Python notifier.

- React client responsibilities:
  - permission request UX
  - service worker registration and push click handling
  - per-user endpoint registration lifecycle
- Python notifier responsibilities:
  - decide when to send (poll lifecycle listeners)
  - resolve recipients from active endpoint records
  - send push messages
  - keep poll-event idempotency via action keys

## Event Types

- poll_opened
- poll_completed
- poll_rescheduled

## Idempotency and Dedupe Keys

Dedupe is poll-event scoped, not endpoint scoped.

- Open key:
  - open:{pollId}
- Complete/reschedule key:
  - complete:{pollId}:{pubId}:{restaurantId}:{restaurantTime}

Notes:
- restaurantId and restaurantTime are empty strings when missing.
- poll_rescheduled is represented by a changed complete key for an already-actioned poll.
- Deduped action keys are stored in existing action docs and remain separate from endpoint storage.

## Action Namespace

Use a dedicated PUSH action namespace in notifier action tracking.

- existing namespaces remain unchanged (EMAIL, PEMAIL)
- new namespace: PUSH

## Firestore Endpoint Data Model

Collection path:
- users/{uid}/push_endpoints/{endpointId}

Ownership and access:
- user can create/update/delete only under their own uid
- user can read only their own endpoints
- notifier runtime can read endpoint docs (server/admin identity)

Recommended endpointId:
- deterministic hash of push endpoint URL and auth key (or endpoint URL only if auth key unavailable)

Document shape:
- endpoint: string (Web Push endpoint URL)
- p256dh: string (subscription key)
- auth: string (subscription auth secret)
- active: boolean
- createdAt: timestamp
- lastSeenAt: timestamp
- disabledAt: timestamp|null
- userAgent: string|null
- platform: string|null
- appVersion: string|null

Semantics:
- enable/refresh sets active=true and updates lastSeenAt
- disable sets active=false and disabledAt
- invalid endpoint response from push provider must deactivate endpoint

## Push Payload Shape

JSON payload sent by notifier:
- eventType: poll_opened|poll_completed|poll_rescheduled
- pollId: string
- title: string
- body: string
- url: string (deep link)
- tag: string (stable dedupe display tag)
- sentAt: ISO timestamp

Client behavior:
- service worker displays notification with title/body/tag
- clicking notification focuses existing app tab when available, otherwise opens URL

## Feature Flag

Frontend feature flag:
- VITE_ENABLE_WEB_PUSH=true|false

Backend feature flag:
- ENABLE_WEB_PUSH=true|false

If disabled, system must keep existing email behavior unchanged.

## Delivery and Failure Semantics

Notifier action marking behavior for PUSH:
- mark action key only when push send operation completes without retryable failure.
- partial failures are allowed:
  - successful endpoints are delivered
  - failed endpoints are reported/logged
  - invalid endpoints are deactivated
- retryable/system failures raise retry exception to avoid false idempotent success.

## Verification Expectations

- open event sends once per active endpoint for key open:{pollId}
- completed event sends once per active endpoint for current complete key
- changed restaurant/time (or selected venue) creates a new complete key and one additional send
- replay/reconnect of identical key sends nothing new
- disabling push prevents future sends to that endpoint
