# Generic Local Cache Specification

## 1. Purpose

Provide a local SQLite-backed cache of selected fields from Firebase collections.

The primary initial use case is the **venue cache**, where backend services frequently need venue information while rendering large numbers of notifications or emails.

The cache exists to reduce repeated Firebase reads.

The cache is an optimisation only.

**Firebase remains authoritative at all times.**

A cache failure, cache miss, stale cache population, or loss of the local cache must never make the application unable to obtain authoritative data from Firebase.

---

# 2. Design Principles

The cache follows these principles:

1. Firebase is the source of truth.
2. The local cache contains only fields required by backend consumers.
3. A cache miss falls back to Firebase.
4. Cache entries may be deleted without recording an explicit invalid state.
5. Normal Firebase change events maintain the local cache.
6. Cache durability is not required for correctness.
7. The initial implementation should favour simplicity over write optimisation.
8. The cache interface should be sufficiently generic to support collections other than venues in the future.

---

# 3. Phase 1 — Simple Synchronising Cache

Phase 1 provides the minimum useful implementation.

It consists of:

* a Firebase change listener;
* a local SQLite projection;
* a read-through cache interface.

It does **not** attempt to establish or monitor global cache health.

It does **not** maintain an invalid/dirty state.

It does **not** require the cache to be completely populated.

---

# 4. Firebase Synchronisation

The cache listener watches a configured Firebase collection.

For each relevant Firebase event:

### Document added

Extract the configured fields and write the resulting projection to the local cache.

```text
Firebase ADD
     ↓
extract configured fields
     ↓
cache.put(documentId, projection)
```

### Document modified

Replace the local cached projection with the newly extracted fields.

```text
Firebase MODIFY
     ↓
extract configured fields
     ↓
cache.put(documentId, projection)
```

### Document deleted

Remove the corresponding local cache entry.

```text
Firebase DELETE
     ↓
cache.delete(documentId)
```

The Firebase document ID is the cache key.

---

# 5. Cache Reads

Consumers must access cached documents through a cache abstraction rather than directly querying the SQLite tables.

Conceptually:

```text
get(collection, documentId)
        │
        ▼
   local cache?
      /    \
    yes     no
     │       │
     ▼       ▼
  return   Firebase
   cache    query
             │
             ▼
       optionally cache
             │
             ▼
           return
```

A cache miss is therefore **not an error**.

The consumer transparently falls back to Firebase.

After retrieving the authoritative document from Firebase, the implementation may populate the local cache.

The exact behaviour of this read-through population is an implementation detail, but it should not be required for correctness.

---

# 6. Cache Invalidation

The cache has no explicit invalid state.

If the backend knows that a cached document should no longer be trusted, it simply removes the entry:

```text
cache.delete(documentId)
```

The next read becomes a cache miss and therefore queries Firebase.

This is intentional.

There is no requirement for:

```text
VALID
INVALID
SYNCING
DIRTY
```

or equivalent cache states.

This keeps the consumer-facing semantics extremely simple:

> **Present means usable; absent means retrieve from Firebase.**

---

# 7. Handling Known External Changes

Operations that modify or delete an authoritative Firebase document may explicitly remove the corresponding cache entry when appropriate.

For example:

```text
operation affecting venue X
        ↓
cache.delete(X)
        ↓
perform/update authoritative Firebase operation
```

The subsequent Firebase modification event will eventually repopulate the cache with the new projection.

If the Firebase operation fails, the cache remains absent.

A subsequent consumer request therefore falls back to Firebase rather than using potentially stale local data.

This avoids the need for the cache to understand whether an external operation succeeded.

---

# 8. Cache Synchronisation Recovery

The cache does not require a global "healthy" state.

The system deliberately does not attempt to answer:

> "Are there any outstanding Firebase changes which have not yet reached the cache?"

Such a global determination is unnecessary because cache misses are safe.

If a cache entry is absent while synchronisation is catching up:

```text
cache miss
    ↓
Firebase
    ↓
authoritative result
```

The application continues to function correctly.

Once the normal Firebase listener receives the relevant document event, the cache can be populated again.

---

# 9. Startup Behaviour

Phase 1 does not require the cache to be fully populated before the application becomes operational.

The application may start with an empty or partially populated cache.

A background process may progressively populate the cache after startup.

