# CDD: Reusable SSE Stream Prototype

**Status:** Proposed for implementation
**Date:** 2026-10-23
**Related:** [Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Projection Synchronisation Implementation Plan](0016-projection-sync-implementation.md);
[ADR-012: HTTP API Architecture](../adr/0012-api-architecture.md)

## 1. Purpose

Build and exercise the smallest reusable server-sent-events component needed by
current-Meets and attendance views. The prototype should turn the agreed stream
lifecycle and authorisation invalidation into tested Go code before it is wired
to production API endpoints.

This is a reusable component prototype, not a mock Meet API, Firestore listener,
or general event bus. Snapshot content and authorisation remain supplied by the
calling endpoint/application layer.

## 2. V1 Behaviour

- A new connection receives a complete current view from the local projection.
- A relevant committed view change causes the stream to read and send a fresh
  complete view. Intermediate states may be coalesced.
- Each connection is authorised against current state. The cursor never grants
  or preserves permission.
- After an authorisation change commits, the in-memory hub signals every active
  stream registered for that UID. At the next event boundary, the handler sends
  `event: view_reset` with no event ID and `data: {}`, then ends the HTTP
  response. The client reconnects and receives a newly authorised full view.
- A write already in flight when invalidation is published may complete. V1
  does not promise to retract it or serialise all response writes against
  authorisation commits.
- The server does not accept stream requests until projection startup readiness
  is complete. Shutdown ends active streams.
- V1 assumes one backend instance and one persistent SQLite database.

The prototype's first slice deliberately has no cursor parsing or incremental
resume. Cursor production and consumption are a later, separately tested slice;
this prototype must not make cursors appear unnecessary. See CDD-0014 for the V1
full-snapshot rule and the future per-UID authorisation-epoch rule for incremental
resume.

## 3. Proposed Component Boundary

Use the existing `net/http` stack. The Go HTTP server already runs handlers
concurrently; a handler owns its request for the stream lifetime. Do not spawn a
goroutine merely to keep each connection open.

A small internal component, proposed at
`internal/lastorders/components/sse`, owns:

- A hub that registers subscriptions by view key and, for authenticated
  subscriptions, by UID.
- A subscription with distinct view-changed, authorisation-invalidated and
  server-shutdown signals.
- Idempotent unregistration when the request ends.
- SSE framing and the request-lifetime loop, or a small handler helper used by
  the endpoint adapter.

The endpoint/application adapter supplies:

- The authenticated UID, obtained from the existing Firebase-auth request
  context; anonymous public views use an empty UID.
- A current-authorisation check for the requested view.
- A callback that reads a complete view from one consistent local snapshot and
  serialises it as JSON.
- The view key and SSE event name, such as a Group's current-Meets view or a
  Meet's attendance summary.

The hub has two notification routes:

- `PublishChanged(viewKey)` wakes subscribers to that view. A one-slot signal
  may coalesce repeated notifications because the handler rereads and sends the
  latest complete state.
- `InvalidateUID(uid)` non-blockingly signals every active subscription for
  that UID, across view keys. It must not wait for a slow client.

A separate shutdown signal ends all subscriptions. Do not close per-subscription
channels. Signal them non-blockingly and unregister under synchronised hub
ownership so concurrent publish, invalidation, shutdown and unregistration are
safe. Notification channels carry wake-ups, not projection data or credentials.

## 4. Connection Lifecycle

For each request:

1. Reject the request with `503 Service Unavailable` and `Retry-After` if
   startup readiness is incomplete. Do not register it with the hub.
2. Establish the view key and UID, then register the subscription **before**
   checking authorisation. This ensures an invalidation racing with setup is
   either seen by the current-authorisation check or retained in the registered
   subscription's buffered signal.
3. Check current authorisation. If denied, return the normal unauthorised or
   not-found response and unregister.
4. Read and send a complete initial snapshot.
5. Select on request cancellation, view change, UID invalidation and server
   shutdown. On a view change, recheck authorisation, read the latest complete
   snapshot and send it. On invalidation, send the generic reset without an ID
   and end the response. On cancellation or shutdown, return and unregister.
