# CDD: Reusable SSE Stream Prototype

**Status:** Lifecycle and cursor components implemented; venue collection endpoint integration in progress
**Date:** 2026-10-26
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
- The prototype exercises one hub and one persistent SQLite database. V1
  production also deploys one backend instance. The design keeps listeners,
  projections and hubs instance-local so future instances can each observe
  source changes independently, but V1 does not guarantee multi-instance
  operation or cross-instance convergence. A future reconnect may reach another
  instance and receive that instance's current local view after authorization
  is checked there.

UID invalidation covers application permission or membership changes for an
authenticated caller. Authorization-scope invalidation covers visibility
changes and includes anonymous streams. Neither mechanism detects Firebase
ID-token revocation or expiry. That token-lifetime behavior must be resolved
separately for a long-lived connection; validating the token when the stream
starts does not imply the server learns of remote token revocation later.

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

- A hub that registers subscriptions by view key, authorization scope and, for
  authenticated subscriptions, UID. An authorization scope identifies the
  resource boundary whose visibility controls the view, such as a Group. This
  lets visibility changes reset anonymous as well as authenticated streams.
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

The hub has notification routes for view changes and authorization changes:

- `PublishChanged(viewKey)` wakes subscribers to that view. A one-slot signal
  may coalesce repeated notifications because the handler rereads and sends the
  latest complete state.
- `InvalidateUID(uid)` non-blockingly signals every active subscription for
  that UID, across view keys. It must not wait for a slow client.
- `InvalidateAllAuthenticated()` non-blockingly signals every active
  authenticated subscription on this instance. Use this conservative fallback
  when an authorization-source listener cannot establish which UIDs were
  affected, and when it reconnects after a period in which changes may have
  been missed. It must not wait for slow clients.
- `InvalidateScope(scopeKey)` non-blockingly signals every subscription in the
  affected authorization scope, including anonymous subscriptions, then causes
  the generic reset and closes each stream. Use this after a visibility change
  that may change access to the view. It must not wait for slow clients.

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
handler from observing cancellation or invalidation indefinitely. The handler
sends and flushes an SSE comment heartbeat every 15 seconds by default; the
interval is configurable per handler. Heartbeats carry no application event or
view data and let the HTTP stack detect disconnected peers during otherwise
quiet periods.

The current app server has a 15-second `WriteTimeout`. The prototype keeps that
server-wide setting for ordinary requests. At stream start, the handler clears
the inherited deadline for this response only. Before each frame it sets a
fresh write deadline, writes and flushes the frame, then clears the deadline
again before it can expire. This is necessary because Go's
`ResponseController.SetWriteDeadline` cannot revive an already-expired write
deadline. The per-frame timeout bounds slow clients while allowing an idle
stream to remain open between heartbeats. A response writer that does not
support deadline control cannot serve a stream; the handler ends it rather than
silently falling back to an unbounded write. This preserves the server timeout
for every non-SSE route without requiring a separate HTTP server.

## 6. Prototype Validation

Use standard-library HTTP test facilities and deterministic channel/barrier
coordination; do not rely on sleeps or live Firestore. The prototype must test:

- Initial full snapshot and a subsequent full snapshot after a committed view
  change.
- Multiple changes coalescing to the newest snapshot without losing final state.
- Invalidation delivered to all active streams for one UID, but not another
  UID; each invalidated stream emits one generic reset and ends.
- Conservative all-authenticated-stream invalidation resets every authenticated
  subscription on that hub and does not reset anonymous subscriptions.
- Scope invalidation resets both authenticated and anonymous subscriptions in
  the affected scope, without resetting subscriptions in another scope.
- Registration-before-authorisation races, including invalidation after
  registration but before the initial snapshot is written.
- Invalidation during a snapshot read and while a response write is in flight;
  assert the documented event-boundary guarantee rather than claiming bytes
  already written can be recalled.
- Request cancellation, hub shutdown and idempotent unregistration.
- Concurrent notification, invalidation, shutdown and unregistration without
  blocking, send-on-closed-channel panics or leaked subscriptions.
- Heartbeat comments are flushed without producing application events, and a
  stalled heartbeat write ends within the configured write deadline.
- Readiness rejection before registration and recovery once ready.
- A slow/disconnected reader is bounded by the selected write-deadline policy
  and does not block other subscribers or hub publishers.
- No attendee identities in anonymous attendance output; authenticated GET
  output may include names, while attendance SSE remains aggregate-only.

Tests should use separate stream keys to prove unrelated views do not wake one
another. No production test hooks or reflection are introduced.

## 7. Cursor Follow-up

**Implementation status:** The component now provides the cursor codec,
request-cursor precedence helper, sequence-aware hub handover, and SSE event-ID
framing. Production snapshot adapters, runtime secret loading, GET snapshots,
and SSE endpoint integration remain pending.