For example:

```text
startup
   ↓
background warmer
   ↓
fetch N documents
   ↓
cache N documents
   ↓
fetch next N
   ↓
...
```

This process should operate at a deliberately controlled rate so that cache warming does not create unnecessary load on Firebase or interfere with normal application activity.

The application must remain fully functional while the cache is warming.

---

# 10. Cache Write Durability

The local cache is non-authoritative.

Consequently, cache state does not need to survive power loss or process failure.

Loss of cache contents is equivalent to starting with an empty cache.

Phase 1 should therefore use the existing SQLite database abstraction with ordinary transaction semantics.

The cache should **not initially introduce special transaction batching or durability optimisation**.

Performance optimisation through transaction piggybacking is explicitly deferred to Phase 2.

---

# 11. Generic Collection Projection

Although the first implementation is expected to be a venue cache, the design should permit a generic cache configuration.

A cache configuration should conceptually specify:

```text
source collection
cache table/projection
document ID field
fields to retain
```

For example:

```text
Collection: venues

Retained fields:
    id
    type
    name
    address
    photoUrl
    website
    recurrence information
```

The exact field list is application-specific and will be defined separately.

Fields that are required only by the frontend and have no backend use should not be copied into the local cache.

---

# 12. Venue Cache — Initial Use Case

The initial cache implementation will support venue information required by backend services.

Expected fields include, subject to detailed schema definition:

* Firebase document ID;
* venue type;
* venue name;
* address;
* photo URL;
* website;
* event recurrence information where required by backend housekeeping.

Venue types currently include concepts such as:

* pub;
* restaurant;
* event.

The complete venue schema is outside the scope of this specification.

---

# 13. Phase 2 — Pending Wites

During phase 1 the application will write to the Venue Cache rather than directly to Firebase.
The Venue cache will forward these writes directly and delete the associated document from the internal SQLite backed cache.

Phase 2 of the development will introduce a write cache. This will update the associated document preserved in the SQLite backed cache.
The SQLite backed cache will have this document marked as Dirty (pending writeback).
A Cell will be queued whose responsibility it is to perform the writeback to Firebase.


---

# 14. Phase 2 Removed

This section is removed

---

# 15. Phase 3 Memory Cache

We considered a memory cache that set before the SQLite write layer. This would accept transactions into memory immediatly.
These Pending writes would eventally be written to SQLite bundled into the next COMMIT transaction. The BaseStore would be responsible for querying us before a COMMIT to see if we had pendiong writes.

This feature will not be implemented because it impacts our consistency guarantees.

---

# 16. Consistency Model

The cache is intentionally **eventually consistent** with Firebase.

During normal operation there may be a period in which:

```text
Firebase ≠ local cache
```

This is acceptable.

The system relies on two properties:

1. Firebase is authoritative.
2. Consumers can always bypass the cache on a miss.

The Cache is responsible for querying Firebase on a Miss to confirm if the document is indeed present.
The principle is that all transactions to the venue list (the pubs collection) go through the cache entity. 
The Cache initially takes the approach that if local modifications are made then it is simpler to delete the internal
copy and wait for the listener to push a new version of the document. 
If while waiting for that push, the application needs that document, then we can always fetch it as part of the application request.


---

# 17. Failure Behaviour

### Firebase listener fails

The cache may stop receiving updates.

Consumers remain able to query Firebase directly on cache misses.

Operational monitoring should report listener failure, but application correctness must not depend on the cache being synchronised.

### SQLite/cache write fails

The cache entry is not considered successfully updated.

The failure should be logged.

The authoritative Firebase document remains unaffected.

A later cache miss can retrieve the authoritative value.

### Local database is lost

The cache is treated as empty.

Startup warming and normal Firebase events repopulate it.

No application data is considered lost.

### Process crashes

Uncommitted cache writes may be lost.

This is acceptable.

The cache is reconstructed from Firebase as required.

---

# 18. Consumer Contract

Consumers should depend only upon the following abstraction:

```text
get(entityType, entityId)
```

with semantics equivalent to:

```text
if cached:
    return cached projection

otherwise:
    return authoritative Firebase document
```

Consumers must not query the internal cache state

The cache acts a layer in front of firebase therefore it is the cache's responsibility to provide
a transparent experience.

---

