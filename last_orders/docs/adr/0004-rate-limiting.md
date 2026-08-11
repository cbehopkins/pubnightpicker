# ADR: Application Rate Limiting and Operational Guards

**Status:** Proposed
**Date:** 2026-08-11

## 1. Context

The backend performs operations where an unexpected increase in call frequency could be undesirable or dangerous.

Examples include:

* sending email;
* sending push notifications;
* calling external APIs;
* performing privileged operations;
* future operations where an application bug could cause excessive activity.

Some of these operations are subject to external quotas. Others are simply operations where excessive repetition is a useful indication that something has gone wrong.

The backend therefore requires a generic mechanism for placing a limit on the frequency of selected operations.

The existing backend has successfully used a token-bucket mechanism for this purpose.

For example:

```text
Email sending:
    capacity: 100
    refill:   100/day
```

The mechanism has also previously been used to ensure that an operational alert is itself rate-limited:

```text
Email limit exceeded
        │
        ▼
Alert limiter
capacity: 1
refill:   1/day
        │
        ▼
Administrator notification
```

The V0 backend should retain the useful part of this design without introducing unnecessary operational complexity.

---

# 2. Decision

The backend will provide a **generic, local, concurrency-safe token-bucket rate limiter**.

Operations that require protection may be associated with a named rate-limit policy.

The rate limiter is an application infrastructure component and is independent of:

* the operation being protected;
* Cellar;
* individual Handlers;
* external service implementations;
* alerting.

A protected operation conceptually performs:

```text
Acquire token
     │
     ├── token available ──► perform operation
     │
     └── no token ─────────► reject/defer operation
```

The exact response to a rejected operation is determined by the caller.

---

# 3. V0 Scope

The V0 implementation will provide:

### 3.1 Token bucket

Each limiter has:

* a maximum capacity;
* a current token count;
* a refill rate;
* a named identity.

For example:

```text
email.send
    capacity: 100
    refill:   100 tokens/day
```

The bucket refills according to elapsed time, up to its configured capacity.

---

### 3.2 Concurrency safety

Multiple Cells may execute concurrently.

The rate limiter MUST therefore guarantee that two concurrent acquisitions cannot consume the same token.

The implementation may use an in-process mutex or equivalent concurrency primitive.

The V0 implementation does not require SQLite transactions or another persistent coordination mechanism for token acquisition.

---

### 3.3 Non-blocking acquisition

V0 rate-limit acquisition should be non-blocking.

Conceptually:

```go
type RateLimiter interface {
    Acquire() bool
}
```

If no token is available, the caller immediately learns that the operation cannot currently proceed.

The caller may then:

* return a rate-limited result;
* cause the Cell to retry;
* skip the operation;
* perform another appropriate application-specific action.

The rate limiter itself does not decide how the failed acquisition should be handled.

Cells should not normally sit blocked waiting for a token to become available.

---

# 4. Named policies

Rate limits should have stable names rather than being anonymous configuration attached to arbitrary code.

Examples include:

```text
email.send
push.send
sweego.send
firebase.auth_delete
webhook.accept
```

The names provide a useful operational identity for:

* logging;
* future metrics;
* configuration;
* troubleshooting.

The precise initial set of policies will be determined as individual services are implemented.

---

# 5. Logging

When a rate limit is exceeded, V0 will log the event.

The log should identify at least:

* the rate-limit policy;
* the fact that acquisition failed;
* sufficient contextual information to identify the affected operation where practical.

For example:

```text
WARN rate limit exceeded
     limit=email.send
```

No automatic administrator notification is required in V0.

This is deliberately simpler than the previous backend.

---

# 6. Separation of concerns

The rate limiter MUST NOT be responsible for alerting.

In particular, the limiter should not contain logic such as:

```text
if exhausted:
    email administrator
```

Instead, the architecture leaves room for a future operational layer:

```text
Rate limiter
     │
     ▼
Limit exceeded
     │
     ▼
Operational alerting
     │
     ▼
Alert rate limiter
     │
     ▼
Administrator notification
```

