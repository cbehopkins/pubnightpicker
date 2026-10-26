# CDD: Projection Synchronisation Implementation Plan

**Status:** Venue milestone implemented; Stage 5 steps E-H implemented; staticcheck gate blocked
**Date:** 2026-10-22
**Related:** [Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Reusable Projection Listeners](0015-projection-listeners.md)
[Reusable SSE Stream Prototype](0017-sse-stream-prototype.md)

## 1. Scope and Starting Point

Implement sequence-aware local projections, replay-idempotent reconciliation,
cursor recovery and, subsequently, SSE delivery in independently testable stages.

The shared projection-watch runner and atomic batch writes exist for venues and
notification profiles. Shared sequence metadata and sequence-aware venue writes
are implemented as of 2026-10-23. Venue baselines now compare existing values and
retain versioned tombstones; remaining baseline visibility and listener-level
verification was completed in Step D, including the Firestore emulator source
contract check. Notification profile stores now reconcile per-source baselines
and allocate combined profile versions atomically. Their listener ingests raw
private-user, public-user and endpoint contributions through three independent
streams. Stage 5 has the execution checklist in section 6; final eligibility and
integration verification remains in Step I.
Cursors remain unimplemented.

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
has the staticcheck toolchain blocker recorded below; Step D checks are complete
including the subsequent Firestore emulator run.

Expand the model according to stage 1. Route `Put`, `Delete`, batch ingestion and
read-through population through shared venue-owned transactional write logic.
Record versions only for actual changes; retain versioned tombstones on deletion
and assign a newer live version on recreation.

**Verify:** Additions, modifications, identical puts, repeated deletes,
recreation, existing-data migration and rollback.

### Stage 4: Replace Baseline Rebuilds With Diffing

**Implemented and locally verified (2026-10-23):** Step D is complete. The
Firestore source contract test subsequently passed against an empty emulator.

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

**Implementation status (2026-10-23):** Steps E-H typed contributions, comparison,
strict conversion, schema migration, typed storage and combined profile
sequencing are implemented, including raw ingestion from all three streams.
The remaining eligibility/integration verification and stage gates are in Step I.

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
position; return consistent full snapshots on every connection. Define
pagination boundaries and full-view responses. Re-authorise each connection.
After a committed permission or membership change, signal active streams for
that UID through an in-memory hub. After a visibility change, signal streams
in the affected authorization scope, including anonymous streams. Send a
generic reset and close so clients reconnect.

If a later version enables incremental cursor resume, bind a durable per-UID
authorization epoch into each cursor and compare it before returning deltas.
Advance the epoch atomically with the local authorization projection; retain it
across user deletion and never reset or reuse it within a database generation.
A mismatch requires a full, currently authorised snapshot. The active-stream
hub remains responsible for promptly closing already-open connections.

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
- [x] Apply deltas through the same venue write helpers. Validate complete
  baselines before applying them; failed store batches preserve the previous
  projection and metadata. Preserve the existing listener policy of logging
  and skipping malformed delta documents, applying only valid mutations.
- [x] Extend existing venue store and listener tests for identical reconnects,
  empty baselines, offline deletions, mixed differences, replay, failures and
  baseline-before-delta application.
- [x] Verify separate readers see the previous or next complete committed
  projection, never a partially reconciled baseline. Test writer contention
  with deterministic coordination rather than timing-only sleeps.
- [x] Preserve existing baseline-applied readiness, cancellation and error
  reporting. Keep the shared runner contract unchanged.

**Check:** Replaying an identical baseline or delta leaves the global sequence
and all venue versions unchanged. A failed batch also preserves tombstones.

**Verified (2026-10-23):** Mixed baseline additions, changes, removals and
recreation; identical and empty replays; failed baseline validation; persistence
failure after earlier writes and allocations; and preservation of existing
tombstones. A test-only SQLite trigger pauses a real transaction so separate
readers can verify old committed rows and metadata during the write, stable
read snapshots after commit, and complete new snapshots afterwards. A competing
writer receives the next sequence after commit, or after rollback without
retaining abandoned allocations. No production test hooks or reflection were
introduced.

