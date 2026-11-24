# ADR-012 — HTTP API Architecture and Resource Contracts

**Status:** Proposed — decisions captured; open items listed  
**Date:** 2026-10-16  
**Updated:** 2026-10-19  
**Project:** PubNightPicker  
**Scope:** Go HTTP API, authentication, Meets, voting, attendance, Venues, Locations, streaming and errors

## 1. Context and goals

PubNightPicker is moving to a Go backend that owns application logic and persistence access.

The frontend must use the HTTP API rather than accessing Firestore directly. It must not depend on Firestore collection names, document structures or legacy field names.

The API should provide:

- Stable, documented resource contracts.
- Server-side authentication and authorisation.
- Explicit separation between Meets, Venues, Locations and Groups.
- Efficient read models for the frontend.
- A consistent error format.
- A path to streaming attendance and current-Meet updates.
- A practical migration path from legacy Firestore data.

OpenAPI is the authoritative HTTP contract.

## 2. Architectural decisions

### 2.1 Layering

The intended flow is:

```text
Frontend
   |
   v
Go HTTP API
   |
   v
Application and domain logic
   |
   v
Repository interfaces
   |
   v
Firestore
```

The HTTP layer handles routing, decoding, authentication context and response formatting. Application/domain logic owns use cases, validation and authorisation decisions. Repository implementations own persistence details and legacy data mapping.

API DTOs, domain types and persistence structs should be distinct. Do not use Firestore persistence structs as public request or response types.

### 2.2 OpenAPI

OpenAPI is the source of truth for:

- Routes and HTTP methods.
- Request and response schemas.
- Authentication requirements.
- Error responses.
- Query parameters and pagination.
- SSE event payloads and cursor behaviour.

Implementations and generated clients should follow the contract rather than independently inventing conventions.

### 2.3 Naming

Use the following terminology consistently:

- **Group:** community, membership and administration boundary.
- **Meet:** a persistent PubNightPicker lifecycle object associated with a Group and scheduled date.
- **Venue:** a pub, restaurant or user-facing event location/resource.
- **Location:** a named geographical grouping used to classify Venues and configure Group defaults.
- **Vote:** a member's choice within a Meet's voting phase.
- **Candidate:** a Venue explicitly offered as a voting option for a Meet.

Voting is a phase/mechanism of a Meet, not a separate public Poll resource. Do not use `/polls/{poll_id}` as the public API.

The Venue type `event` is distinct from a Meet. A Meet is the lifecycle object; an `event` Venue is a type of Venue.

## 3. Authentication and authorisation

### 3.1 Authentication mechanism

Use Firebase Auth ID tokens as bearer tokens.

Authenticated requests supply:

```http
Authorization: Bearer <firebase-id-token>
```

The backend validates the token and derives caller identity from it. Client-supplied user IDs must not be trusted as proof of identity.

### 3.2 Optional authentication

Public discovery endpoints support optional authentication:

- No token: process as anonymous.
- Valid token: process as authenticated.
- Invalid or expired supplied token: return `401 Unauthorized`.

A bad token must not silently downgrade the request to anonymous.

Anonymous access is permitted for public Group discovery and public Venue/Location information.

### 3.3 Meet access

Anonymous and authenticated callers may read open, current and past Meet
collections and current/open streams for public Groups. Invalid supplied
credentials still return `401`; they never fall back to anonymous access.
Individual Meet details require authentication and are readable when the Group
is public or the caller is a member of its private Group. Other Meet-related
reads require Group membership, including:

- Voting snapshots and vote operations.
- Attendance snapshots and updates.
- Attendance streams.

Global administrators may access all Meet resources through inherited
administrative authority.

Authentication establishes identity only. It does not automatically grant membership or resource permissions.

Group-scoped authorisation must be enforced on the server. Global administrators can always see and edit all Venues. Other callers require permission to edit the Venue, as described in section 12.2. Location writes require global-administrator authority in V1.

## 4. Error contract

