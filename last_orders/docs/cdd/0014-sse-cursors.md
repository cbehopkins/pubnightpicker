# ADR-001: Global Change Tracking and Opaque SSE Cursors

- **Status:** Accepted
- **Date:** 2026-10-19
- **Scope:** PubNightPicker Go backend, SQLite read model, SSE live-update API

## Context

PubNightPicker maintains a local SQLite read model populated from Firestore. SQLite is a disposable, server-local cache of authoritative data, intended to avoid repeatedly querying Firestore for read operations.

The frontend needs a mechanism to learn about changes while a relevant screen is open. Server-Sent Events (SSE) will provide low-latency notifications, while ordinary API requests remain responsible for retrieving resource data.

The system must support clients disconnecting and reconnecting without requiring a durable event queue or replaying every intermediate change. A client that falls too far behind, or whose cursor is no longer usable, must be able to recover by refreshing its state.

## Decision

### 1. Maintain a global change sequence

The backend will maintain a monotonically increasing sequence of committed changes to the SQLite read model.

Each independently synchronizable resource will record the sequence number of its most recent change.

For example, a resource updated at sequence 120 will have a change version of 120. If it is subsequently updated at sequence 127, its current change version becomes 127.

The sequence represents change ordering, not wall-clock time.

### 2. Update resource data and its change version atomically

The SQLite projection and the corresponding change version must be updated in the same transaction.

A committed resource update must never become visible without its corresponding change version. Likewise, a version must never advance without the associated resource change being committed.

SQLite transaction and write-serialization semantics will be used to maintain consistent ordering.

### 3. Expose opaque, stream-scoped cursors

The API will expose an opaque cursor representing a synchronization position.

The cursor will be scoped to a logical stream contract, such as `app-changes-v1`. It will not be tied to an individual SSE connection.

The cursor is an API-level abstraction. It must not expose a dependency on SQLite row IDs, sequence implementation, timestamps, or other internal storage details. Its internal representation may initially map directly to the global change sequence.

Cursors from incompatible stream contracts must not be treated as interchangeable. The server may reject an invalid cursor or instruct the client to perform a full refresh.

The cursor format and stream contract will be versionable independently of the underlying storage implementation.

### 4. Determine changes from the current read model

The system will not require a durable history of event payloads.

When a client reconnects with a valid cursor, the backend will identify resources whose latest change versions are later than that cursor and return or notify the client about their latest state.

Intermediate updates to the same resource may be coalesced. The system does not promise to deliver every individual change.

The server must define a consistent synchronization boundary (high-water mark) and ensure the client can safely advance its cursor only after processing the changes through that boundary. Where results are paginated, the same boundary must be maintained across the synchronization pass.

### 5. Use SSE for live notification, not as the source of truth

An in-memory dispatcher will notify currently connected clients of relevant changes.

SSE events may identify changed resources or indicate that a view needs refreshing. Clients will use the API to obtain authoritative current representations from the read model.

The dispatcher is ephemeral. It does not need to retain events for disconnected clients, and it does not constitute a durable dispatch queue.

If the process crashes, in-memory dispatcher state may be lost. On reconnect, the client will synchronize from its last valid cursor against the read model. If incremental synchronization is not possible, the server will request a full refresh.

### 6. Handle deletions explicitly

A resource deleted from the read model cannot be discovered by querying only the remaining resources.

The implementation must therefore represent deletions in a way that supports synchronization, such as retaining tombstones with their change versions, or using an equivalent invalidation mechanism.

Deletion markers may be cleaned up according to a defined retention policy. If a client's cursor predates the available deletion information, the client must perform a full refresh.

### 7. Scope notifications to relevant clients

The sequence is global, but event delivery remains subject to authorization and subscription rules.

Only clients with the appropriate authorization and an active relevant subscription should receive resource notifications. A gap in observed sequence numbers does not, by itself, indicate that events were lost, since intervening changes may be irrelevant to that client.

Changes to a client's authorization or subscription scope must not leave it relying on a cursor that no longer represents a complete view of its accessible data. Such changes may require a full refresh.

## Consequences

### Positive

- No durable event queue or event-payload history is required.
- Repeated updates to the same resource can be coalesced.
- Reconnection can recover from the current SQLite read model.
- The frontend remains independent of SQLite and the internal sequence implementation.
- SSE and ordinary REST resource retrieval remain separate concerns.
- The cursor abstraction permits future changes to the storage or synchronization implementation.