The real listener reconnect test is channel-coordinated and verifies full
baseline application, offline deletion, ordered and replayed deltas, readiness,
stream stop and cancellation. Malformed baselines preserve local state and do
not signal readiness; malformed deltas retain the existing observable skip
policy. Runner APIs, retry policy and HTTP behaviour remain unchanged.

**Emulator verification:** `TestFirestoreWatchCompleteBaselineBeforeDeltas` covers empty
and populated initial queries, ordered deltas and a complete new baseline on
reopen. Initially skipped, it subsequently passed against the empty emulator at
`127.0.0.1:8080`. The recurrence, venue-cache and test-email packages passed
together with the emulator enabled after fixing test isolation and the missing
recurrence SQLite driver import. These tests supply their own fixtures; use a
disposable empty emulator, not an imported example dataset.

### Milestone Completion

- [x] Run focused Base Store, venue store/service and relevant listener tests.
- [ ] Run the milestone gates in section 4 and report any unavailable emulator
  coverage or unrelated failures explicitly.
- [x] Update this checklist and the owning CDDs with verified implementation
  status. Do not mark cursor recovery or SSE as implemented.

**Gate results (2026-10-23):** Full VS Code test run: 579 passed. Full repository
`go test -race ./...`, `go vet ./...` and `gofmt -l .` passed. Pinned staticcheck
`v0.8.1` failed before analysing the code because its importer supports Go export
data version 4, while this compiler emits version 5. This is a toolchain
compatibility blocker, not a passing staticcheck result; resolve it before
claiming all milestone gates have passed. The subsequent emulator-enabled
package checks passed as noted above; the earlier full race result was obtained
without emulator coverage. Stages 5-7, cursor recovery and SSE are not implemented
by this milestone.

**Deliverable:** A complete, sequence-aware venue projection with atomic,
idempotent reconciliation, and reusable sequence infrastructure ready for the
combined user-profile stage.

## 6. Stage 5: Combined User Profile Execution Checklist

**Scope:** Extend the existing notification-profile component into the agreed
combined local user projection. Reuse Base Store sequence allocation and the
existing projection-watch runner. Keep notification callers working through
their established service interfaces. Do not add HTTP endpoints, cursor
encoding, SSE delivery, remote user deletion or housekeeping jobs here.

The current implementation watches `users` and the `push_endpoints` collection
group, stores preferences and endpoints separately, and rewrites baselines.
It does not yet retain `user-public` documents or version the combined resource.
Relevant code lives in `components/notificationprofile`, its database listener,
the existing fake-source package and app notification tests.

### Step E: Typed Source Contributions and Equality

**Field semantics agreed (2026-10-23):**

- Preserve missing, explicit null and explicit values as distinct states for
  public `name`, `photoUrl` and `votesVisible`; moving between these states is
  a profile change. `customPhotoUrl` permits a missing field or a string, not
  null. Never substitute legacy private values.
- Preserve whether private settings were supplied. Apply notification defaults
  only when reading or selecting recipients. Missing-to-explicit-default is a
  profile change even if delivery eligibility is unchanged. Missing
  `webPushEnabled`, `globalChat`, `eventChat`, `notificationEmailEnabled` and
  `openPollEmailEnabled` are effectively false; missing `pollOpens` and
  `pollCompletes` are effectively true. The two email settings are boolean
  enablement flags, not the email-address fields.
- Missing muted poll IDs and an empty array represent the same empty set.
  Ignore ordering and duplicates, but do not trim or otherwise rewrite IDs.
  Wrongly typed entries are invalid rather than silently discarded.
- Reject a complete baseline containing an invalid document without changing
  committed state. For an invalid delta document, retain that source's existing
  contribution, log the rejection and apply other valid changes in the batch.
  Never turn invalid values into defaults. Persistence failures still roll back
  the entire store batch, including sequence allocations and resource versions.
- Endpoint `endpoint`, `p256dh`, `auth` and `active` participate in the combined
  profile version. Endpoint and owner IDs come from the document path; credentials
  remain private. Missing `active` is effectively false at the selector boundary,
  without erasing source-field presence during comparison.
