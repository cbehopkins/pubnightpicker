# CDD: Reusable Projection Listeners

**Status:** Draft - proposed direction for review  
**Date:** 2026-10-21  
**Related:** [CDD-0013: Local Projection Streaming](0013-projection-streaming.md); [CDD-0014: Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md); [CDD-0002: Base Store](0002-base-store.md)

## 1. Purpose

Define a reusable framework for maintaining local SQLite projections from
authoritative remote sources such as Firestore. Venue cache is the first example;
future projections may include Meets, users, votes, and other read models.

This CDD concerns remote-to-local projection maintenance. It does not define
client-facing SSE delivery, which is covered by [CDD-0013](0013-projection-streaming.md),
or listeners that consume source changes to create truths or other work.

## 2. Context

The current venue-cache listener owns stream startup, retry, cancellation, batch
iteration, readiness signalling, document validation, and projection writes.
Similar stream-lifecycle code exists in other database listeners. The source
types and projection operations are domain-specific, however, and should remain
owned by their projection adapters.

The current venue-cache source returns typed change batches, while its listener
performs per-document conversion and `Put`/`Delete` calls. Those writes currently
use `*sql.DB` directly and do not allocate the global change sequence described
by CDD-0014. Reuse must provide a path to update projection state and its change
version atomically without moving application-specific projection semantics into
the Base Store.

## 3. Proposed Decisions

### 3.1 Separate shared stream mechanics from projection semantics

A common runner owns the lifecycle of a source change stream:

- open the stream;
- read and forward typed batches;
- stop the stream when the run ends;
- respond to cancellation;
- retry after retryable open or read failures using a defined backoff policy;
- log failures with projection/stream identity.

The projection adapter owns:

- the source interface and change type;
- mapping remote documents to local projection data;
- malformed-document policy;
- deciding how additions, modifications, and removals affect its projection;
- reconnect reconciliation and any projection-specific readiness or freshness
  information.

The common runner does not know Firestore document fields, local table schemas,
or whether a source listener creates derived work rather than updating a
projection.

### 3.2 Keep source and change types typed

Each projection defines the smallest source contract it needs. Shared lifecycle
code uses Go generics so it does not require one untyped union of all projection
changes. Each newly opened stream emits one complete baseline batch followed by
zero or more ordered change batches. After a stream is reopened, it must emit a
new complete baseline. The source adapter is responsible for guaranteeing that
the baseline represents the complete result set for the projection it owns.

The proposed interface is:

```go
type BatchKind uint8

const (
      BaselineBatch BatchKind = iota
      DeltaBatch
)

type Batch[T any] struct {
      Kind    BatchKind
      Changes []T
}

type Stream[T any] interface {
   Next() (Batch[T], error)
   Stop()
}

type Source[T any] interface {
   Watch(context.Context) (Stream[T], error)
}

type Applier[T any] interface {
   Apply(context.Context, Batch[T]) error
}

type Runner[T any] interface {
   Start(context.Context) error
   Close() error
}
```

`Runner` is constructed from one `Source[T]`, one `Applier[T]`, a logger, and
the shared retry policy. The source and applier remain projection-specific; the
runner does not expose source SDK types or projection schema.

For each opened stream, the runner requires the first batch to be
`BaselineBatch` and subsequent batches to be `DeltaBatch`. An invalid batch
order fails that stream and triggers the shared recovery path. An empty baseline
is valid and means that the authoritative result set is empty. The applier
commits a baseline as an atomic replacement and each delta batch atomically.

### 3.3 Separate baseline application from application-wide readiness

The source contract explicitly identifies the first complete baseline; the
framework does not infer completeness merely from receiving an arbitrary first
batch. In the pinned Firestore Go SDK (`v1.25.0`), there is no separate
initial-sync-complete flag. Instead, `QuerySnapshotIterator.Next()` waits until
the watch has reached a consistent query state and returns a `QuerySnapshot`
for the complete current query result. On the first result, its changes
represent the current matching documents as additions; an empty query is a
valid empty baseline. The Firestore adapter marks that first returned snapshot
as `BaselineBatch`; subsequent snapshots are `DeltaBatch` values. This is a
per-projection reconciliation boundary, not a backend-wide readiness barrier.