All API errors use a consistent envelope:

```json
{
  "error": {
    "code": "meet_not_found",
    "message": "Meet not found"
  }
}
```

`code` is stable and machine-readable. `message` is human-readable and must not be used by clients for program logic.

Proposed status conventions:

| HTTP status | Meaning |
|---|---|
| `400` | Invalid request, malformed input or domain validation failure |
| `401` | Missing required authentication or invalid/expired supplied token |
| `403` | Authenticated caller lacks permission |
| `404` | Resource absent or intentionally hidden from caller |
| `409` | Domain conflict, such as attempting to modify candidates after Meet completion |
| `500` | Unexpected server failure |

V1 does not need a separate `422` validation convention; use `400` for validation errors.

Do not expose internal Firestore errors, stack traces, credentials or other sensitive implementation details.

## 5. Group API

Group identity, visibility, membership, permissions and configuration are specified in ADR-011.

The API should expose Groups through stable opaque IDs. The frontend may initially discover the bootstrap Group by name:

```http
GET /groups?name=ampubnight
```

A single group query may return multiple group ids.

After discovery, it should use the returned Group ID.

Membership routes:

```http
GET    /groups/{group_id}/members
PUT    /groups/{group_id}/members/{user_id}
DELETE /groups/{group_id}/members/{user_id}
```

The backend applies the membership and visibility rules defined in ADR-011. The query will return any public groups that match the name together with any private groups that match the name that the (authenticated) user is a meber of.

## 6. Meet lifecycle and collections

### 6.1 Creating a Meet

```http
POST /meets/{group_id}
```

Creation requires the group-scoped `create_meet` permission or inherited global-administrator authority.

The new Meet must have a scheduled date at creation. A new Meet begins with no selected Venue and no voting candidates. Voting opens immediately, but members can vote only for active candidates or the special `any` option.

A Group may have multiple current Meets. Do not enforce a one-current-Meet-per-Group assumption.

### 6.2 Current and past collections

```http
GET /meets/{group_id}/current
GET /meets/{group_id}/past
```

Current and past are determined by the scheduled calendar date using `Europe/London`, including daylight-saving changes.

A Meet that has not been completed is excluded from both collections, even after its scheduled date passes. It remains available through the relevant Meet workflow, including the voting page.

Once completed:

- If its scheduled date is in the past, it appears in the past collection.
- Otherwise, it appears in the current collection.

No automatic cancellation of uncompleted Meets is required for V1. Collection logic should not depend on such cancellation being implemented. This matches current behaviour of the existing implemntation.

The current collection is expected to be small and does not initially require pagination. The past collection uses cursor pagination, newest first, with a default page size of 20.

### 6.3 Current Meet response

The current page needs enough information to avoid an N+1 pattern of per-Meet requests. The response should include:

- Meet ID and scheduled date.
- Completion/voting state.
- Selected Venue summary where present.
- Venue name, address and image where available.

Meet responses do not contain votes or attendance, including counts or
caller-specific state. Venue candidates are Meet data; votes are voting data.
The frontend composes the separate interfaces with independent loading and
error states. Live voting updates require a separately designed voting API.
Current Meets are ordered by scheduled date, earliest first, then Meet ID.
See [CDD-0019](../cdd/0019-on-demand-meet-projections.md) for view composition.

### 6.4 Past Meet response

The past page needs a compact response containing at least:

- Meet ID.
- Scheduled date.
- Selected Venue name and image where available.

Only a small number of past Meets are expected to be displayed per page.

### 6.5 Individual Meet

```http
GET /meet/{meet_id}
```

Use singular `/meet/{meet_id}` for a single Meet and plural `/meets/{group_id}/...` for Group-scoped collections.

## 7. Completing and rescheduling a Meet

### 7.1 Completion

```http
POST /meet/{meet_id}/complete
```

Completion requires the group-scoped `complete_meet` permission or inherited global-administrator authority. The existing permission source is the `roles/canCompletePoll` document, which contains the UIDs of users with this permission. The repository layer maps this legacy representation to the application permission. The Meet must be in a completable state.

