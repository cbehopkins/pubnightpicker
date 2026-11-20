# CDD: On-Demand Meet Projections

**Status:** Behavioural contract agreed; implementation pending  
**Date:** 2026-11-17  
**Clarified:** 2026-11-19  
**Related:** [API Architecture](../adr/0012-api-architecture.md);
[Component Architecture and Deployment](../adr/0013-component-architecture.md);
[Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Reusable Projection Listeners](0015-projection-listeners.md);
[Reusable SSE Stream Prototype](0017-sse-stream-prototype.md)

## 1. Purpose and Scope

Maintain local SQLite projections for individually requested Meets and the
current- and open-Meet collections of requested Groups, without replicating the
entire authoritative Meet collection.

Firestore remains authoritative. HandPump admits projections on demand and keeps
each admitted projection updated through a shared Firestore watch. Expiry bounds
retention, not total size or listener count.

There is no individual-Meet client stream in this design. A source document watch
keeps cached detail reads updated without implying a corresponding public stream.
Current- and open-Meets collection streaming use independent Group views.

This CDD specifies intent and lifecycle guarantees, not projected fields, concrete
table schemas, locking primitives or goroutine structure.

## 2. Projection Types and Admission

### 2.1 Individual Meet

A request for an individual Meet admits a projection keyed by Meet ID.
One Firestore document watch per admitted Meet, per HandPump instance, maintains
that projection. Concurrent requests share the same entry and initialisation.

An individual Meet's initial expiry deadline is six hours after admission.
Subsequent GETs and source changes do not renew it. Individual-Meet entries do not
expire implicitly at midnight. Attendance streams do not renew these entries.

If the initial authoritative snapshot says the Meet does not exist, return not
found and remove the attempted entry and watch. Do not retain a negative cache.
A subsequent request checks Firestore again.

An individual Meet that is deleted after admission is retired and its watch
stopped. A subsequent request checks Firestore again.

### 2.2 Current Meets for a Group

A current-Meets GET or stream connection admits a projection keyed by
Group ID. One Firestore query watch per admitted Group, per HandPump instance,
maintains its current-Meets view for all interested clients.

Preserve the existing current-Meets definition: voting-complete Meets belonging
to that Group whose calendar date is today or later in `Europe/London`, including
daylight-saving changes. An empty result is a valid, admitted, watched collection.
Order the view by scheduled date, earliest first, then Meet ID as a deterministic
tie-breaker.

The initial expiry deadline is six hours after admission. Each successfully
accepted, authorised current-Meets stream connection moves the shared deadline
to six hours after that acceptance. This extends the lifetime for all subscribers
of the entry, not only the new subscriber.

Acceptance requires a usable initial snapshot and successful authorisation.
Failed authorisation, missing resources, abandoned initialisation and recovery
timeouts do not renew an existing entry. GETs, source changes and traffic on
already-open streams do not renew it either.

### 2.3 Open Meets for a Group

An open-Meets GET or stream connection admits a projection keyed by Group ID,
independently of that Group's current-Meets projection. One Firestore query watch
per admitted Group and open-Meets view, per HandPump instance, maintains the
voting page's complete view for all interested clients.

Include voting-open Meets belonging to the requested Group regardless of
scheduled date, ordered by date then Meet ID. Do not apply a date cutoff. Passing
London midnight alone does not change open-Meets membership. An empty result is a
valid, admitted, watched collection.

The initial expiry deadline is six hours after admission. Each successfully
accepted, authorised open-Meets stream connection moves the shared deadline to
six hours after that acceptance. Failed authorisation, missing resources,
abandoned initialisation and recovery timeouts do not renew it. GETs, source
changes and traffic on already-open streams do not renew it either.

### 2.4 Other Queries

Historical, search and other Meet queries outside the current- and open-Meets
views continue to query Firestore directly. Returning a Meet in query results
does not admit its individual projection. Current- and open-Meets are the
projection-backed collection exceptions.

### 2.5 Separation from Voting and Attendance

Meet detail, current, open and past responses contain no vote or attendance data,
including counts or caller-specific state. Venue candidates belong to the Meet;
votes and their counts belong to voting. The frontend composes the separate
interfaces with independent loading and error states. An unavailable attendance
or voting result must not make a Meet disappear or silently become zero.

Vote and attendance changes do not update a Meet projection, advance a Meet
resource version or trigger current- or open-Meets events. They may independently
use the shared global sequence. The existing attendance interface remains
separate; live voting updates require a separate voting API design.

## 3. Ownership and Persistence

HandPump owns these projections, their source watches, client subscriptions and
eviction. Store projection values and an absolute `valid_until` timestamp in
HandPump's existing local SQLite database, with storage owned separately from
Venue projections. Do not share Landlord's database to implement this design.

Keep runtime watch handles, cancellation and subscriber coordination in memory.
Use the shared projection-watch and streaming machinery where applicable; do not
create a separate durable recovery workflow for these disposable caches.