- Endpoint timestamps (`createdAt`, `lastSeenAt`, `disabledAt`) are internal
  operational metadata, excluded from the synchronised profile representation
  and its version comparison. Timestamp-only updates must not allocate a
  sequence. An `active` change still does. `disabledAt` may be null when the
  frontend registers or reactivates a subscription.
- The endpoint schema is now registered as
  `schema/firestore/collections/push_endpoints.schema.json` in the sibling schema
  project. It also documents existing nullable `userAgent`, `platform` and
  `appVersion` fields from the frontend writer. These descriptive fields are
  excluded from the synchronised profile representation and its version
  comparison, like the operational timestamps. They are not required for push
  delivery; changes to them alone must not allocate a sequence.
- Unversioned endpoint metadata must not be included in cursor-synchronised
  representations. If a later client needs to synchronise these fields, first
  add them to the versioned contract and update comparison and recovery tests.
  This is a deliberate scope exclusion, not permission to return changing
  resource fields under an unchanged version.

The following boxes track implementation, not agreement on these rules.

- [x] Define typed private-user and public-user contributions, identified by
  document ID. Retain every declared field in the current schemas, including
  private email, arrival time, diagnostic request/acknowledgement and notification
  settings, and public name, photos and vote visibility. Use the registered
  endpoint schema, excluding the agreed operational timestamps and descriptive
  client metadata from the synchronised representation.
- [x] Ignore legacy `uid` and undeclared top-level fields. Never fall back from
  missing/null public values to legacy private duplicates. Distinguish missing,
  explicit null where allowed, false and empty values where meaningful.
- [x] Define normalisation and explicit typed equality for all contributions
  and endpoints. Muted poll IDs compare as a sorted, deduplicated set;
  ingestion timestamps do not affect equality or versions. Do not use reflection
  in implementation or tests.
- [x] Provide separate effective notification views with the existing defaults,
  ready for use at the selector/read boundary in steps F-G. Do not persist
  duplicate raw and effective copies of the same source values.
  Retaining diagnostic fields must not emit work or duplicate the test-email
  listener's responsibilities.
- [x] Extend existing mapping/equality tests for every declared field,
  presence/null distinctions, independent pointer values, defaults, set replay,
  invalid values and ignored legacy fields.

**Check:** A real change to any retained contribution is detectable, while
equivalent source replays cannot manufacture profile changes.

**Implemented (2026-10-23):** `PrivateUserContribution`,
`PublicUserContribution` and `EndpointContribution` retain the agreed
synchronisation fields and expose explicit typed equality. Public nullable
fields use `NullableField` to preserve missing/null/value states. Other optional
fields preserve presence through pointers. Muted IDs normalise as a set without
rewriting IDs or mutating source slices. Endpoint operational/descriptive
metadata and legacy IDs are excluded from the contribution types.

`PrivateUserContribution.Preferences` and `EndpointContribution.Endpoint` derive
the established effective views without changing source presence. Existing
document adapters delegate to strict conversion and these views: valid inputs
retain their previous effective defaults, while malformed supplied values now
fail rather than being defaulted or silently discarded. Existing tests that
expected permissive filtering/defaulting were updated for the agreed policy.
No diagnostic work is emitted by conversion.

**Transition boundary at the Step E checkpoint:** The store still persisted
the old effective preference/endpoint views. Steps F-G replace that persistence with
source contributions and apply defaults at the selector/read boundary; this
checkpoint did not preserve raw presence across database round-trips or version
the combined profile. Step F below adds typed storage; public-user streaming
and combined sequencing remain pending.

**Verified:** Full VS Code test run: 597 passed. Notification-profile component
and listener race tests and focused vet checks passed. Mapping/equality tests
cover all retained fields, nullable public states, missing-to-explicit defaults,
independent pointers, unordered/duplicate muted IDs, source immutability,
invalid values, ignored endpoint metadata and ingestion timestamps. No
reflection is used. The known staticcheck toolchain blocker remains unchanged.

### Step F: Component-Owned Schema and Initial Migration

- [x] Define storage for public/private contributions and endpoints, plus one
  combined profile version or retained tombstone per user ID. Derive source
  presence from contribution-row existence, including empty documents; do not
  duplicate listener catch-up state or add persisted readiness flags.
