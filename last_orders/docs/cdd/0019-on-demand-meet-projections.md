# CDD: On-Demand Meet Projections

**Status:** Behavioural contract agreed; implementation pending  
**Date:** 2026-11-17  
**Related:** [API Architecture](../adr/0012-api-architecture.md);
[Component Architecture and Deployment](../adr/0013-component-architecture.md);
[Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Reusable Projection Listeners](0015-projection-listeners.md);
[Reusable SSE Stream Prototype](0017-sse-stream-prototype.md)

## 1. Purpose and Scope

Maintain local SQLite projections for individually requested Meets and the
current-Meets collections of requested Groups, without replicating the entire
authoritative Meet collection.

Firestore remains authoritative. HandPump admits projections on demand and keeps
each admitted projection updated through a shared Firestore watch. Expiry bounds
retention, not total size or listener count.

There is no individual-Meet client stream in this design. A source document watch
keeps cached detail reads updated without implying a corresponding public stream.
Current-Meets collection streaming remains part of the existing interface.

This CDD specifies intent and lifecycle guarantees, not projected fields, concrete
table schemas, locking primitives or goroutine structure.

## 2. Projection Types and Admission

### 2.1 Individual Meet

An authorised individual-Meet detail GET admits a projection keyed by Meet ID.
One Firestore document watch per admitted Meet, per HandPump instance, maintains
that projection. Concurrent requests share the same entry and initialisation.

An individual Meet's initial expiry deadline is six hours after admission.
Subsequent GETs and source changes do not renew it. Individual-Meet entries do not
expire implicitly at midnight. Attendance streams do not renew these entries.

If the initial authoritative snapshot says the Meet does not exist, return not
found and remove the attempted entry and watch. Do not retain a negative cache.
A subsequent request checks Firestore again.

### 2.2 Current Meets for a Group

An authorised current-Meets GET or stream connection admits a projection keyed by
Group ID. One Firestore query watch per admitted Group, per HandPump instance,
maintains its current-Meets view for all interested clients.

Preserve the existing current-Meets definition: voting-complete Meets belonging
to that Group whose calendar date is today or later in `Europe/London`, including
daylight-saving changes. An empty result is a valid, admitted, watched collection.

The initial expiry deadline is six hours after admission. Each successfully
accepted, authorised current-Meets stream connection moves the shared deadline
to six hours after that acceptance. This extends the lifetime for all subscribers
of the entry, not only the new subscriber.

Acceptance requires a usable initial snapshot and successful authorisation.
Failed authorisation, missing resources, abandoned initialisation and recovery
timeouts do not renew an existing entry. GETs, source changes and traffic on
already-open streams do not renew it either.

### 2.3 Other Queries

Historical, search and other Meet queries outside the current-Meets view continue
to query Firestore directly. Returning a Meet in query results does not admit its
individual projection. The current-Meets GET is the explicit projection-backed
collection exception.

## 3. Ownership and Persistence

HandPump owns these projections, their source watches, client subscriptions and
eviction. Store projection values and an absolute `valid_until` timestamp in
HandPump's existing local SQLite database, with storage owned separately from
Venue projections. Do not share Landlord's database to implement this design.

Keep runtime watch handles, cancellation and subscriber coordination in memory.
Use the shared projection-watch and streaming machinery where applicable; do not
create a separate durable recovery workflow for these disposable caches.

The individual-Meet and per-Group projections own their data and lifecycles
independently. They may share conversion helpers, but neither updates or removes
the other's projected records. A Meet may consequently be observed through both
a Group query watch and an individual document watch. This overlap is accepted
to avoid coupling their ownership, expiry and recovery.

Each admitted entry owns its watch lifetime independently of the request that
created it. A caller cancelling its wait or disconnecting does not cancel shared
initialisation or observation. The entry may remain until expiry even when no
clients remain interested; subscriber reference counting is not an eviction rule.

## 4. Initialisation and Local Consistency

Use the watch's initial authoritative snapshot to populate the projection. Do
not serve a newly admitted view until that baseline is validated and committed
to SQLite. Concurrent requests share this work rather than issuing independent
source fetches or opening duplicate watches.

Bound each request's wait for initialisation or recovery. If no usable baseline
is available within that bound, return temporarily unavailable, not an apparently
empty collection or not found. The entry's initial expiry deadline still applies
while initialisation is unsuccessful, so retries cannot retain it indefinitely.

Apply each baseline atomically, reconciling only the entry's owned result set.
A Group baseline must not replace another Group's projection or the individual
Meet cache. Later source changes use the same validated, transactional write
path. Older results and callbacks must not overwrite newer committed state.

Follow CDD-0014/0015 for atomic projected values, resource versions, global
sequence allocation and idempotent application. Current-Meets snapshots and
cursors must describe the same SQLite state, with no missed final state during
snapshot-to-live handover. Projection fields and concrete normalisation remain
implementation work.

## 5. Expiry and Eviction

Run a periodic Cellar housekeeping Cell within HandPump, alongside the local
resources it controls. Landlord must not need to access HandPump's SQLite file
or manipulate its in-memory watches and streams.

Housekeeping finds entries where `valid_until <= now`. This deadline makes an
entry eligible for eviction; physical removal occurs on a subsequent sweep,
rather than requiring an exact timer for every entry. The sweep interval is a
configuration decision, not an additional retention guarantee.

Retirement makes the entry unavailable for further use, cancels its source watch,
resets and closes any associated current-Meets streams, and removes its owned
cached view and admission data. Eviction means local observation has ended; it
does not mean the authoritative Meet has been deleted.

Renewal and eviction must have an unambiguous winner. A renewal completed before
eviction claims an entry protects it from that sweep. Once retirement has begun,
new requests create or join a replacement rather than reviving the retiring
entry. Late callbacks and cleanup from an old watch cannot recreate its entry,
populate a replacement or delete replacement data.

