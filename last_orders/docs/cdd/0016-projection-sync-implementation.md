# CDD: Projection Synchronisation Implementation Plan

**Status:** Draft plan - resource ownership and lifecycle decisions agreed
**Date:** 2026-10-22
**Related:** [Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Reusable Projection Listeners](0015-projection-listeners.md)

## 1. Scope and Starting Point

Implement sequence-aware local projections, replay-idempotent reconciliation,
cursor recovery and, subsequently, SSE delivery in independently testable stages.

The shared projection-watch runner and atomic batch writes exist for venues and
notification profiles. Shared sequence metadata and sequence-aware venue writes
are implemented as of 2026-10-23. Venue baselines now compare existing values and
retain versioned tombstones; remaining baseline visibility and listener-level
verification belongs to Step D. Notification profiles still clear and repopulate
baselines without sequence allocation. Cursors remain unimplemented.

The first implementation milestone is sequence-aware venue reconciliation
(stages 2-4); its prerequisite resource decisions are agreed. It does not change
the runner API, retry policy or HTTP endpoints. User-profile decisions need not
block venue work or shared sequence infrastructure.

## 2. Agreed Constraints

- One venue resource per venue ID; one combined user profile per user ID.
- Retain every explicitly declared schema field, including venue amenities and
  notes. `user-public` owns public fields; `users` owns private settings. Push endpoint
  changes advance the owning profile version without exposing credentials.
- Do not fall back to private duplicate fields if public data is absent or
  null. The schemas checked on 2026-10-23 no longer declare private duplicates.
  Frontend and stored-document cleanup are outside this backend plan.
- Join user documents by their Firestore document ID. Ignore legacy `uid`
  fields, including their removal, for projection values and change detection.
  Neither current user schema declares a `uid` property.
- Retain partial profiles while either public or private user document remains.
  Remove only a deleted source's contributed fields; tombstone the profile when
  neither remains. Endpoints alone cannot create or retain a live profile.
- Push eligibility requires the private user document, applicable opt-ins and
  an eligible endpoint in committed local state. Suppress pushes while private
  user data is missing, including transient creation/catch-up gaps. Housekeeping
  is cleanup, not a prerequisite for excluding orphan endpoints from delivery.
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
2. **User sources (agreed):** `user-public` owns
  public fields and `users` owns private settings; the current schemas no
  longer duplicate these fields. There is no private fallback for missing/null
  public data. Use document IDs, not legacy `uid` values. Endpoint changes
  participate in the combined version. Removing `user-public` removes its
  contributed fields without removing remaining private data. Either user
  document sustains a partial profile; neither means a versioned deletion.
  Endpoints cannot sustain a profile and private-user absence prevents push
  eligibility. Authorised public/private API representations still need to be
  specified before exposure. A combined identity does not grant access to
  private fields or push credentials.
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
CDD-0014/0015. Ownership and lifecycle are agreed; concrete normalisation
mappings accompany adapter tests and API visibility contracts precede exposure.

### Stage 2: Add Shared Sequence Infrastructure

**Implemented:** Step A completed on 2026-10-23. Projection integration remains
part of stages 3-4.

Provide persistent generation identity, global sequence allocation within the
caller's SQLite transaction and a consistent high-water-mark read. Keep resource
schemas and semantics out of Base Store.

**Verify:** Concurrent writers, rollback, restart persistence, new-generation
identity and consistent reads. Failed transactions must not advance the
committed high-water mark.

### Stage 3: Make Venue Writes Sequence-Aware

**Implemented:** Steps B and C completed on 2026-10-23. The first milestone still
requires the remaining Step D verification and milestone gates.

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
Retain partial public/private profiles, prevent endpoints from resurrecting
deleted profiles and require private-user presence for push eligibility.

**Verify:** Changes from every participating source, baseline reconciliation,
replay, deletion, normalisation, private/public access boundaries and existing
notification behaviour. Include endpoints-before-user arrival, public-only and
private-only profiles, removal of either/both user documents, orphan endpoint
updates and restoration after transient catch-up gaps.