- [x] Keep schema ownership and migration SQL in the notification-profile
  component, with a small constructor and separate migration methods/tests as
  established for venues. Keep the undeployed work as one initial migration,
  not a history of intermediate development schemas.
- [x] Atomically discard legacy notification projection data, install the new
  schema and change database generation when existing projection history is
  reset. Preserve venue data/versions, global sequence and unrelated durable
  application work. Fresh creation and ordinary reopen preserve generation.
- [x] Test fresh creation, legacy rebuild, repeat startup, unknown schema
  rejection and injected migration failure. Existing development caches may
  need rebuilding as the initial undeployed migration evolves.

**Check:** Source presence and combined deletion can be represented without
inventing account existence from endpoint rows or deleting unrelated domains.

**Implemented (2026-10-23):** `migrations.go` owns a single initial migration
and `migrations_test.go` verifies its lifecycle separately from store behaviour.
`New` validates its dependency, migrates and constructs the store. Legacy
notification tables are discarded transactionally with a generation change;
venue projection values/versions, the global sequence and unrelated durable
data are preserved. Fresh creation and normal reopen retain generation.

Private source columns and endpoint `active` are nullable, without persisted
effective defaults. Public nullable fields store per-field presence alongside
nullable values to distinguish missing/null/value; these are source semantics,
not listener-readiness flags. Contribution row existence identifies source
presence, including empty documents. `notification_profiles` holds one positive
version and deletion marker per user ID, with a change-query index and no
foreign-key requirement that would prevent retained tombstones or orphan
endpoint storage.

`contribution_store.go` provides typed reads and writes usable with either the
database or a caller-owned transaction. Round-trips preserve every retained
field and replace removed fields correctly. Ordinary preference reads derive
effective defaults; recipient queries apply missing poll/chat defaults without
writing them back. Existing preference-only writes preserve email, arrival and
diagnostic source fields outside their scope. No duplicate effective projection
is persisted.

**Transition boundary at the Step F checkpoint:** Listener batches still carry legacy effective
preference/endpoint views. Those compatibility writes remain usable, but cannot
recover source presence already erased by the old adapter API. Steps G-H must
route raw contributions through the sequence-aware boundary and all three
streams. Step F did not automatically populate `notification_profiles` or
allocate profile versions, reconcile profile lifecycle or add a public-user
watch. Earlier development databases may need rebuilding as this undeployed
initial migration evolves; no second development-only migration was added.

**Verified:** Full VS Code test run: 625 passed. Notification component/listener
race tests, focused vet and diagnostics passed. Tests cover fresh/legacy schema,
repeat startup, close/reopen, unknown version rejection, injected rollback of
rows/schema/generation, preserved venue and durable data, typed source
round-trips, read-only defaults, public-only/orphan push exclusion, caller-owned
transaction rollback and invalid schema states. The profile-version table can
retain a tombstone independently of contribution rows. No reflection is used;
the known staticcheck toolchain blocker remains unchanged.

### Step G: One Transactional Profile Write Boundary

- [x] Route private/public writes, preference puts/deletes, endpoint
  puts/deletes, batch ingestion and `SetEndpointActive` through component-owned
  transactional helpers. Keep `InvalidateEndpoint` remote-first, followed by
  this local boundary; a later identical listener update must be a no-op.
- [x] Compare each affected user's combined state before and after a batch.
  Allocate one new profile version per actually changed live resource in that
  transaction, not one version per endpoint or source row. Commit contributions,
  endpoints, profile versions/tombstones and allocations together.
- [x] Retain a partial live profile while either user document exists. Removing
  one source removes only its contribution. Removing the last source writes a
  newer tombstone; repeated absent deletes do not allocate. Reappearance creates
  a newer live version.
- [x] Endpoint rows alone must not establish or resurrect a profile. Retain
  orphan endpoints only as internal data pending cleanup; their changes do not
  advance an absent profile or its tombstone. When a user document appears,
  include the currently committed endpoint contribution in its live profile.