Illustrative request:

```json
{
  "venue_id": "venue-A",
  "restaurant_id": "venue-B",
  "restaurant_time": "19:30"
}
```

Rules:

- `venue_id` is required.
- `restaurant_id` is optional.
- If `restaurant_id` is provided, `restaurant_time` is required.
- If no `restaurant_id` is provided, no restaurant time is supplied.
- Any valid Venue may be selected at completion, even if it is not an active voting candidate.
- The special vote key `any` is not a Venue and cannot be selected as the Meet's Venue.
- Completion selects the Venue and closes voting in the frontend.
- Completing a Meet does not mean the real-world Meet has ended.
- The frontend does not reopen voting after completion. The backend does not enforce this restriction: vote changes after completion do not alter the selected Venue, and enforcing closure would add complexity without benefit.
- Existing votes may be retained as history.

The ability to choose a Venue outside the candidate list is intentional: the final plan may change to another nearby Venue.

`POST /meet/{meet_id}/complete` is a first-commit-wins transition. An open Meet
may be completed once; a later API attempt, whether identical or different,
returns conflict. During backend-first migration, the existing direct frontend
writer may still compete with automatic completion. The completed-state
transaction prevents overwrites, but full cross-writer first-commit guarantees
begin only after the frontend cutover.

### 7.2 Rescheduling endpoint: compatibility semantics

```http
POST /meet/{meet_id}/reschedule
```

Despite the endpoint's name, this operation changes the selected Venue only. It does **not** change the scheduled date.

The endpoint name is retained for compatibility.

Rules:

- The scheduled date remains unchanged.
- The frontend does not reopen voting.
- The caller needs the same permission as completion; there is no separate reschedule permission.
- When the selected Venue changes, the actual previous Venue ID is appended to `previous_venue_ids`, preserving order and duplicates.
- Changing restaurant details alone does not append a previous Venue ID.
- Omitted restaurant fields are preserved; clearing the restaurant also clears its time.
- A time-only update is valid for an existing restaurant.
- A supplied date is rejected.
- Venue history is retained.

The exact request schema should be documented in OpenAPI to make the Venue-only semantics explicit and avoid future confusion.

## 8. Voting candidates and votes

### 8.1 Candidate management

```http
PUT    /meet/{meet_id}/candidates/{venue_id}
DELETE /meet/{meet_id}/candidates/{venue_id}
```

A new Meet starts without candidates. A Venue must be explicitly added before members can vote for it.

Candidate management requires the existing legacy candidate-management permission associated with “Can Add delete Pub Polls”. It is distinct from the permission to complete a Meet.

Users with this permission may add and remove candidates for the Meet, including event Venue candidates. Preserve these permission semantics; no further verification of the permission's purpose is required. Its API representation belongs in OpenAPI.

Candidates must not be added or removed after the Meet is completed, regardless of the caller's permissions.

Removing a candidate removes it from active voting options but does not delete its vote document:

- Historical votes remain stored.
- Removed candidates are omitted from the normal voting snapshot.
- Re-adding the same candidate restores the existing votes to the active view.

This behaviour preserves history and avoids destructive vote deletion when a candidate is temporarily removed.

### 8.2 Vote operations

```http
PUT    /meet/{meet_id}/votes/{venue_id}
DELETE /meet/{meet_id}/votes/{venue_id}
```

These operations are bodyless. The caller's identity comes from the validated Firebase token; the client does not supply a `user_id`.

Rules:

- The caller must be a Group member.
- The frontend offers voting only while it is open; the backend accepts otherwise valid vote changes after completion.
- A normal Venue vote requires that Venue to be an active candidate.
- The special key `any` is allowed and is not a Venue ID.
- `PUT` and `DELETE` should be idempotent.

Venue IDs are opaque and must not collide with the reserved `any` vote key.