Repeated cleanup must be safe. Ordinary eviction must not reset the shared
database generation or sequence allocator, delete unrelated projections or
durable work, or report an authoritative Meet deletion. Resource bookkeeping
must remain compatible with the existing full-snapshot synchronisation contract.

## 6. London Midnight

At London calendar midnight, explicitly retire all admitted current-Meets
projections, cancel their query watches and send their clients a generic reset
before closing the streams. Midnight overrides a renewed expiry deadline and
does not wait for the ordinary housekeeping sweep.

The date threshold in an existing Firestore query does not advance merely because
time passes. A subsequent request must create or join an entry using the new
London date. Admission racing with midnight must not serve an old-date entry or
allow a retiring watch to publish into the new-date projection.

This rule changes collection membership, not the existence of the underlying
Meets. It does not retire individual-Meet or Venue projections.

## 7. Failures and Recovery

When a source watch reports a failure, mark the entry unavailable and reset and
close its current-Meets client streams. Retain the entry while retrying until
recovery or expiry. GETs and new streams wait within their bounded allowance for
a fresh authoritative baseline; they must not serve the known-disconnected view
as healthy. Network failure detection itself is not guaranteed to be immediate.

Reuse the existing source open/read retry behaviour, whose current default delay
is five seconds. Recovery requires a fresh validated baseline committed to SQLite
before serving resumes. Recovery attempts do not renew expiry.

A validation or SQLite application failure must not partially publish a batch,
silently skip malformed Meets or leave an old view apparently healthy. Make the
entry unavailable and close its affected client streams. The existing watch
runner returns application failures to its owner; that owner retries entry
initialisation from a fresh baseline using the existing retry interval until
success or expiry. Requests share those attempts. Do not promise entry-level
isolation where an underlying database failure affects other work too.

Authoritative deletion of an individually watched Meet removes its live
projection; subsequent detail reads return not found. There is no detail stream
to notify. A Meet leaving a current-Meets query produces an updated collection
snapshot while the Group watch remains open. This need not mean Meet deletion.

## 8. Client Streaming, Authorisation and Writes

Only the current-Meets projection has a client stream within this design. Follow
CDD-0013/0017: full replacement snapshots on connection and relevant changes,
fresh snapshots on reconnect, existing cursor handling, bounded subscriber
buffers and slow-client closure. No durable replay of every mutation is promised.

Use the existing generic `view_reset` event, with no event ID and `data: {}`,
before closing for retirement, detected watch failure or applicable authorisation
invalidation. Reconnection reauthorises and creates or joins a usable entry.

Authorise every GET and stream connection. A shared cached view or watch grants
no access by itself. Existing permission, membership and visibility invalidation
rules still apply, including anonymous streams where permitted. Do not extend
this work into inventing a new Group authorisation model.

Writes remain authoritative in Firestore. These watches subsequently update the
projections; this CDD introduces neither optimistic SQLite updates nor another
projection-writing path. A successful mutation does not by itself guarantee that
the next cached GET or stream event already reflects it. Mutation responses and
read-your-write requirements belong with the write endpoint contracts.

## 9. Restart, Shutdown and Operational Limits

On HandPump restart, discard these two kinds of cached view and admission data;
do not eagerly restore their watches. Rebuild only when requested, and require
fresh baselines before serving. Preserve unrelated SQLite data, durable work and
shared sequence infrastructure. Persisted deadlines do not promise admission
survives a process restart.

On shutdown, stop entry admission, cancel owned watches and close active client
streams through HandPump's lifecycle. This is not Landlord shutdown draining.

V1 has no additional hard entry or listener cap. Six-hour retention is not a size
bound, and new stream connections can keep a popular Group entry alive
indefinitely. Monitor active entries, source watches, subscribers and recovery
failures. A future cap needs an explicit rejection or active-entry eviction
policy rather than an implicit change to these rules.

## 10. Verification Criteria

Implementation checks must demonstrate:

- Concurrent admission shares one initialisation and watch per entry per instance.
- Initial views wait for committed baselines; empty results, missing Meets and
  unavailable sources remain distinct outcomes.
- Group baseline reconciliation cannot affect another Group or individual cache.
- Successful current-Meets stream admission renews the shared deadline; GETs,
  failed admissions, source updates and retries do not.
- Request cancellation does not cancel entry-owned work or other callers.
- Expiry, renewal and replacement races cannot revive or corrupt retired entries.
- Midnight retires only current-Meets entries and new admissions use the new
  London date, including daylight-saving boundaries.
- Source and application failures stop unhealthy reads and streams; recovery
  requires a fresh atomic baseline and remains bounded by entry expiry.
- Authoritative removal and ordinary cache eviction have distinct behaviour.
- Current-Meets snapshots, cursor handover, reconnects, authorisation invalidation
  and slow-client handling satisfy the existing streaming contract.
- Restart discards only these disposable caches and does not restore all watches
  or reset unrelated synchronisation state or durable work.

## 11. Explicit Follow-Up Work

- Define projected fields, normalisation, schemas and concrete Firestore queries
  and indexes while implementing the adapters.
- Choose concrete configuration for request wait bounds, sweep frequency and the
  initial six-hour cache lifetime; no additional timing values are fixed here.
- Reconcile older generic startup-baseline wording with lazy per-entry readiness:
  backend readiness is required, but startup must not eagerly admit every Group
  or Meet merely to enable streams.
- Design attendance and separately stored vote observation independently. Neither
  is implicitly covered by watching the Meet document.
- Specify write-endpoint consistency requirements separately.
- Do not add an individual-Meet detail stream or its event envelopes.