- [x] Test all writer paths, changed presence even for empty documents,
  multiple endpoints belonging to one user in a batch, multiple affected users,
  replay, deletion, recreation and rollback after allocation. Verify venue and
  user changes share the same global allocator.

**Check:** Each profile version describes the combined locally committed
resource, not a source notification or an independently versioned endpoint.

**Implemented (2026-10-23):** `profile_write.go` owns the transaction boundary.
It snapshots each affected user's private/public contributions and sorted
endpoints, applies the batch, then compares final combined values. Each changed
owner receives one global sequence and profile version; net-zero batches and
identical replay do not allocate. Contributions, endpoint rows, versions and
tombstones roll back together. Identical source rows are not rewritten, so
their ingestion timestamps are preserved too.

Typed private/public/endpoint batch APIs and existing preference/endpoint APIs
use this boundary. Preference-only writes preserve unrelated private fields.
`SetEndpointActive` participates; `InvalidateEndpoint` remains remote-first,
with a later identical source update treated as a no-op. Source baselines
collect existing owner IDs before removing missing rows, reconcile only their
own tables and preserve other contributions. This storage-level baseline work
is shared with Step H; no third stream is added here.

A profile remains live while either user contribution exists. Removing the last
contribution writes a retained tombstone; orphan endpoint changes neither create
a profile nor advance its tombstone. Reappearance creates a newer live profile
using the current endpoint state. Empty contribution documents still establish
source presence and participate in versioning.

**Verified:** Full VS Code test run: 640 passed. Focused component/listener race,
vet and diagnostics passed. Tests cover presence/null changes, compatibility
writers, repeated and net-zero batches, multiple endpoints per owner, multiple
owners, source-isolated baselines, deletion/recreation, orphan endpoints,
remote-first invalidation, rollback after allocations and shared venue/profile
ordering. Concurrent identical source puts allocate once; distinct endpoint
writes are preserved; repeated last-source deletion allocates one tombstone.

The listener test database now matches the application's immediate transaction
locking configuration; deferred read-to-write upgrades failed under concurrent
streams. Its removal fixture now supplies a complete subsequent baseline rather
than a duplicate added/removed key in the initial baseline. No runner API or
production retry policy was changed, and the single initial migration remains.

**Transition boundary at the Step G checkpoint:** The existing two streams produced versioned writes,
but their effective-view adapters still erase raw field presence before store
ingestion. Step H below replaces those listener adapters with raw contribution
conversion and adds the public-user stream. Earlier development
caches may need rebuilding; no development-only backfill migration was added.
No reflection is used. Staticcheck remains blocked by the known toolchain issue.

### Step H: Three Independent Projection Streams

- [x] Add the `user-public` source/service watch capability and typed conversion.
  Update existing test fakes and app injection points to support it; do not
  introduce a second runner or an untyped universal change model.
- [x] Run private-user, public-user and endpoint streams using projectionwatch.
  Reconcile each complete baseline against only its owned contribution within
  one transaction. A public baseline must never clear private rows or endpoints;
  an endpoint baseline must not remove user contributions.
- [x] Apply baseline differences and deltas through Step G helpers, collecting
  affected owner IDs before removing endpoint rows. Preserve unchanged profile
  versions and existing tombstones on identical reconnects or empty replays.
- [x] Preserve existing error reporting, cancellation and source retries. Extend
  adapter readiness to mean all three initial baselines have committed once,
  without adding an application-wide barrier or blocking partial-profile reads.
- [x] Test malformed baselines without destructive omission; keep delta error
  policy explicit and observable. Use channel-coordinated listener tests for
  reordered initial baselines, reconnects, offline removals, replay, readiness,
  stream stop and worker exit. Verify that one stream's recovery cannot erase
  another stream's contribution.

**Check:** Independent streams converge without claiming a remote cross-source
snapshot, and readers observe only complete local transactions.

**Implemented (2026-10-23):** `Source.WatchPublicUsers` and its service forwarding
method provide the third watch. The listener runs all three through the existing
runner and converts documents directly to private/public/endpoint contributions,
not legacy effective views. Missing fields and public null states now survive
the complete Firestore-to-SQLite path. Existing fake sources and app readiness
and persistence-failure stand-ins implement the extended contract.

