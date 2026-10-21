# CDD: Local Projection Streaming

**Status:** Draft - direction agreed; detailed design pending
**Date:** 2026-10-19
**Related:** [ADR-012, section 10](../adr/0012-api-architecture.md#10-server-sent-events); [CDD-0014: Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md)

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
  authorized view. A valid cursor from the same database lineage can resume with
  changes committed after that cursor; repeated changes to a resource may be
  coalesced to its latest state.
- A cursor cannot be continued incrementally if the projection lineage differs,
  required change/deletion history is unavailable, or the effective
  authorization or time-dependent view has changed in a way that invalidates
  the prior view. In these cases, the server returns a full replacement of the
  currently authorized view with a replacement cursor.
- While a connection is live, SSE notifications indicate that the view may have
  changed; they are not the source of truth or a durable record of every
  projection write. The client obtains current authorized view data through the
  API. If a client cannot keep up with notifications, the server may terminate
  or reset the stream; the client resumes from its last processed cursor, or
  receives a full replacement if incremental recovery is no longer safe.
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
- Every snapshot or incremental synchronization re-authorizes the request
  against the current authorization state. A cursor does not freeze or encode
  the client's permissions. Synchronization results must reflect current
  visibility, including resources the client is no longer allowed to see.
- A change to effective authorization that can alter a client's view must be
  represented by durable synchronization state, such as a user- or
  authorization-scope invalidation marker in the ordered change stream. The
  marker is committed atomically with the local authorization projection
  change. A live stream is notified to invalidate/reset; reconnecting from a
  cursor before the marker must receive a full currently authorized view rather
  than applying ordinary resource changes alone. The marker must not disclose
  protected permission or resource details. When a connected stream reaches
  this marker, it emits a generic view-reset notification and terminates before
  sending further ordinary view changes; the client reconnects to receive the
  full currently authorized view.
- Current Meet membership can change at London calendar midnight without a
  projection write. At that boundary, the Current Meet stream sends a view-reset
  notification and then terminates. A reconnect with a cursor from an earlier
  London calendar date must receive a full replacement of the current view; a
  same-date cursor may resume incrementally if otherwise valid. This also
  recovers clients that were disconnected, or a backend that was unavailable,
  at midnight. Client-side jitter may be added later to spread reconnect load;
  it is not required for correctness at current scale.
- A snapshot and its cursor must describe the same logical state. Changes must
  not be lost between snapshot retrieval and live subscription.
- SSE event IDs and `Last-Event-ID` support reconnection. Their exact encoding
  and recovery envelopes will be specified in OpenAPI. A supplied valid cursor
  enables incremental catch-up; a missing or unusable cursor yields a full
  authorized view.
- The API must not promise durable replay of every intermediate event.
- Existing authentication and Group authorisation rules apply to snapshots,
  streams and recovery responses.

## 3. Detailed Design Required

Resolve the following before implementing the component:

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
  Define the durable authorization invalidation marker, its ordering relative
  to resource changes, and whether it is tracked per user or per authorization
  scope. A live notification callback alone is insufficient for reconnect
  correctness.
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

## 4. Verification Criteria

The completed design and implementation must demonstrate:

- No missed final state during snapshot/subscription races or reconnects.
- Correct behaviour for concurrent projection writes and globally ordered
  sequence allocation.
- Latest-view recovery for unusable cursors, including after restart or rebuild.
- Incremental catch-up for valid cursors when authorization, lineage and
  time-dependent view context remain compatible.
- Correct removal and collection-boundary updates without leaking hidden data.
- Every synchronization is authorized against current state, and revoked
  visibility causes a full authorized view when incremental continuation would
  otherwise retain or expose stale data.
- Current Meet clients refresh at the London midnight boundary and recover
  correctly when reconnecting after that boundary.
- A cursor from a different projection lineage causes a correct fresh sync.
- Consistent authorization for initial state, live updates and recovery.
- Backend operations do not depend on Firebase listener startup or catch-up
  completing; their local-miss behaviour is explicit.
- Reuse of the same component for attendance and current-Meet views.
