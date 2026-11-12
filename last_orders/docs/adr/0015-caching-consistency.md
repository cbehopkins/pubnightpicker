# ADR: Data Caching and Eventual Consistency

**Status:** Proposed  
**Decision scope:** Handpump data access and client update streams  
**Authoritative store:** Firebase Firestore

## Context

Handpump is intended to replace data-access responsibilities currently handled directly by frontend clients.

The initial HandPump deployment will run as a single backend instance and serve a small user population. The design should therefore favour a simple, understandable consistency model over distributed transactions, complex conflict resolution or infrastructure introduced solely for hypothetical scale. The architecture should remain compatible with future instances, but V1 does not guarantee multi-instance operation.

Handpump may run as multiple independent instances. Each instance may maintain its own local data cache and serve client update streams. Requests from a particular client are not guaranteed to reach the same instance.

Consequently, different instances may temporarily hold or serve different versions of the same data.

The frontend already accommodates delayed propagation of changes. The backend should provide a reliable path towards convergence without requiring every intermediate state to be delivered to every client.

## Decision

### 1. Authoritative writes

Firestore remains the authoritative data store.

A Handpump write operation must commit successfully to Firestore before Handpump reports the operation as successful.

A local cache update is not a substitute for a successful authoritative write.

The initial implementation does not require a transaction spanning Firestore and the local cache. If a process crashes after committing a database write but before updating its local cache, the cache must be able to recover.

The backend must distinguish a failed authoritative write from a successful write whose subsequent cache update or notification failed.

When concurrent writes overlap on the same resource or field, V1 accepts the
value Firestore ultimately records as current. Handpump does not compare
client-generated timestamps or versions, detect the conflict, reject either
write, or promise which caller wins. This matches the current frontend
behavior. Concurrent edits can therefore overwrite one another; callers read
the current authoritative state on a later operation and projections converge
to that state as Firestore changes are observed. Firestore's own operation
semantics still apply to non-overlapping field updates.

### 2. Instance-local read caches

Handpump instances may maintain local caches to serve reads efficiently.

Cached reads may be stale. Different instances may temporarily return different versions of a resource.

The system does not require session affinity: subsequent requests from a client may be handled by different instances.

Local caches are derived state, not authoritative storage. They must be recoverable after process restarts, missed notifications and interrupted updates.

Each instance owns its own source listeners and local cache. An observed source
change updates or invalidates that instance's cache; it does not directly
update another instance's cache. Until each instance observes the change,
requests routed there may read stale projected data. A new API request does not
by itself force a Firestore read for ordinary projected data. It reads the
instance's local projection, which converges through its listener and the
defined resynchronization path. Authorization checks have their separate
authoritative-refresh and cache-expiry rules in ADR-0014.

### 3. Eventual consistency

Changes may take time to propagate between Firestore, Handpump instances and connected clients.

Within the V1 single-instance deployment, the intended behaviour is eventual
convergence on the latest authoritative state rather than immediate
consistency between Firestore and the local projection. Future instances are
designed to maintain independent listeners and projections, but convergence
across instances is not a V1 guarantee and requires further operational
verification.

Propagation delays of several seconds are acceptable in normal operation. Delays of several minutes should be treated as a potential operational problem requiring investigation, rather than as a normal consistency guarantee.

These are initial operational expectations, not a formal service-level objective.

### 4. Cache invalidation and recovery

Cache notifications indicate that cached state may be stale. They are not themselves the authoritative data.

The implementation must provide a reliable recovery path for:

- Added, modified and deleted documents.
- Missed or delayed notifications.
- Client and stream reconnections.
- Handpump process restarts.
- Interrupted cache updates.
- Divergence between local cached state and authoritative data.

Where a cache cannot establish that its state is current enough for the operation being performed, it must refresh or resynchronise from the authoritative store.

The initial implementation should favour simple invalidation and resynchronisation over targeted cache mutation where the latter would introduce unnecessary complexity. More selective updates can be introduced later if measurements justify them.

### 5. Client update streams

A client update stream should allow the client to converge on the latest state.

The system does not need to replay every intermediate change when a client reconnects or falls behind. Sending the latest version of a resource is an acceptable recovery mechanism.

Stream cursors must be unique within their respective streams. The implementation may evolve its cursor mechanism later, provided the stream contract remains clear.

The stream implementation must handle reconnection and missed updates without relying on the client remaining connected to a particular Handpump instance.

### 6. Concurrent edits and conflict resolution

For ordinary editable Meet state, last-write-wins is acceptable. If concurrent
writes overlap, no deterministic winner is promised; Handpump reflects the
value Firestore records as current and its projections eventually converge to
that state.

This includes edits such as changing the selected venue, restaurant time or rescheduling a Meet. The initial design does not require distributed locks or sophisticated automatic conflict resolution. Occasional conflicting edits may be resolved manually by the users.

V1 does not add a separate ordering mechanism for competing writes. It must not
use timestamps generated independently by different instances to claim a
reliable total ordering or to select a winner independently of Firestore.

Business rules remain distinct from consistency mechanics. For example, votes received after a Meet has been completed may be ignored according to the completion rule.

### 7. Side effects and retries

Side effects triggered by data changes, including completion-triggered notifications, must be handled sensibly when operations are retried or notifications are delivered more than once.

Where duplicate execution would be harmful, the implementation should use appropriate idempotency or deduplication mechanisms.

Eventual consistency does not excuse losing required side effects, nor does it require introducing a general-purpose event-sourcing system.

### 8. Scope of consistency guarantees

The initial design accepts temporary divergence between local caches and between client streams.

It does not promise that all clients observe every intermediate state, nor that every instance immediately reflects every successful write.

It does require that authoritative writes are committed before success is reported and that stale derived state can recover and converge.

## Consequences

### Positive

- Reads can be served efficiently from instance-local caches.
- The application can scale across independent Handpump instances without session affinity.
- Cache failures and missed notifications can be recovered without distributed transactions.
- Stream reconnection does not require replaying every intermediate event.
- The implementation avoids premature complexity around distributed locking and conflict resolution.

### Negative

- Users may temporarily see stale data.
- Different clients or instances may temporarily disagree.
- Cache recovery and stream resynchronisation are essential correctness requirements.
- Concurrent edits can overwrite one another under the accepted last-write-wins policy.
- Side effects require separate consideration of retries and duplicate execution.

These trade-offs are acceptable for the initial scale and product requirements.

## Deferred decisions

- Formal propagation-latency objectives.
- Targeted cache invalidation versus whole-cache invalidation for individual data domains.
- More sophisticated conflict resolution if manual resolution becomes inadequate.
- Dedicated messaging infrastructure if required by scale or reliability.
- Stronger consistency guarantees for specific operations if future business requirements demand them.