Adapter readiness closes only after all three initial baselines commit once;
partial-profile reads and notification selectors continue to use committed local
state while another stream is pending. Reconnects do not reset that readiness.
Each source uses the Step G source-owned batch boundary, preserving other
contributions and versioned tombstones. Malformed baselines report failure
without signalling readiness or replacing data; malformed public deltas retain
the prior contribution, log rejection and apply other valid changes.

**Verified:** Full VS Code test run: 658 passed. Full repository race, vet and
formatting checks passed. Channel-coordinated tests cover all six initial stream
orders, partial push eligibility, raw field presence, all-three readiness,
identical replay, private-stream reconnect/offline deletion, preserved public
and endpoint state, deletion/recreation, orphan updates, stream stop and worker
exit. Baseline rejection is tested for each source, with a separate public delta
logging test. Existing app readiness and persistence-failure checks pass.

The new three-stream Firestore emulator test passed against the running empty
emulator, including in the full race run. It verifies complete private/public
and endpoint baselines, ignored legacy `uid`, private missing fields, nullable
public fields, same endpoint ID under different owner paths, ordered deltas,
public deletion and populated/empty public baselines after reopening. Emulator
tests now use isolated per-test projects rather than the shared application
project. No production retry policy or runner interface was changed. No
reflection is used; the staticcheck toolchain blocker remains explicit.

### Step I: Eligibility, Integration and Stage Gates

- [ ] Require a locally present private user contribution, applicable opt-ins
  and an eligible endpoint for push selection, including diagnostic selection.
  Public-only profiles and orphan endpoints cannot receive pushes. Retain the
  current email preference semantics through private-source reads.
- [ ] Exercise public-first, private-first and endpoints-first arrival, removal
  of either/both user documents, delayed catch-up and recreation. Suppress
  delivery while private data is missing; housekeeping must not be required to
  exclude orphan endpoints. Remote deletion remains asynchronous until ingested.
- [ ] Extend existing store/service, listener, push/email and app integration
  tests. Prove unchanged externally used notification behaviour, except the
  explicitly agreed missing-user safety rule. Verify atomic readers and
  concurrent writers using deterministic coordination, without production hooks.
- [ ] Extend source emulator tests for complete/empty baselines, public-user
  watch, endpoint owner keys, ordered deltas and reconnect completeness. Use
  isolated projects and an empty disposable emulator; do not clear user data or
  weaken completeness assertions to accommodate imported fixtures.
- [ ] Run focused checks after each edit and the section 4 gates at completion.
  Record emulator-enabled results separately. Resolve or explicitly retain the
  known staticcheck/compiler compatibility blocker; do not call it a pass.
- [ ] Update CDD-0014/0015 and this checklist with verified status. Mark user
  profile sequencing complete only after every supported writer participates.

**Deliverable:** One versioned combined user profile per ID, maintained from
three independent streams, with replay-idempotent reconciliation and safe
notification eligibility. No new public API is exposed by this stage.

### Handover to Stages 6-7

CDD-0013, CDD-0014 and CDD-0017 now record the agreed stream lifecycle and
prototype decisions. The remaining production gates are:

- Specify public/self/private API representations and authorization in
  OpenAPI. The combined internal identity does not expose private settings or
  endpoint credentials to public consumers.
- Finalize the opaque cursor and recovery envelopes in OpenAPI, including
  stream scope, database generation, sequence bounds, high-water marks,
  pagination and full-refresh behavior.
- Integrate per-UID invalidation after permission or membership changes and
  authorization-scope invalidation after visibility changes, including
  anonymous streams. Each reconnect receives a complete, currently authorized
  snapshot, so no durable invalidation marker is required in V1.
- Resolve production HTTP write deadlines, browser authentication transport,
  CORS for streaming GET requests, readiness gates, snapshot-to-live
  handover, slow-client policy and the London-midnight reset behavior.

CDD-0017's isolated stream lifecycle prototype may proceed before these
production gates are closed. Do not wire production cursor parsing or SSE
endpoints until the applicable OpenAPI and integration decisions are explicit.