This allows the same rate-limiting primitive to be reused for both ordinary operations and future alert suppression.

---

# 7. Relationship to Cellar

Rate limiting is independent of Cellar.

A Handler may use a rate limiter before performing an operation:

```go
if !limits.EmailSend.Acquire() {
    return ErrRateLimited
}

return sender.Send(ctx, message)
```

The Cell execution model determines what happens after `ErrRateLimited`.

The limiter does not create Cells, reschedule Cells, or otherwise depend upon Cellar.

This separation allows the same primitive to protect operations performed outside Cell handlers if required.

---

# 8. Restart semantics

V0 rate-limit state is local process state.

Consequently, restarting the backend resets the token buckets.

This is acceptable for V0.

The rate limiter is intended primarily as a guard against application mistakes and unexpected activity, rather than as a security boundary or authoritative quota enforcement mechanism.

If persistent or externally coordinated limits are required in the future, that can be introduced independently.

---

# 9. Failure semantics

Failure of the rate-limiting mechanism itself should fail closed where practical.

An operation MUST NOT silently bypass a configured protection because the limiter encountered an internal error.

The precise API may therefore eventually distinguish:

```go
Acquire() (bool, error)
```

rather than only:

```go
Acquire() bool
```

The final Go interface should be selected during implementation, but the architectural requirement is that limiter failure must not accidentally remove the safety guard.

---

# 10. Non-goals for V0

The following are explicitly deferred.

### 10.1 Circuit breakers

V0 will not automatically stop calling a dependency because repeated calls are failing.

A future circuit breaker may distinguish:

```text
healthy
degraded
open
half-open
```

but this is not part of the V0 rate-limiting mechanism.

---

### 10.2 Automatic alerting

V0 will log rate-limit exhaustion.

It will not automatically email or otherwise notify an administrator.

A future alerting system may itself use rate limiting to prevent notification storms.

---

### 10.3 Persistent rate limits

Token-bucket state will not be persisted in SQLite.

Restarting the backend resets the buckets.

---

### 10.4 Distributed rate limiting

The V0 limiter is local to one backend process.

No coordination between multiple backend instances is required.

---

### 10.5 Hierarchical limits

V0 does not require combinations such as:

```text
per-provider: 10/minute
per-endpoint: 2/minute
global:       100/minute
```

The design should not prevent these being added later, but they are unnecessary for the initial implementation.

---

# 11. Example policies

The following are illustrative rather than mandatory initial configuration.

### Email sending

```text
Policy:  email.send
Capacity: 100
Refill:   100/day
```

This provides a safety boundary around accidental excessive email generation.

### Administrative notification

This is a future example rather than a V0 requirement:

```text
Policy:  alert.email
Capacity: 1
Refill:   1/day
```

This would allow an alerting system to report a problem without generating repeated administrator notifications.

---

# 12. Consequences

## Positive

* Provides a single reusable protection mechanism.
* Protects the backend against accidental excessive operations.
* Helps keep external-service usage within expected quotas.
* Works naturally with concurrent Cell execution.
* Keeps rate limiting independent of individual Handlers.
* Provides named limits that can later become useful metrics and operational controls.
* Keeps V0 implementation small.

## Negative

* Limits reset when the process restarts.
* Limits are not globally enforced across multiple backend processes.
* Rate-limit exhaustion initially produces only logs.
* Callers must decide how to handle rejected operations.

These limitations are intentional V0 trade-offs.

---

# 13. Future extensions

The architecture leaves room for:

* persistent token buckets;
* distributed rate limiting;
* hierarchical limits;
* circuit breakers;
* automatic operational alerts;
* alert-specific rate limiting;
* metrics and dashboards;
* dynamic runtime configuration.

None of these are required for V0.

---

# 14. Architectural invariant

The key invariant established by this ADR is:

> **Operations which have an explicit application safety or quota requirement may be protected by a named, concurrency-safe local token bucket. Exceeding the limit must never silently result in the protected operation proceeding.**

V0 deliberately stops there.