6. Before writing each snapshot, give an already-buffered invalidation priority
   over the snapshot. This prevents a known invalidation from being followed by
   another ordinary view event. An invalidation concurrent with an in-flight
   network write may still complete, as allowed by the V1 guarantee.

Use a normal completed `200` SSE response followed by EOF for reset and
shutdown; do not use `204`, which tells EventSource not to reconnect. The reset
contains no UID, permission name or resource detail.

## 5. Buffering and Slow Clients

Hub operations must never block on network writes. View-change notifications
are coalescible wake-ups; the next snapshot represents the latest committed
state. Authorisation invalidation and shutdown have distinct buffered signals
and must not be dropped. A slow or disconnected client is removed when its
request ends. Bound write duration so a blocked response cannot prevent the
handler from observing cancellation or invalidation indefinitely.

The prototype must explicitly resolve interaction with the current app server's
15-second `WriteTimeout` before production wiring. A fixed short response-write
timeout is incompatible with an indefinitely open SSE response unless the
stream uses an appropriate deadline strategy. Do not silently disable timeouts
for every API route.

## 6. Prototype Validation

Use standard-library HTTP test facilities and deterministic channel/barrier
coordination; do not rely on sleeps or live Firestore. The prototype must test:

- Initial full snapshot and a subsequent full snapshot after a committed view
  change.
- Multiple changes coalescing to the newest snapshot without losing final state.
- Invalidation delivered to all active streams for one UID, but not another
  UID; each invalidated stream emits one generic reset and ends.
- Registration-before-authorisation races, including invalidation after
  registration but before the initial snapshot is written.
- Invalidation during a snapshot read and while a response write is in flight;
  assert the documented event-boundary guarantee rather than claiming bytes
  already written can be recalled.
- Request cancellation, hub shutdown and idempotent unregistration.
- Concurrent notification, invalidation, shutdown and unregistration without
  blocking, send-on-closed-channel panics or leaked subscriptions.
- Readiness rejection before registration and recovery once ready.
- A slow/disconnected reader is bounded by the selected write-deadline policy
  and does not block other subscribers or hub publishers.
- No attendee identities in anonymous attendance output; authenticated GET
  output may include names, while attendance SSE remains aggregate-only.

Tests should use separate stream keys to prove unrelated views do not wake one
another. No production test hooks or reflection are introduced.

## 7. Cursor Follow-up

After the lifecycle prototype is accepted, add opaque cursor support in a
separate slice:

- Emit a cursor corresponding to each full snapshot and use it as the SSE event
  ID. Preserve V1's full snapshot on every new connection, even when a cursor is
  supplied.
- Keep stream contract, view scope, database generation and global sequence
  behind the opaque cursor contract. Do not expose SQLite implementation
  details.
- Incremental resume is a later extension. It requires validating the cursor's
  per-UID authorisation epoch before returning any deltas. Advance that durable
  epoch atomically with the local authorisation projection, retain it across
  user deletion, and force a full snapshot on mismatch. The in-memory hub still
  closes already-open streams.

The prototype does not decide the cursor encoding or signing-key format; those
remain subject to the cursor design and tests.

## 8. Integration Checks Before Production Use

- Confirm how browser clients send Firebase credentials for an SSE `GET`.
  Native `EventSource` cannot set an arbitrary bearer header; a fetch-based
  streaming client is one option. Do not put bearer tokens in cursor/query
  strings.
- Update or wrap API CORS policy for authenticated `GET` streaming and its
  preflight behaviour. The current CORS middleware is tailored to `POST` routes.
- Wire the readiness gate to app startup after the required local projection
  baselines have committed, and close the stream hub before shutting down the
  HTTP server.
- Keep endpoint-specific authorization and public/private response shaping out
  of the generic hub.

## 9. Completion Criteria

The prototype is ready to reuse when its race, lifecycle, privacy, readiness and
slow-client tests pass; its API surface remains small; and the write-timeout and
browser-auth transport questions are recorded with a concrete production
integration decision. Cursor parsing and incremental resume are not required to
complete this prototype.
