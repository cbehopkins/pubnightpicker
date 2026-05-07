# Web Push Notification Contract

Status: active
Updated: 2026-05-29

## Scope

This contract defines web push behavior across the React app and the Python notifier.

- React client responsibilities:
  - permission request UX
  - service worker registration and push click handling
  - per-user endpoint registration lifecycle
  - per-type push preference storage (`users/{uid}.pushPreferences`)
- Python notifier responsibilities:
  - decide when to send (poll lifecycle listeners + chat message listeners)
  - resolve recipients from active endpoint records filtered by per-type preference
  - send push messages
  - keep poll-event idempotency via action keys
  - keep chat message idempotency via `chat_push_actions` collection

## Event Types

- poll_opened
- poll_completed
- poll_rescheduled
- chat_message_sent_global
- chat_message_sent_event

## Idempotency and Dedupe Keys

### Poll events
Dedupe is poll-event scoped, not endpoint scoped.

- Open key:
  - open:{pollId}
- Complete/reschedule key:
  - complete:{pollId}:{pubId}:{restaurantId}:{restaurantTime}

Notes:
- restaurantId and restaurantTime are empty strings when missing.
- poll_rescheduled is represented by a changed complete key for an already-actioned poll.
- Deduped action keys are stored in existing action docs and remain separate from endpoint storage.

### Chat messages
Dedupe is per-message, tracked in `chat_push_actions/{messageId}`.

- A doc is created when a message triggers its first push batch.
- `notified` array records uids that have already received a push for that message.
- Endpoints added after the initial batch are not retroactively notified.

## User Push Preferences

Stored on the private user document `users/{uid}` under the `pushPreferences` map.

Fields:
- pollOpens: boolean (default true when absent)
- pollCompletes: boolean (default true when absent)
- globalChat: boolean (default false when absent)
- eventChat: boolean (default false when absent)

Migration default: if `pushPreferences` is missing from a user doc, treat as
`{ pollOpens: true, pollCompletes: true, globalChat: false, eventChat: false }`.

The master switch `users/{uid}.webPushEnabled` must also be true; preferences are
only consulted when the master switch is on.

The four fields correspond to these event types:
- pollOpens → poll_opened
- pollCompletes → poll_completed, poll_rescheduled
- globalChat → chat_message_sent_global
- eventChat → chat_message_sent_event

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

## Chat Push Actions Collection

Collection path: `chat_push_actions/{messageId}`

Access: written exclusively by the Python notifier via admin SDK. No client read/write allowed.

Document shape:
- scopeType: string ("global" or "event")
- scopeId: string ("main" for global, pollId for event)
- notified: array of uid strings (records who has been push-notified for this message)
- createdAt: timestamp

Notifier behavior:
1. On new message in `messages` collection (Firestore trigger):
   - Resolve eligible recipients: active push endpoints where `webPushEnabled=true`
     and the matching `pushPreferences` boolean is true.
   - For event chat (`scopeType=="event"`), additionally filter to users who are
     attending the event (`attendance/{scopeId}`), excluding the message author.
   - For global chat (`scopeType=="global"`), filter to all users who have opted in,
     excluding the message author.
   - Write a `chat_push_actions/{messageId}` doc with `notified` array of notified uids.
   - Send push to each resolved endpoint.
2. Idempotency: check `chat_push_actions/{messageId}` before sending; skip uids
   already in `notified`.

## Push Payload Shape

JSON payload sent by notifier for poll events:
- eventType: poll_opened|poll_completed|poll_rescheduled
- pollId: string
- title: string
- body: string
- url: string (deep link)
- tag: string (stable dedupe display tag, e.g. "poll:{pollId}")
- sentAt: ISO timestamp

JSON payload sent by notifier for chat events:
- eventType: chat_message_sent_global|chat_message_sent_event
- messageId: string
- pollId: string|null (null for global chat)
- title: string
- body: string (truncated message preview)
- url: string (deep link to relevant page)
- tag: string ("chat:{scopeId}" — groups notifications by conversation)
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
- chat message push respects per-type preference; opting out prevents sends for that type
- chat message push is not sent to the message author
