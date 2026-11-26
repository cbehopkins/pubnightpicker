# CDD: Voting API and Cache

**Status:** Implemented; cutover and lifecycle follow-ups remain  
**Date:** 2026-11-26  
**Related:** [HTTP API Architecture](../adr/0012-api-architecture.md);
[Local Projection Streaming](0013-projection-streaming.md);
[Global Change Tracking and Opaque SSE Cursors](0014-sse-cursors.md);
[Reusable SSE Stream Prototype](0017-sse-stream-prototype.md);
[On-Demand Meet Projections](0019-on-demand-meet-projections.md);
[OpenAPI](../pubnightpicker-openapi-v1.yaml)

## 1. Purpose and Scope

The voting API moves voting reads and writes behind Last Orders. HandPump owns
vote authorization, privacy-safe response construction, and a lazy local
projection of each requested Meet's `votes/{meet_id}` document. Snapshot reads
and Group voting streams read this projection. Firestore remains authoritative.

This CDD records the implemented behavior and its design rationale. It does
not redefine the separate Meet, attendance, or general SSE contracts.

## 2. Ownership and Projection

The Firestore `votes/{meet_id}` document remains the write model. Its existing
shape is preserved: option IDs map to arrays of UIDs, with the reserved `any`
option stored alongside Venue IDs. No ordering state is added.

Each HandPump instance owns its own SQLite `vote_cache` projection and
in-memory entry/watch per admitted Meet. The row contains the Meet ID, an owner
token, fixed expiry, normalized vote arrays, Firestore revision, and global
SQLite sequence. Runtime watch state is not persisted. Separate HandPump
instances have independent watches and local state; there is no cross-instance
cache sharing or convergence guarantee.

Admission is lazy. A snapshot read or stream snapshot admits the Meet, persists
a new owner token and expiry, then starts one Firestore document watch. Concurrent
readers of the same active entry share its initialization and watch. A newly
admitted projection is not served until the watch's initial document snapshot
has been validated and committed. A confirmed missing vote document is a valid
empty projection and is not created by reading it.

The initial wait is bounded to five seconds. Source failures are retried after
five seconds; while unavailable, reads wait for a fresh committed baseline
rather than serving the known-disconnected projection. Watch callbacks apply
only while their owner token is current, preventing a retired watch from
overwriting a replacement entry. Projection changes and their global sequence
are committed atomically. Duplicate UIDs in a legacy option array are
deduplicated and sorted during normalization.

Entries expire six hours after admission. Reads and stream connections do not
renew the deadline. A one-minute HandPump housekeeping timer evicts expired
entries, cancels their watches, and deletes only rows with the matching owner
token. Expiry bounds retention; it is not an authoritative Meet or vote
deletion.

## 3. Snapshot Contract and Privacy

`GET /api/meet/{meet_id}/votes` requires authentication and current access to
the Meet's Group. Reading does not require `canVote`. It returns every active
candidate plus `any`, each with a distinct-voter total, anonymous count, and
visible public voter summaries. `any` is counted independently and may coexist
with Venue selections.

For each active option, the endpoint joins the projected UIDs to `user-public`
profiles. A known profile with missing or null `votesVisible` is visible by
default. A profile that opts out, is missing, or cannot be read contributes to
the total and anonymous count but has no identity in the response. Visible
summaries contain only the UID, public name, and public photo URL; private
profile fields are not returned. The caller's `selected_option_ids` are
returned separately and include stored selections for removed candidates, so
the caller can withdraw them. Removed candidates are not included in the active
options or their totals. The `vote_revision` is the projected Firestore update
time, when available.

The projection is local and watch-driven. Firestore remains authoritative; a
successful write does not imply that a subsequent snapshot has already observed
that write through the listener.

## 4. Writes

`PUT` and `DELETE /api/meet/{meet_id}/votes/{venue_id}` are bodyless and use the
UID from the verified Firebase token. Both require Meet Group access and the
global `canVote` permission. `any` is valid; adding a Venue vote requires that
the Venue is an active candidate and is not soft-deleted. A caller may select
multiple options. Completion does not block later vote changes and those
changes do not alter the selected Venue.

The Firestore transaction makes mutations idempotent and preserves the
existing vote-document shape. Removing a vote from an absent document is a
no-op that does not create it and returns no revision. For an existing document,
the service rereads after transaction success and returns its current Firestore
update time as `X-Vote-Revision`; concurrent later writes may therefore be
reflected in that header. Successful mutations return `204` only after the
Firestore transaction completes.

## 5. Live Updates

The canonical stream is `GET /api/meets/{group_id}/votes/stream`, with one or
more repeated `meet_id` query parameters (1 to 50 distinct, non-empty IDs). It
multiplexes voting snapshots for several Meets over one Group-scoped connection.
This design responds to the HTTP/1.1 connection limit of four connections per
port encountered by the voting workflow: a separate stream per Meet could
exhaust those slots. The server verifies Group access and that every listed
Meet belongs to that Group. React shares one connection per Group and signed-in
user for the Meets it currently displays; it reconnects with an updated Meet
list when that set changes.

Each `voting_snapshots` event contains complete snapshots for all listed Meets,
including on connection and reconnect. Vote-document, Meet/candidate, and
public-profile changes cause a replacement snapshot; clients reconnect with a
new Meet list when the displayed set changes. The stream uses the shared opaque
SSE cursor machinery. Cursors order committed local projection state; they are
not authorization credentials and V1 does not replay deltas. Authorization is
checked for the stream representation, and permission or visibility
invalidation can send a generic `view_reset` and close the connection. Source
watch failure marks the entry unavailable and resets affected voting views;
recovery requires a fresh committed baseline.

The app's voting-stream readiness gate is open at startup because voting
projections are on-demand. Each stream still waits for the required per-Meet
projection baseline when its initial snapshot is built.

## 6. Browser Cutover

React's voting hook uses the snapshot, mutation, and Group stream APIs. It
consumes the server's visible voter summaries and does not need hidden voter
UIDs. Pending selections remain immediate in the UI; server state is
authoritative after confirmation or refresh.

The Firestore rules cutover is not complete. `react/firestore.rules` still
allows authenticated clients to read and update `votes/{poll}`, and the
corresponding rules tests are skipped as pending that update. This leaves the
browser privacy boundary incomplete and must be resolved before claiming that
browser voting access is exclusively backend-mediated.

## 7. Operational Follow-Ups

1. **Firestore rules:** Deny browser reads and writes to raw vote documents
   after confirming all frontend voting paths use the API, then enable and
   update the currently skipped rules tests.
2. **Expiry and active streams:** Cache entries are not renewed by open stream
   connections, and eviction currently cancels/deletes the cache entry without
   explicitly resetting its voting view in the SSE hub. Define and implement
   the desired behavior for a stream that outlives its six-hour cache entry so
   it cannot silently remain attached to an evicted projection.
3. **Uncertain writes:** The API does not automatically replay a timed-out
   mutation. The UI should refresh server state and let the user reapply the
   latest choice rather than replaying a possibly superseded request.

## 8. Verification

Existing HTTP/emulator coverage exercises privacy-safe snapshots, distinct
counts, caller selections including removed candidates, authorization,
idempotent writes, revisions, absent-document removal, and stream reconnect and
authorization revocation. Component tests cover legacy UID deduplication,
concurrent initialization sharing one watch, valid empty baselines, and
rejection of malformed baselines. React hook tests cover the snapshot, mutation,
and Group stream API paths.

The remaining verification gates are the Firestore rules denial tests and
deterministic coverage for cache eviction while a stream is active. Tests were
not run as part of this documentation change.