### 8.3 Vote persistence ownership
The voting cache is a read projection, not the sole owner of vote documents. Backend operations may modify votes for distinct purposes:

* Vote operations add or remove the caller’s selection.
* User deletion removes the user’s votes.
* Meet creation and deletion create or delete the associated vote document.
* Event recurrence creates a vote document with the new Meet.

These operations do not write through the cache. Meet lifecycle changes must preserve their atomic transactions, and the cache observes committed changes through its Firestore watch.

### 8.4 Voting snapshot

The voting snapshot should include active candidate Venues and, for each candidate:

- Vote count.
- Identities of users who voted for it.
- Whether the caller voted for it.

The `any` option provides equivalent voting information.

The frontend explicitly needs to know who voted, not only the aggregate counts.

Voter identities use the shared minimal public user summary:

```json
{
  "id": "user-opaque-id",
  "name": "Display name",
  "photo_url": "https://example.invalid/photo"
}
```

`photo_url` is optional. Do not expose Firebase credentials, provider details or unrelated profile fields.

### 8.5 Voting stream

```http
GET /meets/{group_id}/votes/stream?meet_id={meet_id}&meet_id={meet_id}
```

Live voting updates use one stream per Group covering the Meets the client lists, rather than one stream per Meet. Browsers allow only six concurrent HTTP/1.1 connections per origin, so a page holding one long-lived stream per Meet could exhaust them and leave ordinary API requests queued indefinitely. Each event carries a complete voting snapshot for every listed Meet. The client reconnects with a new list when the Meets it shows change. Every listed Meet must belong to the Group. The list is bounded at 50 Meets.

## 9. Attendance

### 9.1 Per-Venue and Any-Venue status mutations

```http
PUT    /meet/{meet_id}/attendance/{venue_id}
PATCH  /meet/{meet_id}/attendance/{venue_id}
DELETE /meet/{meet_id}/attendance/{venue_id}
```

The caller must be an authenticated true member of the Meet's Group. Public
visibility, global administrator status and voting permission do not grant this
mutation. The verified UID identifies the caller; clients cannot name another
user. The Meet and attendance document are checked and mutated in one Firestore
transaction.

Attendance status values:

- `attending`
- `not_attending`

`PUT` accepts only `status` and returns no content. `DELETE` withdraws the
caller's response. Both operations are idempotent, maintain exclusive status
arrays, and clear the caller's ETA when withdrawing or setting
`not_attending`. A removed Venue can be withdrawn when the caller has a response
for it or the Meet records its removal; unrelated Venues are rejected. The
literal `any` targets an independent standing response in the existing
attendance document. It changes no Venue response. For main Venue candidates it
is the fallback when no explicit Venue response exists; explicit responses
override it, and restaurants never inherit it. A single explicit response
applies when a Venue serves both roles. Existing Venue entries, including
indistinguishable legacy copies, remain untouched and are treated as explicit
responses. Older clients continue to interoperate through the existing
document shape.

Changing or withdrawing the standing response atomically clears ETAs that no
longer have effective can-come attendance, while retaining ETAs for explicit
Venue can-come responses.

### 9.2 Authenticated snapshots and Group stream

```http
GET /meet/{meet_id}/attendance
GET /meets/{group_id}/attendance/stream?meet_id={meet_id}&meet_id={meet_id}
```

Both reads require authentication and voting-style Meet/Group visibility.
Snapshots preserve the existing per-Venue status arrays and separate `any`
entry, expose applicable ETAs and public participant summaries, and do not
apply voting-visibility preferences to attendance identities. The Group stream
follows 1-50 distinct Meets in the requested Group and sends complete
replacement snapshots with opaque cursors. Reconnect returns current state;
authorization changes reset and close the view. Meet, attendance and public
profile changes refresh it. Private profile fields are excluded.

Per-Venue and Any-Venue status writes require true Group membership,
independently of voting permission. The PATCH operation supports independent
status and ETA updates, including explicit ETA removal. Existing bulk UI
actions compose this caller-owned API rather than writing attendance
Firestore documents directly. Firestore rules remain unchanged during the
compatibility window.