The individual-Meet, current-Meets and open-Meets projections own their data and
lifecycles independently. The current- and open-Meets views for one Group may
share conversion and streaming machinery, but neither updates or removes the
other's projected records. A Meet may consequently be observed through either
Group query watch and an individual document watch. This overlap is accepted to
avoid coupling their ownership, expiry and recovery.

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
sequence allocation and idempotent application. Current- and open-Meets
snapshots and cursors must describe the same SQLite state, with no missed final
state during snapshot-to-live handover. Projection fields and concrete
normalisation remain implementation work.

### 4.1 Venue Composition

Current- and open-Meets views include selected Venue summaries from the shared Venue
projection. Retain the current details and deleted status of soft-deleted Venues
in that projection, including on a fresh baseline. An internal read for Meet
views can include them; ordinary Venue discovery, GET and Venue collection
streams still exclude them. This requires extending the existing Venue adapter
and store; do not introduce a separate Firestore Venue listener per Meet.

If the authoritative Venue is confirmed missing, preserve its ID and explicitly
represent the missing reference without inventing its details. A local cache miss
alone does not establish that the Venue is missing. Distinguish confirmed absence
from soft deletion and temporary source or store failure. If required Venue data
cannot be reliably established, the composed view is temporarily unavailable.

Meet and Venue data and the cursor are read from one consistent SQLite snapshot.
Independent Firestore watches can catch up at different times: this is not an
atomic cross-source Firestore snapshot. A committed change to displayed Venue
details updates affected current-Meets views without changing the Meet resource
version or creating an action audit record. This applies to Firestore-ingested
changes as well as API writes. Notify after commit, wake only views referencing
the Venue and do not emit a replacement if their visible view has not changed.
Register for both Meet and relevant Venue changes before the initial snapshot so
concurrent changes are not lost. Rapid changes may coalesce into the latest view.

## 5. Expiry and Eviction

Run a periodic Cellar housekeeping Cell within HandPump, alongside the local
resources it controls. Landlord must not need to access HandPump's SQLite file
or manipulate its in-memory watches and streams.

Housekeeping finds entries where `valid_until <= now`. This deadline makes an
entry eligible for eviction; physical removal occurs on a subsequent sweep,
rather than requiring an exact timer for every entry. The sweep interval is a
configuration decision, not an additional retention guarantee.

Retirement makes the entry unavailable for further use, cancels its source watch,
resets and closes any associated current- or open-Meets streams, and removes its owned
cached view and admission data. Eviction means local observation has ended; it
does not mean the authoritative Meet has been deleted.

Renewal and eviction must have an unambiguous winner. A renewal completed before
eviction claims an entry protects it from that sweep, even if its deadline has
already passed. Once retirement has begun,
new requests create or join a replacement rather than reviving the retiring
entry. Late callbacks and cleanup from an old watch cannot recreate its entry,
populate a replacement or delete replacement data.

Repeated cleanup must be safe. Ordinary eviction must not reset the shared
database generation or sequence allocator, delete unrelated projections or
durable work, or report an authoritative Meet deletion. Resource bookkeeping
must remain compatible with the existing full-snapshot synchronisation contract.
V1 may discard evicted entries' projection data and per-entry history without
retaining indefinite deletion markers: reconnects receive complete views. Future
incremental resume requires an explicit retention boundary or equivalent recovery
design and must not assume evicted history remains available.

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
Meets. It does not retire individual-Meet, open-Meets or Venue projections.

## 7. Failures and Recovery

When a source watch reports a failure, mark the entry unavailable and reset and
close the affected current- or open-Meets client streams. Retain the entry while retrying until
recovery or expiry. GETs and new streams wait within their bounded allowance for
a fresh authoritative baseline; they must not serve the known-disconnected view
as healthy. Network failure detection itself is not guaranteed to be immediate.
Only streams for the affected entry are reset, unless an underlying failure also
affects other entries.

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

Authoritative deletion of an individually watched Meet retires its entry and
watch; subsequent detail reads check Firestore and return not found if still
absent. There is no detail stream to notify. A Meet entering or leaving a
current- or open-Meets query produces an updated collection snapshot while the
Group watch remains open. This need not mean Meet deletion.

If the shared Venue listener reports failure, reset and close current- or
open-Meets streams that require Venue details and make those composed views unavailable
until a fresh Venue baseline commits. Groups without that dependency need not
reset solely for this reason. Reconnecting requests use the same bounded wait.

## 8. Client Streaming, Authorisation and Writes

Current- and open-Meets projections have client streams within this design.
Follow
CDD-0013/0017: full replacement snapshots on connection and relevant changes,
fresh snapshots on reconnect, existing cursor handling, bounded subscriber
buffers and slow-client closure. No durable replay of every mutation is promised.

Use the existing generic `view_reset` event, with no event ID and `data: {}`,
before closing for retirement, detected watch failure or applicable authorisation
invalidation. Reconnection reauthorises and creates or joins a usable entry.
London midnight retires current-Meets streams only; open-Meets streams remain
open because date passing does not change their membership.