**Follow-up:** Make backend-owned user deletion an early architecture feature,
then tighten the transitional partial-profile/orphan rules. Housekeeping may
repair leftovers in the meantime; do not depend on it for push eligibility.

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

## 5. First Milestone: Execution Checklist

**Scope:** Stages 2-4 only. Implement the shared persistence foundation and one
fully participating projection, venues. Do not begin user-profile adaptation,
cursor encoding, HTTP endpoints or SSE dispatch in this milestone.

Each step includes focused validation before proceeding. Checklist items track
implementation, not approval of the already agreed decisions.

### Step A: Shared Transaction Metadata

- [x] Extend `internal/lastorders/basestore/store.go` with a component-neutral
  metadata schema holding persistent database generation and global sequence.
- [x] Initialise metadata without changing generation on normal startup. Ensure
  concurrent initialisation cannot replace an established generation.
- [x] Provide sequence allocation using the caller's `*sql.Tx`, and metadata
  reads that can use the caller's read transaction. Do not introduce an
  independently committing allocator or resource-specific Base Store methods.
- [x] Provide a transaction-aware generation-change operation for projection
  rebuilds. A generation change does not itself reset unrelated projection
  versions or permit the allocator to move behind them.
- [x] Add focused Base Store tests for initialisation, reopen, concurrent writes,
  allocation rollback, generation rollback and consistent metadata reads.

**Check:** Rolled-back allocations never advance the committed high-water mark;
committed allocations across writers are globally ordered.

**Implemented API:** `ReadSyncState`, `NextSequence` and `ChangeGeneration`
require the caller's `*sql.Tx`; none commits independently. `SyncState` returns
generation and sequence from one read snapshot. Generation changes preserve the
sequence. A singleton `projection_sync_state` row is initialised without
replacing existing metadata. Exhausted sequences fail rather than overflow.

**Verified (2026-10-23):** Six focused Base Store tests, the focused race run,
Base Store vet checks and the full VS Code test run (543 passed). Existing venue
and notification store/service checks also passed. Tests cover rolled-back and
committed metadata, separate-reader visibility, stable read snapshots with
resource data, persistent identity, new-database identity, concurrent
initialisation/allocation and sequence exhaustion. The full milestone's race,
vet and staticcheck gates remain for completion of stages 3-4.

### Step B: Complete Venue Values and Migration

- [x] Extend `internal/lastorders/components/venuecache/types.go` and its mapping
  tests to cover all declared fields from the current pubs schema, including
  notes and amenities. Preserve existing consumer-facing behaviour unless the
  agreed field contract requires a change.
- [x] Define and test concrete optional-value and recurrence normalisation so
  equivalent projected values compare equal. Exclude undeclared source fields
  from change detection. Do not invent defaults that erase meaningful absence.
- [x] Extend the venue-owned schema in `store.go` for full projected values,
  resource versions and retained deletion markers. Ordinary reads must exclude
  tombstones and retain the existing cache-miss contract.
- [x] Introduce a one-time transactional migration that discards affected old
  projection rows, installs the new schema and changes database generation.
  Migration detection must not clear the projection on subsequent startups.
- [x] Test migration from the existing schema, repeat startup, rollback and
  preservation of unrelated SQLite data. Do not delete the database file.

**Check:** Both fresh and previously deployed databases reach the supported
schema; ordinary restarts retain current projection data and generation.

**Implemented (2026-10-23):** Notes and all six amenity flags preserve absence
using nullable fields; explicit empty notes and false flags round-trip through
SQLite. Existing string fields retain their previous missing-to-empty mapping.
Recurrence preserves absent fields, empty objects/arrays and explicit numeric
values; weekdays are sorted and deduplicated without changing the source data.
Invalid recurrence values, including undeclared properties and null weekdays,
fail conversion. Undeclared top-level document fields do not affect projection
values. The schema's `dog_friend` and `beer_gerden` spellings map to the local
`DogFriendly` and `BeerGarden` fields.

The venue-owned schema marker records the migration once. Rebuilding a legacy
venue table and changing database generation commit atomically; fresh creation
and repeat startup preserve generation. Unrelated data and the existing global
sequence are retained. Unknown schema versions fail without destroying data.
Ordinary reads hide rows marked deleted. At the Step B checkpoint, version and
deletion columns existed but writes were not yet sequence-aware; Step C below
supersedes that transitional behaviour.