# 19. Architectural Invariants

The implementation must preserve these invariants.

### Invariant 1 — Firebase is authoritative

The local cache can never override Firebase as the source of truth.

### Invariant 2 — Cache miss is safe

A missing cache entry must result in an authoritative Firebase lookup rather than an application failure.

### Invariant 3 — Cache deletion is safe

Deleting a cache entry is always a valid operation and does not require an explicit invalid state.

### Invariant 4 — Cache loss is safe

The complete loss of the local cache must not cause loss of application data or prevent correct operation.

### Invariant 5 — Cache synchronisation is eventually corrective

Normal Firebase ADD/MODIFY/DELETE events eventually bring the local projection into line with Firebase.

### Invariant 6 — Cache optimisation must not affect semantics

Any Phase 2 transaction batching or write optimisation must preserve exactly the same externally visible cache behaviour as Phase 1.

---

# 20. Implementation Phases

## Phase 1 — Simple cache

Implement:

* generic cache interface;
* configurable collection projection;
* SQLite storage;
* Firebase ADD/MODIFY/DELETE listener;
* cache reads;
* Firebase fallback on cache miss;
* optional cache population after Firebase reads;
* explicit cache deletion;
* background startup warming; <- This should come for free as part of the listener behaviour
* ordinary SQLite transactions.

Do not implement:

* global cache health tracking;
* invalid/dirty cache states;
* complex reconciliation;
* transaction piggybacking;
* cache durability guarantees.

## Phase 2 — Transaction-aware optimisation

Consider implementing:
* background flushing of pending writes;
* batching of cache writes;
* appropriate metrics around queue depth and write latency.

Phase 2 must not alter the Phase 1 consumer contract.

---

# 21. Fundamental Design Decision

The central design decision is:

> **The cache is a performance optimisation, not a consistency mechanism.**

This permits a deliberately simple implementation.

If the cache is populated, use it.

If the cache is absent, query Firebase.

If the cache is lost, rebuild it.

If a known change makes a cached entry suspect, delete it.

There is no requirement to prove that the cache is globally synchronised before the rest of the backend can operate.

# 22. Schema

The current schema for the pubs/venue collection is included below:
```
{
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "$id": "pubnightpicker/firestore/pubs",
    "title": "pubs/{venueId}",
    "type": "object",
    "x-schemaVersion": 1,
    "required": [
        "name"
    ],
    "properties": {
        "name": {
            "type": "string",
            "minLength": 1
        },
        "venueType": {
            "type": "string",
            "enum": [
                "pub",
                "restaurant",
                "event"
            ]
        },
        "web_site": {
            "type": "string"
        },
        "map": {
            "type": "string"
        },
        "address": {
            "type": "string"
        },
        "notes": {
            "type": "string"
        },
        "pubImage": {
            "type": "string"
        },
        "recurrence": {
            "type": "object",
            "properties": {
                "frequency": {
                    "type": "string",
                    "enum": [
                        "once",
                        "weekly",
                        "monthly",
                        "yearly"
                    ]
                },
                "date": {
                    "type": "string",
                    "format": "date"
                },
                "interval": {
                    "type": "integer",
                    "minimum": 1
                },
                "weekdays": {
                    "type": "array",
                    "items": {
                        "type": "integer",
                        "minimum": 0,
                        "maximum": 6
                    }
                },
                "weekday": {
                    "type": "integer",
                    "minimum": 0,
                    "maximum": 6
                },
                "nth": {
                    "type": "integer",
                    "enum": [
                        -1,
                        1,
                        2,
                        3,
                        4
                    ]
                },
                "month": {
                    "type": "integer",
                    "minimum": 1,
                    "maximum": 12
                },
                "month_day": {
                    "type": "integer",
                    "minimum": 1,
                    "maximum": 31
                }
            },
            "additionalProperties": false
        },
        "next_occurrence_date": {
            "type": "string",
            "format": "date"
        },
        "parking": {
            "type": "boolean"
        },
        "food": {
            "type": "boolean"
        },
        "dog_friend": {
            "type": "boolean"
        },
        "beer_gerden": {
            "type": "boolean"
        },
        "out_of_town": {
            "type": "boolean"
        },
        "banned": {
            "type": "boolean"
        }
    },
    "additionalProperties": true
}
```