### Trade-offs and constraints

- Correct cursor advancement and synchronization boundaries require careful implementation.
- Deletions require tombstones or an equivalent mechanism.
- A full refresh is the fallback when incremental synchronization cannot be proven complete.
- The in-memory dispatcher provides low-latency notification, not guaranteed delivery.
- If the SQLite read model is lost or rebuilt, clients may need to refresh their state.

## Implementation decisions (2026-10-22)

- A venue is one independently synchronisable resource per venue ID.
- A user profile is one combined resource per user ID, rather than separate
	synchronisable preference and endpoint resources. Push endpoint additions,
	changes and removals participate in the owning profile's version.
- Retain every explicitly declared field in the relevant Firestore schemas,
	including venue notes and amenities, rather than the existing cache subsets.
	`user-public` is authoritative for fields also declared in `users`. This
	precedence does not grant public access to private user fields or endpoint
	credentials. There is no fallback to private values when the public document
	or field is absent or null. Deleting the public document removes its
	contributed fields, not any remaining private data.
- Duplicate profile fields in `users`, such as `name`, `photoUrl` and
	`votesVisible`, are deprecated. Update frontend consumers to read their public
	counterparts, then remove the deprecated private fields in a coordinated
	schema/data cleanup. This decision does not itself remove fields or migrate
	clients. User ID remains the combined resource identity; precise schema
	removals belong to that follow-up.
- Compare normalised domain values using field-specific defaults. Preserve
	missing/null distinctions where meaningful, rather than treating every
	missing field as false. Muted poll IDs are compared as a set; other arrays
	follow their domain semantics. Local ingestion timestamps do not affect
	resource versions.
- Explicitly declared fields in the Firestore schemas define the domain data
	under consideration, rather than arbitrary additional document fields or
	local ingestion metadata. The reference schemas are
	`schema/firestore/collections/pubs.schema.json`, `users.schema.json`, and
	`user-public.schema.json` in the sibling schema project. Existing cache
	models must not silently define the full API resource contract: they currently
	retain only subsets of this data.
- Projection stores own resource versions and deletion markers. Shared
	infrastructure owns only the global sequence allocator, used within the
	projection write transaction.
- V1 retains deletion markers indefinitely. A V2 maintenance task may introduce
	cleanup, together with a retention boundary that requires older cursors to
	perform a full refresh.
- Each SQLite database generation has a persistent identity. Ordinary process
	restarts preserve that identity; rebuilding or replacing the read model must
	establish a new generation. Cursors from a different generation require a
	full refresh, even if their sequence values overlap those in the new database.
- Existing local projection data may be discarded and rebuilt during migration;
	preserving or backfilling old cache rows is not required. Reset affected
	projection state and establish a new generation atomically, then reconcile
	from authoritative sources using existing local-miss behaviour. This does
	not authorise deletion of unrelated tables or durable application work.

Before implementing the combined user profile, define resource existence when
only some source documents are present, and which fields each authorised caller
may retrieve. Deprecating duplicate fields does not by itself define which
source deletion removes the combined resource. A
combined internal resource must not imply public exposure of private user data
or push credentials.

## Implementation outline

The staged delivery plan and outstanding resource-contract decisions are tracked
in [CDD-0016: Projection Synchronisation Implementation Plan](0016-projection-sync-implementation.md).

1. Add global change-sequence allocation to the SQLite read-model update path.
2. Store the last-change sequence on independently synchronizable resources.
3. Ensure projection updates and version assignments commit atomically.
4. Define the opaque cursor contract for `app-changes-v1`.
5. Implement cursor validation and incremental synchronization with a consistent high-water mark.
6. Implement SSE notifications for currently connected, interested clients.
7. Implement deletion tracking and a full-refresh fallback.
8. Test disconnects, reconnects, concurrent updates, repeated updates, deletions, process restarts, and read-model rebuilds.

## Out of scope

- Durable event delivery guarantees.
- Replaying every intermediate resource change.
- Persisting SSE connections or dispatcher state across process restarts.
- Introducing a distributed message broker before deployment requirements justify one.
- Defining push-notification delivery, which remains a separate mechanism for user notifications.

## Guiding principle

**The read model records the latest version of each resource; the cursor identifies a synchronization position; SSE tells connected clients when they may need to synchronize.** The system does not depend on retaining a permanent history of events.