**Verified:** Direct package tests, focused venue/Base Store race and vet checks,
and the full VS Code test run (544 passed). Migration tests cover reopen,
preserved unrelated data, unknown schemas and an injected failure after the
generation update proving rollback of schema, rows, marker and generation.

### Step C: One Venue Write Boundary

- [x] Keep one initial migration for the undeployed changes. Its final schema
  requires positive versions without a zero default. Legacy deployed cache
  rows are discarded atomically by that migration; no second migration is
  needed solely for intermediate development versions. Earlier local
  development caches may need rebuilding before testing the revised schema.
- [x] Refactor venue `Put`, `Delete` and `ApplyBatch` to call venue-owned helpers
  using the same transaction for comparison, allocation and persistence.
- [x] Allocate a version only for an actual insertion, value change, removal or
  recreation. Keep identical puts and already-absent deletes as no-ops.
- [x] Confirm `Service.Get` read-through still populates through `Store.Put`;
  there must be no unversioned alternative write path.
- [x] Test live-row versions, deletion markers, identical replay, recreation,
  mixed writes and whole-transaction rollback. Keep deleted values out of
  ordinary venue reads.
- [x] Extend existing service tests to verify read-through participates in
  versioning without changing its current remote-fetch and failure behaviour.

**Check:** Every supported venue writer commits data and its version together;
no resource version can become visible ahead of its committed sequence.

**Implemented (2026-10-23):** Puts and deletes delegate to the atomic batch
boundary. Normalised projected values are compared by value, including nullable
fields, before sequence allocation. Removals retain a versioned tombstone;
recreation assigns a newer live version. Invalid inputs fail before writes.
Read-through still returns the authoritative source value if population fails,
without committing an orphan sequence allocation.

The baseline-wide delete was also replaced here because it would bypass this
write boundary and erase retained tombstones. Baselines remove absent live rows
through the same deletion helper and upsert only actual differences; replayed
identical baselines preserve versions. Step D still supplies the broader
baseline/source and reader-visibility verification.

**Verified:** Full VS Code test run (563 passed), focused venue/Base Store race
and vet checks, migration and schema checks, replay and recreation tests, and
injected persistence failure after allocations proving whole-batch rollback.
Concurrent identical puts allocate once; concurrent repeated deletes allocate
one tombstone version, using the application's WAL/busy-timeout/immediate-lock
settings. The shared runner and HTTP contracts are unchanged.

### Step D: Baseline Reconciliation

- [x] Replace the baseline-wide delete in venue `ApplyBatch` with comparison
  against the complete incoming projected set within one transaction.
- [x] Insert new venues, update changed values, tombstone missing live venues
  and leave identical live rows and existing tombstones untouched.
- [ ] Apply deltas through the same venue write helpers. Validate the whole
  batch so any mapping, validation or persistence failure preserves the
  previous committed projection and sequence metadata.
- [ ] Extend existing venue store and listener tests for identical reconnects,
  empty baselines, offline deletions, mixed differences, replay, failures and
  baseline-before-delta application.
- [ ] Verify separate readers see the previous or next complete committed
  projection, never a partially reconciled baseline. Test writer contention
  with deterministic coordination rather than timing-only sleeps.
- [ ] Preserve existing baseline-applied readiness, cancellation and error
  reporting. Keep the shared runner contract unchanged.

**Check:** Replaying an identical baseline or delta leaves the global sequence
and all venue versions unchanged. A failed batch also preserves tombstones.

### Milestone Completion

- [ ] Run focused Base Store, venue store/service and relevant listener tests.
- [ ] Run the milestone gates in section 4 and report any unavailable emulator
  coverage or unrelated failures explicitly.
- [ ] Update this checklist and the owning CDDs with verified implementation
  status. Do not mark cursor recovery or SSE as implemented.

**Deliverable:** A complete, sequence-aware venue projection with atomic,
idempotent reconciliation, and reusable sequence infrastructure ready for the
combined user-profile stage.