## 10. Server-Sent Events

### 10.1 Routes

```http
GET /meets/{group_id}/attendance/stream?meet_id={meet_id}&meet_id={meet_id}
GET /meets/{group_id}/current/stream
```

The attendance stream publishes complete snapshots for the listed Meets in one
Group connection. The current-Meets stream publishes changes to the Group's
current Meets.

There is no past-Meets stream in V1; the past collection is static and fetched using HTTP.

### 10.2 Event semantics

SSE events describe resulting view state rather than low-level storage mutations. Streams are derived from projections held in the local database, following the existing Venue cache pattern, rather than directly from Firestore listeners.

The local projection implementation must be extended to record modification timestamps. These timestamps are the source for stream cursors; the exact ordering and cursor contract remain design work.

Clients recover the latest complete view after reconnecting without assuming
they observed every intermediate event. The attendance and Group voting stream
cursor envelopes and full-snapshot semantics are specified in OpenAPI.

### 10.3 Snapshot and cursor protocol

The intended protocol is:

1. Fetch a snapshot containing state and revision `N`.
2. Subscribe to the stream using revision `N` as the last observed cursor.
3. Return view changes after `N`, or the latest view state if the cursor cannot be used.
4. Continue receiving live events.
5. Use SSE event IDs and `Last-Event-ID` for reconnection.

The snapshot and its revision must represent the same logical point in time. A concurrent update must not fall into a gap between the snapshot and stream subscription.

An unknown, invalid or expired cursor returns the most up-to-date version of the requested view. For example, an unusable cursor for changes to a Meet returns the latest version of that Meet. Recovery does not require reconstructing every intermediate change. Cursor encoding and event envelopes are specified in the CDD and OpenAPI.

### 10.4 Reusable projection-stream component

Projection-based streaming must be designed as a reusable component, not separately implemented for each endpoint. [CDD-0013](../cdd/0013-projection-streaming.md) captures the agreed direction and outstanding design details.

The design must cover timestamp assignment and ordering, consistent snapshots, subscription handover, deletions and collection departures, restart recovery, and multiple-instance behaviour. The existing Venue cache does not yet store modification timestamps and must be extended as part of that work.

There is no decision to use a separate in-memory event history as the stream's source. The API must not promise durable replay of every intermediate event; latest-view recovery is the agreed fallback.

## 11. Locations

Location is a first-class resource with:

- `id`
- `name`
- `notes`

Legacy `out_of_town` classification is replaced by Location.

Routes:

```http
GET    /locations
GET    /locations/{location_id}
POST   /locations
PATCH  /locations/{location_id}
```

Anonymous callers may read public Location information. Writes require global-administrator authority in V1.

Location deletion is not supported. There is no Location deletion endpoint or deletion-conflict contract in the current scope.

The API should use `default_location_id` for a Group's default rather than duplicating Location names in Group configuration.

## 12. Venues

A Venue represents a pub, restaurant or user-facing event/festival resource.

Supported `venue_type` values:

- `pub`
- `restaurant`
- `event`

Only `pub` Venues participate in automatic Venue selection that populates voting candidates. A `restaurant` implies food; a `pub` may or may not serve food. The `event` type and its recurrence behaviour must be retained.

Routes:

```http
GET    /venues
GET    /venues/{venue_id}
POST   /venues
PATCH  /venues/{venue_id}
DELETE /venues/{venue_id}
```

Creating, editing and deleting Venues require the caller's `canManagePubs` permission or global `admin` role. The server must enforce this permission for all three operations; frontend route guards are not an authorization boundary. This matches the current frontend implementation in `react/src/components/pages/EditPub.js` and `react/src/dbtools/pubs.js`.

### 12.1 Public discovery

In the current API version, venue reads are unrestricted: anonymous and authenticated callers receive the same venue data. Ordinary reads and streams include all non-deleted Venues, including banned Venues. Group visibility does not filter reads in this version.

