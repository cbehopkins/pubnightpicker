# CDD: Projection Synchronisation Implementation Plan

**Status:** Draft plan - core resource decisions agreed; user lifecycle details pending
**Date:** 2026-10-22
**Related:** [Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Reusable Projection Listeners](0015-projection-listeners.md)

## 1. Scope and Starting Point

Implement sequence-aware local projections, replay-idempotent reconciliation,
cursor recovery and, subsequently, SSE delivery in independently testable stages.

The shared projection-watch runner and atomic batch writes already exist for
venues and notification profiles. Baselines currently clear and repopulate each
projection. Global sequence allocation, resource versions, tombstones and cursors
are not yet implemented.

The first implementation milestone is sequence-aware venue reconciliation
(stages 2-4), after resolving the applicable stage 1 decisions. It does not change
the runner API, retry policy or HTTP endpoints. User-profile decisions need not
block venue work or shared sequence infrastructure.

## 2. Agreed Constraints

- One venue resource per venue ID; one combined user profile per user ID.
- Retain every explicitly declared schema field, including venue amenities and
  notes. `user-public` owns fields overlapping with `users`; push endpoint
  changes advance the owning profile version without exposing credentials.
- Do not fall back to private duplicate fields if public data is absent or
  null. Private duplicates are deprecated: migrate frontend reads to
  `user-public`, then remove duplicates through coordinated schema/data cleanup.
  The plan does not authorise immediate schema removals or frontend changes.
- Explicitly declared Firestore schema fields define domain data under
  consideration; arbitrary extra fields and ingestion bookkeeping do not.
- Projection stores own comparisons, resource versions and deletion markers.
  Shared infrastructure owns global sequence allocation.
- All participating writers use the same transaction boundary, including
  listeners, read-through population and direct local mutations.
- Identical projected values and deletion of already-absent resources are
  no-ops. Failed transactions advance neither data nor committed versions.
- V1 retains tombstones indefinitely. Cleanup and a corresponding cursor
  retention boundary are deferred to V2.
- Database generation identity persists across ordinary restarts and changes
  when projection history is rebuilt. A generation mismatch requires refresh.
- Existing projection rows may be discarded and repopulated during migration;
  no backfill is required. Reset projection state and generation atomically,
  preserving unrelated data and durable work.
- Use field-specific defaults and preserve meaningful missing/null distinctions.
  Compare muted poll IDs as a set and other arrays by their domain semantics.
- SQLite remains authoritative for the state exposed by local reads and
  streaming; a local snapshot does not promise Firestore catch-up.

## 3. Implementation Stages

### Stage 1: Finalise Resource Contracts

The following decisions are recorded in CDD-0014. Resolve the remaining details
before implementing the affected resource:

1. **Field coverage (agreed):** Retain all explicitly declared schema fields,
  not only today's cache subsets.
2. **User sources (partly agreed):** `user-public` is authoritative for overlapping
  fields, with no private fallback for missing/null public data; private
  duplicates are deprecated pending frontend migration. Endpoint changes
  participate in the combined version. Removing `user-public` removes its
  contributed fields without removing remaining private data. Still define
  combined profile existence and authorised public/private representations.
  A combined identity does not grant access to private fields or push
  credentials.
3. **Normalisation (agreed direction):** Use field-specific defaults, preserve
  meaningful null/missing distinctions and compare muted poll IDs as a set.
  Specify concrete field mappings with each adapter's tests. Ingestion
  timestamps are not domain changes. The user schema now declares
  `pushPreferences.eventChatMutedPollIds`.
4. **Migration (agreed):** Discard and reconcile affected projection data rather
  than backfilling old cache rows. Atomically change the generation with the
  projection reset. Preserve unrelated SQLite data and durable application
  work; do not delete the whole database.

**Exit:** Applicable resource contracts and migration behaviour are explicit in
CDD-0014/0015; unresolved user-profile decisions remain visibly pending.

### Stage 2: Add Shared Sequence Infrastructure

Provide persistent generation identity, global sequence allocation within the
caller's SQLite transaction and a consistent high-water-mark read. Keep resource
schemas and semantics out of Base Store.

**Verify:** Concurrent writers, rollback, restart persistence, new-generation
identity and consistent reads. Failed transactions must not advance the
committed high-water mark.

### Stage 3: Make Venue Writes Sequence-Aware

Expand the model according to stage 1. Route `Put`, `Delete`, batch ingestion and
read-through population through shared venue-owned transactional write logic.
Record versions only for actual changes; retain versioned tombstones on deletion
and assign a newer live version on recreation.

**Verify:** Additions, modifications, identical puts, repeated deletes,
recreation, existing-data migration and rollback.

### Stage 4: Replace Baseline Rebuilds With Diffing

Validate and map each complete baseline, then compare it with committed venue
state in one transaction. Insert additions, update differences and tombstone
resources absent from the baseline. Leave identical resources untouched. Apply
deltas through the same comparison/write logic. Preserve readiness and error
reporting.

**Verify:** Identical reconnects, mixed changes, empty baselines, invalid input,
rollback of data and metadata, and atomic visibility to readers.

**Milestone:** Venue reconciliation produces no artificial changes on reconnect;
all supported venue writers participate in global ordering.

### Stage 5: Adapt Combined User Profiles

Implement the agreed source composition and normalisation with one version per
user profile. Cover preferences, endpoints and local endpoint deactivation as
required by the contract. Independent Firestore streams converge on locally
committed state; they do not provide an atomic cross-source remote snapshot.

**Verify:** Changes from every participating source, baseline reconciliation,
replay, deletion, normalisation, private/public access boundaries and existing
notification behaviour.

### Stage 6: Implement Cursor and Recovery Reads

Specify opaque encoding and stream scope. Validate generation and sequence
position; return consistent snapshots or latest-state incremental changes with
deletions. Define pagination boundaries and full-refresh responses. Resolve
durable authorisation invalidation before exposing protected views.

**Verify:** Concurrent updates, pagination, restart/rebuild, unusable cursors,
deletions and visibility changes. A snapshot and cursor describe the same state.

### Stage 7: Add SSE Delivery

Finalise OpenAPI envelopes, then implement snapshot-to-live handover,
post-commit notifications, reconnect recovery, cancellation and slow-client
handling. Include the authorisation and London-midnight reset rules from
CDD-0013 where applicable. Notifications remain ephemeral, not a durable queue.

**Verify:** No missed final state during handover, disconnect/reconnect recovery,
reset behaviour, backpressure and reuse across the intended view types.

## 4. Validation and Delivery

Use focused tests after each implementation change. At milestones, run the
repository gates: `go test -race ./...`, `go vet ./...`,
`go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...` and `gofmt -l .`.
Run the applicable Firestore emulator tests when verifying source contracts;
report emulator coverage separately if the emulator is unavailable.

Keep changes scoped to one stage or a small dependency-coherent part of a stage.
Do not silently resolve pending contracts in code. Update this plan and the
owning CDD as decisions are agreed and milestones are verified.