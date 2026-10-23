# CDD: Local Projection Streaming

**Status:** Draft - direction agreed; detailed design pending
**Date:** 2026-10-19
**Related:** [ADR-012, section 10](../adr/0012-api-architecture.md#10-server-sent-events); [CDD-0014: Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md); [CDD-0017: Reusable SSE Stream Prototype](0017-sse-stream-prototype.md)

## 1. Purpose

Design a reusable component for streaming application views derived from local
database projections. Attendance and current-Meet streams must share this
component rather than independently implement cursor and recovery behaviour.
Streaming describes the state currently committed to this backend's local
projection; it does not wait for, or claim completion of, remote Firebase
ingestion.

This draft records agreed decisions and the design questions still to resolve.
It is not an implementation-ready cursor or wire contract.

## 2. Agreed Decisions

- Streams are derived from projections in the local database, following the
  existing [Venue cache](../../internal/lastorders/components/venuecache/store.go)
  pattern. They are not direct streams of Firestore mutations.
- Streaming cursors use the global SQLite change sequence described by
  [CDD-0014](0014-sse-cursors.md), not wall-clock timestamps. Each independently
  synchronizable resource records its latest change sequence. Projection data,
  that resource version, and allocation of the global sequence are committed
  atomically in the same SQLite transaction.
- The first snapshot from a Firebase query listener is not a backend-wide
  readiness signal. The Go Firestore iterator exposes query snapshots and their
  changes, but no explicit initial-sync-complete flag. Listener startup and
  catch-up are asynchronous; backend operations must not wait for every remote
  projection listener to finish. Each operation defines its own behaviour when
  the required local projection is absent or incomplete, such as a source
  read-through or an explicit unavailable result.
- An initial SSE view is a consistent view of local state at its SQLite
  high-water mark. It must not imply that Firebase has finished populating the
  local projection.
- Events describe resulting view state, not storage operations. Clients need
  not observe every intermediate change to recover correctly.
- A new stream without a cursor starts with a full replacement of the currently
  authorized view. V1 sends a complete replacement view when a stream connects
  and after each relevant change, including when a valid cursor is supplied.
  This avoids a durable event log and makes reconnect recovery independent of
  retaining intermediate event payloads.
- Every connection returns a full current view; the server does not incrementally
  apply changes from a cursor. A malformed, future, wrong-scope or
  wrong-generation cursor is ignored and the current view is still returned.
- While a connection is live, each SSE event carries the latest complete view;
  it is not a durable record of every intermediate projection write. On
  reconnect, the server sends a fresh complete view. If a client cannot keep up,
  the server closes the stream and the client reconnects.
- An unknown, invalid, expired or otherwise unusable cursor returns the latest
  version of the requested view. For example, a request for Meet changes since
  an unusable cursor returns the latest version of that Meet.
- A cursor is opaque and scoped to a stream contract and the lineage of the
  local projection database that issued it, as well as its synchronization
  position. The lineage identifier is persisted with that database and changes
  when its projection history is reset or rebuilt. A cursor from another
  lineage is unusable and causes a fresh view to be returned. Separate backend
  instances with separate local databases do not share a cursor history; shared
  projection storage and dispatch are not required at this stage.
- Every GET and stream connection checks current authorization. A cursor does
  not freeze or encode the client's permissions.
- **Open-stream authorization invalidation:** The HTTP request handler owns the
  long-lived stream; no extra goroutine is needed just to keep it open. Keep an
  in-memory hub keyed by authenticated UID, with a buffered invalidation signal
  registered for each
  active stream. Register before checking current authorization so a racing
  invalidation is either observed by the check or delivered to the stream.
  After an authorization change commits, publish an invalidation for the
  affected UID. The hub non-blockingly signals every registered stream; it does
  not close per-stream channels. At the next event boundary, the handler sends
  a generic `view_reset` with no event ID and `data: {}`, then closes the
  response. The client reconnects and receives a full view after fresh
  authorization. If no stream is registered, no invalidation state is retained.
- Authorization invalidation is in-memory, not durable. A write already in
  flight when invalidation is published may complete; V1 does not promise
  retraction or a strict post-commit send barrier. A stronger guarantee would
  require serializing event writes with authorization changes.
- Current Meet membership can change at London calendar midnight without a
  projection write. At that boundary, the Current Meet stream sends a generic
  `view_reset` event with no event ID, then terminates. Every reconnect receives
  a full replacement, including same-date reconnects. This also recovers clients
  that were disconnected, or a backend that was unavailable, at midnight.
  Client-side jitter may be added later to spread reconnect load; it is not
  required for correctness at current scale.
- A snapshot and its cursor must describe the same logical state. Changes must
  not be lost between snapshot retrieval and live subscription.
- SSE event IDs and `Last-Event-ID` support reconnection. Their exact encoding
  and recovery envelopes will be specified in OpenAPI. Every connection gets a
  full current view and fresh cursor; an unusable cursor is not an error.
- SSE connections are not accepted until backend startup readiness checks have
  completed. Required local projections must be initialised and startup
  baselines committed; this does not claim that Firebase is globally caught up.
  Active streams close during shutdown and clients reconnect after startup.
- The API must not promise durable replay of every intermediate event.
- Existing authentication and Group authorisation rules apply to snapshots,
  streams and recovery responses.

## 3. Detailed Design Checklist

The following original checklist is retained for traceability; current status
and decisions are recorded below. Sequence allocation and atomic projection
versions are implemented. Snapshot handover, complete-view event semantics,
startup gating, single-instance deployment and bounded-client behaviour are
decided for V1. Cursor encoding remains a proposal. Authorization invalidation
is decided as a per-UID in-memory stream signal; no durable marker is required
because every reconnect receives a complete snapshot.

1. **Sequence writes:** Define the shared SQLite mechanism for allocating a
  monotonically increasing sequence and recording each resource's latest
  version atomically with projection writes. All writers, including ingestion,
  read-through population and backend mutations, must use it.
2. **Cursor contract:** Specify encoding while keeping cursors opaque, the stream
  contract and database-lineage fields, and cursor expiry/validation. Define
  high-water marks for snapshots and pagination; do not use timestamps as the
  ordering mechanism. Align cursor validation and recovery envelopes with
  [CDD-0014](0014-sse-cursors.md).
3. **Snapshot and live handover:** Define the SQLite read boundary and dispatcher
  handover so an update concurrent with snapshot retrieval cannot be missed.
4. **View composition:** Define consistent revisions for views assembled from
  multiple projection records and how changes are represented to clients.
5. **Removal and visibility changes:** Define tombstone or equivalent handling
  for deletions and collection-boundary changes, retention, and how sync tells
  clients to remove data that is no longer visible after re-authorization.
  Publish per-UID stream invalidation only after the authorization change
  commits. The handler emits a generic reset and closes; reconnect always gets
  a full snapshot, so no durable marker is required.
6. **Recovery:** Define the full-view response and replacement cursor when no
  cursor is supplied or incremental continuation is unsafe, including expired
  cursors, missing deletion history, authorization invalidation, date-boundary
  mismatch and lineage mismatch. The Current Meet reset/reconnect behaviour is
  decided; its wire envelope remains to be specified.
7. **Runtime behaviour:** Define reusable component responsibilities, subscriber
  lifecycle, slow-client handling and backpressure, per-operation behaviour when
  local projection data is unavailable, and expected routing when instances
  have separate local databases.
8. **HTTP contract:** Specify attendance and current-Meet event schemas, reset
  notifications, initial local-view delivery and recovery envelopes in OpenAPI.
  Keep past-Meet pagination cursor design separate from streaming cursors.

### V1 Decision Status (2026-10-23)

- **Cursor proposal:** Use a versioned, HMAC-authenticated opaque token scoped
  to stream contract, resource ID, database generation and global sequence.
  Current-Meets tokens also include the `Europe/London` calendar date. The
  signing key must remain stable across restarts. V1 has no cursor expiry while
  tombstones are retained indefinitely. Malformed, tampered, future,
  wrong-scope, wrong-generation or wrong-date cursors return a full view.
  Signing-key rotation also results in full views.
- **Handover:** Register the subscriber before reading the snapshot. Read the
  view and high-water mark in one SQLite read transaction, send that view and
  cursor, discard buffered notifications at or below the high-water mark, then
  deliver later notifications.
- **Events:** Send complete replacement views on stream connection and after
  relevant changes. Current-Meets events replace the collection; attendance
  events contain aggregate counts. The opaque cursor is both the SSE event ID
  and the `cursor` field in snapshot/event JSON. `Last-Event-ID` takes
  precedence over `after`. Past-Meet pagination cursors are separate.
- **Startup/runtime:** Do not accept SSE until required local projections have
  committed startup baselines. V1 assumes one backend instance and one
  persistent SQLite database. Close active streams during shutdown. Bound
  subscriber buffers; close a slow client's stream on overflow so it can
  reconnect for a complete view.
- **Public attendance:** Public Group Meet details and attendance counts are
  available anonymously. Anonymous attendance responses and streams never
  expose attendee identities; authenticated GET responses may include attendee
  details.
- **Authorization invalidation:** After an authorization change commits,
  non-blockingly signal each active stream registered for the affected UID.
  The handler sends a generic reset with no event ID and `data: {}`, then
  closes. The client reconnects and receives a complete snapshot after fresh
  authorization. No durable marker is needed. An event already in flight may
  complete.
- **Future incremental resume (not V1):** Cursors remain useful when the
  authorization has not changed. The selected invalidation mechanism is a
  durable per-UID authorization epoch, advanced atomically with the committed
  local authorization projection and bound into each cursor. Before returning
  deltas, compare the cursor's epoch with the current durable epoch for that UID;
  a mismatch forces a full snapshot. Retain the epoch across user deletion and
  never reset or reuse it within a database generation. Keep the in-memory hub
  to promptly close live streams. Cursor deletion is not selected because a
  registry would need to cover cursors held by clients already disconnected.

## 4. Verification Criteria

The completed design and implementation must demonstrate:

- No missed final state during snapshot/subscription races or reconnects.
- Correct behaviour for concurrent projection writes and globally ordered
  sequence allocation.
- Latest-view recovery for unusable cursors, including after restart or rebuild.
- Every connection receives a complete current view under current
  authorization; no incremental event history is required.
- Correct removal and collection-boundary updates without leaking hidden data.
- A detected authorization change signals active streams for the affected UID;
  reconnect returns a fully re-authorized view.
- Current Meet clients refresh at the London midnight boundary and recover
  correctly when reconnecting after that boundary.
- A cursor from a different projection lineage causes a correct fresh sync.
- Consistent authorization for initial state, live updates and recovery.
- Backend operations do not depend on Firebase listener startup or catch-up
  completing; their local-miss behaviour is explicit.
- Reuse of the same component for attendance and current-Meet views.