Application operations still define their own local-miss behaviour, such as
source read-through or an explicit unavailable result. A projection may expose
readiness meaning that its first baseline has been atomically applied; this
does not imply that every projection has loaded or that remote ingestion never
lags.

The common runner has no `Ready` method or application-wide readiness channel.
If a projection needs to expose baseline-applied readiness, that remains an
adapter-level capability.

### 3.4 Make projection writes sequence-aware at the owning persistence boundary

Every committed change to an independently synchronizable local resource must
update its projection data and latest change version atomically with allocation
of the global sequence defined by CDD-0014. This applies to listener ingestion,
read-through population, and future backend mutations, not only to writes made
by the common runner.

The design should provide a transaction-aware projection write capability to
the projection store/adapter. It must preserve component ownership of schemas
and projection semantics; it must not add a duplicate event history or put
venue/user-specific methods in the Base Store.

The common runner only invokes the adapter's write operation. It does not
allocate sequence numbers itself, because source changes are not necessarily
identical to committed local projection changes.

Resource contracts and the staged implementation are recorded in
[CDD-0014](0014-sse-cursors.md#implementation-decisions-2026-10-22) and
[CDD-0016](0016-projection-sync-implementation.md). Venues retain all declared
schema fields. A combined user profile takes public fields from `user-public`
and private settings from `users`, without falling back to legacy private
duplicates, and includes endpoint changes in its version. The current schemas
no longer duplicate public fields in `users`. Join documents by their Firestore
document IDs; ignore legacy `uid` fields, including changes or removal. Comparisons
use field-specific normalisation, including set semantics for muted poll IDs;
local ingestion timestamps are excluded. Existing projection rows may be
discarded during migration with a generation reset, without deleting unrelated
SQLite data or durable work.

For combined user profiles, reconcile each source's contribution independently.
Retain a partial profile while either user document remains, and tombstone it
when neither remains. Endpoints alone cannot establish or resurrect a profile.
Push eligibility requires the locally present private user document and its
applicable opt-ins; transient catch-up gaps suppress delivery rather than
allowing orphan endpoints. Housekeeping can repair leftovers but is not part of
this eligibility boundary. Stricter coordinated deletion is a follow-up when
the backend becomes the sole user-deletion path.

### 3.5 Rebuild projections from a complete baseline on stream restart

When the runner opens a new stream after a failure, the source adapter supplies
a complete baseline before subsequent deltas. The adapter maps and validates
the complete baseline, then diffs its projected values against the committed
local projection within one SQLite transaction. It inserts new resources,
updates resources whose projected values changed, and removes resources absent
from the baseline. Readers continue to see the previous committed projection
until the transaction commits; on a cold start, the projection is empty and
existing per-operation local-miss behaviour applies. Subsequent change batches
are applied as deltas.

This recovery path identifies source-side deletions that occurred while the
stream was down: rows absent from the complete baseline are removed as part of
the diff. Projection data, sequence allocation, resource versions, and any
required deletion markers must be committed consistently in the same
transaction. The adapter remains responsible for comparing its projection's
domain values; the common runner does not clear or diff arbitrary tables.

The Firestore adapter must test that the pinned SDK's first returned query
snapshot is applied as a complete baseline and that later deltas are ordered
after it. If a source cannot provide this contract, that adapter must supply
another explicit reconciliation strategy rather than silently claiming
convergence.

### 3.6 Apply each batch transactionally and make replay idempotent

Apply each incoming baseline or delta batch using as few SQLite transactions as
practical: normally one transaction per batch. A failed batch rolls back as a
unit and must not advance the sequence or resource versions. The runner may
reopen the stream; its complete baseline then allows the projection to be
reconciled again.

The projection adapter compares normalized projected values, not raw source
documents. It allocates a sequence and updates a resource version only when the
local projection state actually changes. Replayed identical puts and deletes
of already-absent resources are no-ops and do not advance the sequence. A
baseline with no differences therefore commits no resource changes. This avoids
turning every reconnect into apparent changes to the entire projection.

### 3.7 Validate documents before projection application

The source/validation layer should validate remote documents against the
applicable schema before passing typed changes to the projection adapter. The
adapter should not duplicate schema validation, but remains responsible for
projection-specific conversion and semantic constraints.

An invalid document must not be silently skipped while its batch is treated as
a complete baseline: doing so could remove the existing projection row during
replacement. Baseline validation failure leaves the previously committed
projection intact and is reported. Handling invalid items in ordinary delta
batches, including whether they are skipped or fail/reopen the stream, must be
explicit and observable.

## 4. Out of Scope

- Client-facing SSE cursors, snapshots, subscriptions, and recovery envelopes.
- A durable event log or guaranteed replay of every remote mutation.
- A universal remote document model or universal projection schema.
- Listeners that transform source events into truths, notifications, or other
  asynchronous work rather than maintaining a local projection.
- A generic backend-wide readiness barrier.
- Guaranteeing convergence across separate backend instances with independent
  local databases.

## 5. Open Design Questions

Resolve these while implementing the first adapter, before treating the runner
as a stable shared contract:

1. **Retry policy:** Define the retryable operational errors, terminal
   configuration/permission errors, backoff policy, and how listener health is
   exposed. Source open/read errors close the current stream; they do not resume
   a failed iterator.
2. **Delta validation failure:** Decide which validation/conversion errors fail
   the batch and reopen from a baseline versus which can be skipped safely.
   Skipped invalid documents must be observable and must not be mistaken for a
   successfully reconciled baseline.
3. **Sequence scope:** Confirm that all projection writes contributing to the
   CDD-0014 synchronization contract use the same allocator and transaction
   boundary, including read-through population and backend mutations outside
   listeners.
4. **Migration:** Decide how to introduce the runner without changing current
   listener startup and application behaviour unexpectedly, and how to migrate
   venue cache as the first adapter.

## 6. Verification Criteria

The design and implementation must demonstrate:

- Stream stop and worker exit on cancellation and application shutdown.
- Retry after retryable stream-open and stream-read failures.
- No leaked or concurrently used stream iterators.
- Idempotent handling of replayed source changes.
- A complete baseline atomically replaces its projection, including removal of
   local rows absent from the baseline.
- Replaying an identical baseline or delta does not advance resource versions or
   the global sequence; only projection differences do.
- Readers observe either the previous committed projection or the replacement,
   never an empty/partially rebuilt projection during reconnect.
- A failed batch leaves projection rows, sequence state, resource versions, and
   deletion markers unchanged.
- Atomic projection and sequence-version writes for every supported writer.
- Projection-specific behaviour for malformed documents and removals is tested.
- The Firestore adapter verifies baseline-before-delta ordering and full baseline
   completeness.
- No application-wide readiness dependency on Firebase listener catch-up.
- Reuse of the same stream runner by at least two projection adapters before
  considering its contract stable.

## 7. Migration Sketch

1. Define the runner contract and focused tests for stream lifecycle, retry, and
   cancellation.
2. Introduce the transaction-aware local projection write boundary required by
   CDD-0014, without moving projection-specific schema ownership into the Base
   Store.
3. Adapt venue cache while preserving its current source mapping and local-miss
   behaviour, and implement atomic baseline replacement.
4. Adapt a second projection with meaningfully different payload or stream
   shape, such as notification profiles, to validate what should be shared.
5. Keep event-producing listeners on their existing path unless a separate CDD
   establishes that they share the same semantics.

## 8. Implementation Status (2026-10-23)

The shared runner is used by venue and notification-profile listeners. Venue
stores now use shared transaction-aware sequence allocation, projection-owned
versions and retained tombstones for listener batches and read-through writes.
Venue baselines reconcile only real projected differences. Store and listener
tests verify replay, offline deletions, rollback, atomic reader visibility and
baseline-before-delta ordering without changing the runner contract.

Malformed venue baselines fail without signalling readiness or replacing local
state. The existing venue listener policy for malformed deltas remains a logged
skip; notification profiles are not yet sequence-aware. The broader source
validation boundary and retry-classification questions remain open.

The Firestore emulator baseline-contract test subsequently passed against an
empty disposable emulator. Full tests, race, vet and formatting checks passed;
the full race gate preceded emulator execution. Pinned staticcheck remains
blocked by compiler export-data incompatibility. The combined user-profile
implementation is planned in section 6 of CDD-0016; it is not yet implemented.
See [CDD-0016](0016-projection-sync-implementation.md) for detailed verification
and the remaining work. Cursors and SSE remain unimplemented.
