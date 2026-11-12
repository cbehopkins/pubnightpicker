# ADR: Authorisation Caching and Invalidation

**Status:** Proposed  
**Decision scope:** Handpump authorisation  
**Authoritative store:** Firebase Firestore

## Context

Handpump will make backend authorisation decisions using permission data currently stored in Firestore.

The existing permission model uses a `roles` collection. Each role is represented by a document whose fields are keyed by user ID. A user has a role when the corresponding field is `true`; absent or non-true values do not grant that permission.

Role changes are infrequent. However, permission revocation must not depend solely on a long-lived cache, particularly where a permission allows destructive or otherwise sensitive operations.

Handpump may run as multiple independent instances. Each instance therefore has its own process-local state and must not assume that another instance has refreshed its cache.

The frontend currently writes role documents directly to Firebase. Migration to API-managed permission changes is desirable but must not be a prerequisite for the first version of Handpump.

## Decision

### 1. Separation of authentication and authorisation

Authentication establishes the identity of the caller, including their user ID.

Authorisation determines whether that user may perform the requested operation.

A valid identity token does not, by itself, establish that a user still holds a permission that may have been revoked since the token was issued.

Handpump must evaluate the relevant permission for each protected operation using sufficiently fresh permission data.

This means every protected API request performs an authorization check through
the local cache. It does not require a Firestore read on every request: a
non-expired cache entry may be used. After a listener invalidates the entry, or
its TTL expires, the next check must refresh the relevant permission from
Firestore before granting access. This authorization refresh rule is separate
from ordinary data reads, which may use instance-local projections as
described in ADR-0015.

### 2. Authoritative permission state

Firestore remains the authoritative source of role and permission state.

Handpump must not treat its local cache as authoritative or allow cached permissions to remain trusted indefinitely.

### 3. In-memory permission cache

Each Handpump instance maintains its own in-memory authorisation cache.

The cache:

- Starts empty when the instance starts.
- Is populated lazily when permission checks require data.
- Caches both grants and denials.
- Records when each result was checked.
- Checks the age of an entry on every cache lookup.
- Fetches fresh permission state when an entry is absent or expired.

The cache is disposable and does not require persistence or reconstruction across process restarts.

The initial implementation should use a simple in-memory structure keyed by user ID and role. It does not need to reproduce Firestore's physical storage layout.

V1 migrates the existing implicit `ampubnight` Group and does not implement
Group support. Its cache key is user ID and role. When explicit Groups are
implemented, group-scoped entries extend this key with Group ID; global
permissions remain unscoped. Group-specific role listeners and cache
invalidation are part of that later Group implementation, not a V1 dependency.

### 4. Permission-cache expiry

The initial permission-cache TTL is **10 seconds**.

This is a fallback freshness mechanism, not the primary invalidation mechanism and not a guarantee that all in-flight operations will stop within ten seconds of revocation.

When an entry expires, Handpump must refresh the relevant permission state from Firestore before relying on it.

If that refresh fails, Handpump must fail closed for the protected operation. It must not extend the validity of an expired grant because the authoritative store is unavailable. A failed refresh is not an authoritative denial and must not be cached as one for the normal TTL. Retry the Firestore refresh after a short failure-retry interval, initially **1 second**, so permission checks can recover promptly when Firestore becomes available again. The current operation remains denied even if a subsequent retry succeeds.

### 5. Listener-driven invalidation

Each Handpump instance listens for changes to the authoritative Firestore roles collection.

For the initial implementation, any observed role-document addition, modification or deletion invalidates the entire local authorisation cache.

Every Handpump instance runs its own Firestore roles listener and applies this
invalidation to its own process-local cache. For active SSE streams, the
listener also publishes post-observation invalidation to that instance's
stream hub for each UID whose effective authorization may have changed. The
hub signals that UID's streams to reset and close as specified in CDD-0014 and
CDD-0017. A change observed by one instance does not directly invalidate
another instance; each instance receives the Firestore change through its own
listener. Listener delay can therefore temporarily leave another instance's
cache or streams unchanged, with the TTL remaining the fallback for subsequent
authorization checks.

Clearing the whole permission cache does not require identifying the affected
users. To close only the affected SSE streams, the listener must compare the
previous and newly observed role-document values and publish invalidation for
UIDs whose effective permissions changed. If it cannot establish that set, it
must conservatively invalidate all active authenticated streams on that
instance. Targeted permission-cache invalidation may be considered later if
whole-cache invalidation becomes a measurable performance problem.

Invalidation means that cached permission results must no longer be relied upon. It does not itself establish whether a user is authorised or unauthorised.

A permission lookup that began before an invalidation must not reinsert a stale result afterwards. The implementation must protect against this race, for example by using a cache-generation counter and only storing a fetched result if the generation has not changed since the lookup began.

### 6. Listener failures and recovery

Listener notifications are an optimisation for prompt invalidation. Cache expiry remains necessary as a fallback.

When a listener reconnects after a disconnection, the instance must invalidate its local cache before relying on cached permissions again.

Because changes may have occurred while disconnected, the instance must also
reset and close all of its active authenticated SSE streams when the listener
reconnects. Each client reconnects and receives a new authorization check.

If listener delivery is delayed or a change is missed, the TTL provides a fallback provided that permission refreshes are functioning.

If the authoritative store is unavailable and permission data has expired, protected operations must fail closed.

Listener health and persistent failures should be observable through application health reporting and logs.

### 7. Direct Firebase writes during migration

The first version must continue to support existing frontend writes directly to Firestore role documents.

The Handpump authorisation cache must therefore respond to changes independently of whether those changes originated through Handpump.

In the longer term, all permission changes should be managed through an API. That API can initiate invalidation as part of the permission-management workflow.

This future migration is not a dependency of the first version.

### 8. Revocation and in-flight operations

This design does not guarantee instantaneous revocation.

An operation that has already passed its authorisation check may still be in progress when a permission change occurs. The initial implementation does not introduce distributed locking or a global revocation barrier.

If stricter revocation guarantees become necessary for particular operations, they will require an explicit additional design.

## Consequences

### Positive

- Permission checks normally avoid repeated Firestore reads.
- Both grants and denials can be cached.
- Role changes trigger prompt invalidation without requiring every writing client to notify Handpump.
- Missed invalidations do not permit permissions to remain trusted indefinitely while refreshes work correctly.
- The cache is simple, ephemeral and easy to discard.

### Negative

- Each Handpump instance maintains its own listener and cache.
- A role change invalidates unrelated cached permissions as well.
- Listener disconnections can increase database reads and delay invalidation until fallback mechanisms take effect.
- The design does not guarantee instantaneous revocation for operations already in progress.

These trade-offs are acceptable for the initial implementation.

## Deferred decisions

- Migrating all permission changes behind an API.
- Introducing targeted cache invalidation.
- Introducing a central invalidation broadcaster or message broker.
- Defining stronger revocation barriers for selected operations.
- Tuning the TTL based on measured load and operational experience.