Authorise every GET and stream connection in the API layer. The cache may admit
and fetch an entry before the API decides whether to return it, so a denied
request may still populate the cache. A shared cached view or watch grants no
access by itself. Authorisation must apply to the Group association in the
representation actually returned. Individual Meet GET requires authentication;
public Groups permit reads by authenticated callers, while private Groups
require membership or global-administrator authority. Collection views and
other Meet operations retain their existing membership rules. Do not infer
authorisation from projection admission.

Writes remain authoritative in Firestore. These watches subsequently update the
projections; this CDD introduces neither optimistic SQLite updates nor another
projection-writing path. A successful mutation does not by itself guarantee that
the next cached GET or stream event already reflects it. Mutation responses and
read-your-write requirements belong with the write endpoint contracts.

The write side owns Firestore action audit records shown in the Admin frontend,
including who created a Meet and who added each Venue. Projection admission,
source snapshots, replays and recovery must never create these action records.
The existing audit producer's delivery guarantees and the future write API's
audit contract are separate work; a projection cannot reconstruct actor history.

## 9. Restart, Shutdown and Operational Limits

On HandPump restart, discard individual, current- and open-Meet cached view and
admission data; do not eagerly restore their watches. Rebuild only when requested, and require
fresh baselines before serving. Preserve unrelated SQLite data, durable work and
shared sequence infrastructure. Persisted deadlines do not promise admission
survives a process restart.

Backend startup readiness still applies, but individual Meet and per-Group
current- and open-Meets baselines are per-entry requirements, not startup-wide blockers.

On shutdown, stop entry admission, cancel owned watches and close active client
streams through HandPump's lifecycle. This is not Landlord shutdown draining.

V1 has no additional hard entry or listener cap. Six-hour retention is not a size
bound, and new stream connections can keep a popular Group entry alive
indefinitely. Monitor active entries, source watches, subscribers and recovery
failures. A future cap needs an explicit rejection or active-entry eviction
policy rather than an implicit change to these rules.

### Operational Logging

Routine admission, retirement and recovery diagnostics use a projection-scoped
runtime opt-in and are masked by default. Do not log each cache hit, unchanged
snapshot or successful stream message. Failures remain visible at warning or
error level, with repeated failures controlled to avoid log flooding. Include
projection type, relevant Meet or Group ID, outcome or retirement reason and
error where applicable; exclude document contents and personal data. These are
ordinary backend logs, not Admin-visible Firestore action audit records.

## 10. Verification Criteria

Implementation checks must demonstrate:

- Concurrent admission shares one initialisation and watch per entry per instance.
- Initial views wait for committed baselines; empty results, missing Meets and
  unavailable sources remain distinct outcomes.
- Group baseline reconciliation cannot affect another Group or individual cache.
- Successful current- or open-Meets stream admission renews that view's shared deadline; GETs,
  failed admissions, source updates and retries do not.
- Request cancellation does not cancel entry-owned work or other callers.
- Expiry, renewal and replacement races cannot revive or corrupt retired entries.
- Midnight retires only current-Meets entries and new admissions use the new
  London date, including daylight-saving boundaries.
- Source and application failures stop unhealthy reads and streams; recovery
  requires a fresh atomic baseline and remains bounded by entry expiry.
- Authoritative removal and ordinary cache eviction have distinct behaviour.
- Vote and attendance changes never update Meet versions or trigger collection streams;
  candidates remain Meet data and separate frontend failures do not erase Meets.
- Soft-deleted Venue details survive a fresh Venue baseline, remain excluded from
  ordinary Venue reads and are available in Meet views; confirmed missing and
  temporarily unavailable Venues remain distinct.
- Relevant committed Venue changes, including externally ingested changes, reach
  affected streams without missed snapshot-to-live updates or unrelated events.
- A Venue listener failure resets only streams dependent on it; their snapshots
  recover from a fresh baseline and one consistent local SQLite state.
- Denied requests may admit entries but never receive unauthorised data or renew a
  stream deadline. Authorisation applies to the representation actually returned.
- Projection lifecycle diagnostics are disabled by default and opt-in per
  projection; failures remain visible without repeated-log flooding. Source
  observation and replay never create Firestore action audit records.
- Current- and open-Meets snapshots, cursor handover, reconnects, authorisation invalidation
  and slow-client handling satisfy the existing streaming contract.
- Restart discards only these disposable caches and does not restore all watches
  or reset unrelated synchronisation state or durable work.

## 11. Explicit Follow-Up Work

- Define projected fields, normalisation, schemas and concrete Firestore queries
  and indexes while implementing the adapters.
- Choose concrete configuration for request wait bounds, sweep frequency and the
  initial six-hour cache lifetime; no additional timing values are fixed here.
- Design live voting updates independently of the open-Meets view. Attendance and
  votes are not implicitly covered by watching the Meet document.
- Specify write-endpoint consistency requirements separately.
- Specify write-side action audit delivery, attribution and failure handling
  separately; existing producers do not imply an atomic audit guarantee.
- Do not add an individual-Meet detail stream or its event envelopes.
