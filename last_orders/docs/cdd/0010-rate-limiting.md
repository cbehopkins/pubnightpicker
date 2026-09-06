# CDD: Application Token Sources

**Status:** Proposed
**Date:** 2026-09-06
**Related ADR:** Application Rate Limiting and Operational Guards

---

## 1. Purpose

This document defines the Version 0 implementation contract for application rate limiting.

The mechanism provides a simple way for application components to ask:

> Can I perform this operation now, or must I wait?

The mechanism is based on named token sources.

A token source maintains a finite number of tokens which may be consumed during a daily period. Once all tokens have been consumed, further acquisitions are rejected until the daily reset.

The token source is a local application infrastructure component. It does not determine what the protected operation should do when a token is unavailable.

---

# 2. Caller Interface

The caller-facing interface is deliberately minimal:

```go
type TokenSource interface {
    Acquire() int
}
```

`Acquire()` is non-blocking.

It returns:

```text
0       token was granted

>0      token was not granted; the returned value is the
        number of seconds the caller should wait before
        trying again
```

For example:

```go
if wait := tokens.Acquire(); wait != 0 {
    // Rate limited.
    // Retry after wait seconds.
    return ErrRateLimited
}

return performOperation()
```

The token source does not block waiting for a token to become available.

---

# 3. Token Consumption

Each successful call to `Acquire()` consumes exactly one token.

If at least one token is available:

```text
remaining > 0
    │
    ▼
remaining--
    │
    ▼
return 0
```

If no token is available:

```text
remaining == 0
    │
    ▼
return seconds until daily reset
```

A failed acquisition does not consume a token.

---

# 4. Daily Limit

Version 0 supports daily token limits.

A token source is configured with a maximum number of tokens:

```text
email.send
    maximum tokens: 100
```

At the beginning of each daily period the source contains the configured maximum number of tokens.

Tokens are consumed as operations are performed.

The daily period ends at midnight according to the backend's configured/application timezone.

At the beginning of the next daily period the source is reset to its maximum.

For example:

```text
maximum = 3

00:00   3 tokens
09:00   Acquire → 0   2 tokens
10:00   Acquire → 0   1 token
11:00   Acquire → 0   0 tokens
12:00   Acquire → wait
...
23:59   Acquire → wait
00:00   3 tokens
```

There is no continuous or fractional refill in Version 0.

---

# 5. Reset

A token source does not need a background timer to perform its daily reset.

The reset may be performed lazily by `Acquire()`.

When `Acquire()` is called, the source determines whether the current daily period differs from the period in which its tokens were last initialised.

If the period has changed, the source:

1. resets the available token count to the configured maximum;
2. records the new daily period;
3. clears its exhausted state;
4. proceeds with the acquisition.

This means an inactive token source requires no background activity.

---

# 6. Wait Time

When a source is exhausted, `Acquire()` returns the number of seconds until the next daily reset.

The returned value is therefore a caller-facing indication of when another acquisition may reasonably be attempted.

The value is an integer number of seconds.

The implementation should round the wait time such that the caller is not instructed to retry before the reset has occurred.

A caller must not interpret a non-zero return value as a token having been reserved for it. It merely indicates that the source is currently exhausted.

---

# 7. Exhaustion Callback

A token source may optionally be configured with an exhaustion callback.

The callback is invoked when the source becomes exhausted as a result of a successful acquisition.

For example:

```text
maximum = 100

99th successful acquisition
    remaining = 1

100th successful acquisition
    remaining = 0
    exhausted callback invoked

101st acquisition
    returns wait time
    callback is not invoked again
```

The callback is therefore associated with the **transition into exhaustion**, rather than every failed acquisition.

This prevents a repeatedly attempted operation from generating repeated exhaustion actions.

The callback is optional.

The token source remains responsible only for invoking the callback. It does not assign any particular meaning to the callback.

For example, a future operational system could use it to generate an alert.

---

# 8. Callback Failure

The exhaustion callback must not prevent the token acquisition state from being updated.

The token has been consumed regardless of whether the callback succeeds.

If the callback can report an error, that error must not cause the successful acquisition to be rolled back.

The token source should therefore treat the callback as an operational side effect rather than part of token acquisition.

The precise callback signature is an implementation detail, provided these semantics are maintained.

---

# 9. Concurrency

A token source may be accessed concurrently by multiple application goroutines.

`Acquire()` MUST be concurrency-safe.

In particular, concurrent acquisitions must not be able to consume the same token.

For example, with one token remaining:

```text
             Acquire A
                 │
                 ├── consumes final token
                 │
             Acquire B
                 │
                 └── receives wait time
```

It must never be possible for both A and B to receive a successful acquisition.

The check, reset, token consumption, and exhaustion transition must be performed as one concurrency-safe operation.

An in-process mutex or equivalent mechanism is sufficient for Version 0.

No persistent or distributed locking is required.

---

# 10. Source Identity

Token sources have stable application-level names.

Examples:

```text
email.send
push.send
sweego.send
firebase.auth_delete
```

The name is primarily an application/configuration identity.