After the lifecycle prototype is accepted, add the V1 opaque-cursor slice
described below. This slice adds cursor production and validation to full
snapshots; it does not add incremental delta replay. Every connection continues
to return a complete, currently authorised view, including when the supplied
cursor is valid. This matches the existing stream and snapshot definitions in
`docs/pubnightpicker-openapi-v1.yaml`.

Each snapshot adapter returns its serialized view and cursor as one result. It
reads the view and the shared sync state (`generation`, global `sequence`) in
the same SQLite read transaction. The SSE handler writes that cursor both as
the event `id` and in the JSON `cursor` field. A reset event has no ID. The
initial subscription-before-snapshot ordering remains in place. View-change
notifications carry committed sequence numbers and coalesce to the highest
pending sequence. After the initial snapshot, discard buffered notifications at
or below its high-water mark; deliver later ones. After a live snapshot, ignore
notifications at or below the last-sent high-water mark. This preserves the
snapshot-to-live boundary without redundant pre-boundary snapshots.

The V1 cursor is a versioned HMAC-SHA256 token. Its payload contains the stream
contract, resource scope, SQLite database generation and global sequence.
Current-Meets cursors also contain the `Europe/London` calendar date. The
token format is `v1.<base64url-payload>.<base64url-mac>`; the MAC covers a
cursor-specific domain separator and the encoded payload. The token is opaque
as an API contract and is not confidential; clients must store and return it
without interpreting its contents. It is not an authorization credential.

Use a separate runtime secret named `LAST_ORDERS_CURSOR_HMAC_KEY`, encoded as
unpadded base64url for at least 32 random bytes. Load it from the deployment's
secret configuration, keep it stable across restarts, and never log it. Cursor-enabled
endpoints must fail startup if the secret is absent or malformed. V1 uses one
active key; rotating it intentionally invalidates older cursors, which then
recover by returning a full current view. Test code injects a fixed key rather
than relying on environment state.

On connection, `Last-Event-ID` takes precedence over the `after` query value.
Validate a supplied cursor's MAC, version, stream contract, resource scope,
database generation, non-negative sequence bound, current high-water bound,
and (for Current-Meets) London date. A malformed, tampered, future,
wrong-scope, wrong-generation, wrong-date or old-key cursor is not an HTTP
error: return a complete current snapshot and a newly issued cursor. The cursor
does not skip the initial snapshot in V1. No cursor expiry is needed while
tombstones are retained indefinitely.

The endpoint adapter owns resource-specific snapshot queries and authorization,
and obtains the snapshot's committed generation and sequence from the same
SQLite transaction. The cursor codec owns token encoding and validation; the
generic handler owns SSE framing and event IDs. For example, the Current-Meets
stream is scoped to a Group and the attendance stream to a Meet. Current
authorization is checked on every connection; the in-memory hub continues to
reset already-open streams after authorization changes.

Cursor component tests cover encoding/decoding, tampering, unsupported
versions, scope/resource mismatch, generation/date mismatch, negative/future
sequences, key rotation, `Last-Event-ID` precedence and cursor equality between
the JSON snapshot and SSE ID. Endpoint integration tests must also prove that
valid and invalid reconnect cursors both receive full snapshots with a fresh
cursor. A concurrent projection update test verifies that each cursor
identifies the same SQLite snapshot as its data.

Incremental resume is a later slice. Before returning deltas, it requires a
durable per-UID authorization epoch advanced atomically with the local
authorization projection and bound into each cursor. Retain that epoch across
user deletion and never reset or reuse it within a database generation. An
epoch mismatch forces a full snapshot; the in-memory hub still closes
already-open streams.

## 8. Integration Checks Before Production Use

- Confirm how browser clients send Firebase credentials for an SSE `GET`.
  Native `EventSource` cannot set an arbitrary bearer header; a fetch-based
  streaming client is one option. Do not put bearer tokens in cursor/query
  strings.
- Decide whether Firebase token validity is checked only at stream creation or
  needs a separate mid-connection expiry/revocation policy. The per-UID hub
  handles application permission changes, not Firebase token revocation by
  itself.
- Update or wrap API CORS policy for authenticated `GET` streaming and its
  preflight behaviour. The current CORS middleware is tailored to `POST` routes.
- Wire the readiness gate to app startup after the required local projection
  baselines have committed, and close the stream hub before shutting down the
  HTTP server.
- Keep endpoint-specific authorization and public/private response shaping out
  of the generic hub.

## 9. Completion Criteria

The prototype is ready to reuse when its race, lifecycle, privacy, readiness and
slow-client tests pass; its API surface remains small; and the per-frame
write-timeout policy is recorded for production wiring. Browser-auth transport
remains an endpoint integration decision. Cursor codec tests are part of the
component prototype; production recovery reads and incremental resume remain
separate follow-up work.