Potential filters include:

```http
GET /venues?location_id={location_id}
GET /venues?dog_friendly=true
```

The precise query parameter inventory should be defined in OpenAPI.

Public Venue responses should contain descriptive information needed by clients, such as name, address, website/map URLs, image, notes, type, Location, public features and `banned`. Unrelated internal operational metadata should not be exposed by default.

`banned` is an ordinary Venue attribute and has the same visibility as other Venue attributes. It is not admin-only and does not introduce special discovery, query or candidate-discovery filtering rules.

### 12.2 Venue visibility

A Venue may be associated with Groups for application use. The current API version does not use that association to restrict reads.

The bootstrap process should add the `ampubnight` Group to the visibility list for existing Venues.

Global administrators can always see and edit every Venue. A caller with `canManagePubs` can create, edit and delete Venues. Merely seeing a Group does not grant `canManagePubs`. The API must enforce this rule on the server.

### 12.3 Soft deletion

Venue deletion is soft deletion using `deleted_on`. Location deletion is not supported.

Normal discovery and ordinary operational queries exclude deleted records.
Meet views retain references to soft-deleted Venues and display their retained
details and deleted status. The shared Venue projection must retain those details
on a fresh baseline and expose them to an internal Meet-view read, without
changing ordinary Venue discovery or GET visibility. A confirmed missing Venue is
represented by its ID and missing status, not fabricated details. Temporary
source failure is not proof of absence. See
[CDD-0019](../cdd/0019-on-demand-meet-projections.md#41-venue-composition).

A Venue's `banned` state is distinct from deletion. Banning indicates that PubNightPicker should not visit the Venue because of a moderation or operational reason; it does not mean the record has been deleted.

### 12.4 Legacy field mapping

The legacy Firestore Venue collection is `pubs/{venueId}`. It contains fields including:

```text
name
venueType
web_site
map
address
notes
pubImage
recurrence
next_occurrence_date
parking
food
dog_friend
beer_gerden
out_of_town
banned
visiblegroups
previous_pubs
restaurant
deleted_on_date
```

The API should use consistent names rather than reproducing legacy naming errors. Suggested mappings include:

| Legacy field | API/domain field |
|---|---|
| `venueType` | `venue_type` |
| `web_site` | `website_url` |
| `map` | `map_url` |
| `pubImage` | `image_url` |
| `dog_friend` | `dog_friendly` |
| `beer_gerden` | `beer_garden` |
| `out_of_town` | Location reference |
| `visiblegroups` | `visible_group_ids` |
| `previous_pubs` | `previous_venue_ids` |
| `restaurant` | `restaurant_id`, if it is an ID |
| `deleted_on_date` | `deleted_on` |

Confirm each mapping against the legacy code and stored data before implementing migration. The repository layer owns the translation; clients must not know the old field names.

## 13. Venue recurrence

Recurrence is normally omitted from ordinary Venue responses and requests unless a client needs to configure or inspect it.

Preserve the recurrence contract defined by [CDD-0005](../cdd/0005-event-recurrence.md) and implemented by the [recurrence component](../../internal/lastorders/components/recurrence/rules.go). The public representation must be typed rather than an arbitrary unvalidated map.

The rule fields are `frequency`, `date`, `interval`, `weekdays`, `weekday`, `nth`, `month` and `month_day`. Supported forms are:

- **Once:** `frequency = once` with a configured `date`; a missing, invalid or passed date has no future occurrence.
- **Weekly:** one or more `weekdays` (or the legacy single `weekday`) and an interval in weeks.
- **Monthly:** `month_day`, or `weekday` with `nth`, and an interval in months.
- **Yearly:** `month` plus either `month_day` or `weekday` with `nth`, and an interval in years.

Weekdays use Monday = 0 through Sunday = 6. `nth = -1` means the final matching weekday. The existing parser defaults a missing or non-positive interval to 1. Interval is spacing between occurrences, not a repeat count; the existing rule has no repeat-count or end-date field and no separate `start_date`.

`next_occurrence_date` is derived state, separate from the configured `date` of a once rule. All evaluation uses `Europe/London`. Invalid calendar dates must not roll into another date; monthly and yearly calculations skip periods without a matching valid date.

When advancing a stale occurrence, the reference is `max(storedDate + 1 day, today)`. This may re-anchor intervals greater than one, as explicitly accepted in CDD-0005. If no usable rule or future occurrence remains, the stored occurrence is cleared.

OpenAPI must express this existing typed contract rather than introduce new recurrence semantics. Recurrence behaviour and compatibility are not unresolved architectural questions.

## 14. Public user summary

A shared minimal user summary is used for votes, attendance and member lists:

```json
{
  "id": "user-opaque-id",
  "name": "Display name",
  "photo_url": "https://example.invalid/photo"
}
```

- `id` is the stable user identifier used by the API.
- `name`, when present, comes from the user's public display name and may be missing.
- `photo_url` is optional.
- Authentication credentials, provider tokens and unrelated profile fields are excluded.

Clients must tolerate missing display names and photos, as the frontend already does. No server-generated fallback display name is required by this ADR. OpenAPI must reflect these optional fields.

## 15. Current and past collection consistency

The collection rules are based on the scheduled calendar date in `Europe/London`, not a fixed UTC offset.

This ensures the date boundary follows GMT/BST correctly.

The API should not infer completion merely from the date passing. Uncompleted Meets remain outside both current and past collections until they are completed, while remaining accessible through their appropriate workflow.

Per-Group timezone configuration is deferred to V3 or a similarly distant future iteration.

## 16. Implementation guidance

- Keep the public API independent of Firestore collection and field names.
- Use separate API DTOs, domain types and persistence structs.
- Make repository mappings explicit and test legacy compatibility.
- Use transactions or other appropriate atomic operations for multi-record invariants.
- Apply authorisation consistently in application logic rather than relying on individual handlers to implement subtly different rules.
- Design current-page read models to avoid N+1 requests.
- Make cursor and revision semantics explicit in OpenAPI and stream documentation.
- Ensure idempotent operations remain safe under client retries.
- Test concurrent membership changes, vote updates, candidate removal/re-addition and SSE reconnects.

## 17. Open questions

The architectural decisions above are settled. The remaining work is detailed component and HTTP contract design:

1. **Reusable projection streaming:** Complete CDD-0013, including local modification timestamps, ordering and tie handling, consistent snapshots, subscription handover, deletions, restart recovery and multiple-instance behaviour. Unknown or unusable cursors return the latest requested view; that behaviour is decided.
2. **SSE wire contract:** Define cursor encoding and the latest-view recovery envelope in OpenAPI, together with current-Meet events for creation, completion, Venue changes and current/past transitions.
3. **Past-Meet pagination cursor:** Design encoding, expiry and consistency guarantees. This is separate from the timestamp-derived SSE cursor contract.
4. **OpenAPI resource details:** Finalise the Venue-only reschedule payload, permission representations, public/Group Venue visibility representation, typed recurrence schemas and optional public profile fields, following the decisions in this ADR.

## 18. Consequences

### Positive

- The frontend has a stable, documented API contract.
- Firestore can be changed without requiring client changes.
- Meets, Venues and Locations have distinct responsibilities.
- Authentication and authorisation are enforced centrally.
- Vote history is preserved when candidates are removed.
- Attendance supports efficient snapshots and resumable streaming.
- Public discovery can be separated from membership-only Meet access.

### Trade-offs

- API DTOs and repository mappings add some implementation work compared with exposing Firestore documents directly.
- Recurrence DTOs must preserve the existing CDD-0005 and component contract.
- Reusable SSE delivery requires timestamped local projections and an explicit consistency and cursor design.
- Detailed resource schemas, stream payloads and pagination cursors remain to be finalised in OpenAPI.