It may be used for:

* configuration;
* logging;
* diagnostics;
* future metrics.

The token source itself does not need to perform a lookup by name.

---

# 11. Token Source Configuration

Application configuration provides a collection of named token sources.

Conceptually:

```go
TokenSources["email.send"]
TokenSources["push.send"]
TokenSources["sweego.send"]
```

Configuration establishes:

* the source name;
* maximum token count;
* optional exhaustion callback.

A component requiring a source performs the lookup during application composition/configuration.

For example:

```go
emailTokens := TokenSources["email.send"]

emailSender := NewEmailSender(emailTokens)
```

The resulting `EmailSender` receives only the `TokenSource` it requires.

It does not receive the complete token-source table and does not perform the name lookup itself.

This keeps application components independent of global rate-limit configuration.

---

# 12. Configuration Errors

A requested token-source name which is not configured represents a programming/configuration error.

It is not a normal runtime rate-limit condition.

The distinction is therefore:

```text
source exists
    │
    └── Acquire()
          │
          ├── 0       → permitted
          └── >0      → rate limited

source does not exist
    │
    └── configuration/programming error
```

Application startup/configuration should preferably detect missing required sources rather than allowing them to appear as normal runtime failures.

---

# 13. Logging

Rate-limit exhaustion should be observable.

The source or its surrounding infrastructure may log the transition into exhaustion, including the source name.

Logging must not occur once per failed acquisition merely because a source remains exhausted.

For example:

```text
WARN rate limit exhausted
     source=email.send
```

The exhaustion callback provides an alternative mechanism for operational handling where required.

---

# 14. Process Lifetime

Token-source state is local process state.

The state is not persisted.

Consequently:

* restarting the backend resets token sources;
* different backend processes have independent token counts;
* the mechanism does not provide a globally enforced quota.

This is intentional for Version 0.

The mechanism is an application safety guard rather than an authoritative external quota or security boundary.

---

# 15. Cellar Integration

Token sources are independent of Cellar.

A Cell or other application component may receive a `TokenSource` as a dependency and acquire a token before performing a protected operation.

For example:

```go
if wait := emailTokens.Acquire(); wait != 0 {
    // Cell/application-specific handling.
}
```

The token source does not:

* create Cells;
* reschedule Cells;
* retry operations;
* determine whether a Cell should fail;
* determine whether an operation should be skipped.

The Cell execution model determines what happens after an acquisition is refused.

---

# 16. Idempotency

Acquiring a token is an application-local state change.

A successful acquisition consumes one token.

The token source does not provide distributed idempotency.

If an operation is retried after successfully acquiring a token, the retry requires another acquisition unless the surrounding application explicitly arranges otherwise.

This is acceptable because token acquisition is intended to limit operation attempts, while operation idempotency is handled by the operation's own architecture.

---

# 17. Version 0 Non-Goals

Version 0 does not provide:

* continuous token refill;
* hourly, minute, or arbitrary-duration limits;
* persistent token state;
* distributed token coordination;
* hierarchical limits;
* weighted token consumption;
* blocking acquisition;
* automatic retries;
* automatic rescheduling;
* circuit breakers;
* mandatory alerting;
* runtime policy mutation.

These may be introduced by a future version if required.

---

# 18. Example

A service is configured with:

```text
Source:
    email.send

Maximum:
    100 tokens/day

Exhaustion callback:
    notifyOperationalLayer
```

The email sender receives the source:

```go
sender := NewEmailSender(
    tokenSources["email.send"],
)
```

When sending:

```go
if wait := sender.tokens.Acquire(); wait != 0 {
    return ErrRateLimited
}

return sender.sweego.Send(...)
```

The sender has no knowledge of:

* the token-source registry;
* the configured maximum;
* the reset mechanism;
* the exhaustion callback.

It simply requests a token and acts according to the result.

---

# 19. Architectural Invariants

The following invariants apply to Version 0:

1. **A successful `Acquire()` consumes exactly one token.**

2. **A failed `Acquire()` consumes no token.**

3. **`Acquire()` never blocks waiting for a token.**

4. **Concurrent acquisitions cannot consume the same token.**

5. **An exhausted source never grants a token before its daily reset.**

6. **An exhausted source reports the time until the next daily reset.**

7. **Exhaustion callbacks occur on transition into exhaustion, not on every failed acquisition.**

8. **A configured protection must not be silently bypassed because its token source is exhausted.**

9. **Application components receive the specific `TokenSource` they require rather than the global token-source registry.**

10. **Token-source state is local process state and is not authoritative across backend instances.**

---

# 20. Summary

The Version 0 rate-limiting API intentionally consists of a very small abstraction:

```go
type TokenSource interface {
    Acquire() int
}
```

The caller needs to know only whether it has obtained a token or how long it should wait before trying again.

Named sources and their configuration belong to application composition. Individual services receive only the source they require.

The implementation provides a concurrency-safe daily quota with a reset at midnight and an optional exhaustion callback.

No broader rate-limiting machinery is required until the application demonstrates a need for it.
