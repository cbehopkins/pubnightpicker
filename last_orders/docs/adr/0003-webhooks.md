# ADR: Webhook Ingestion and Durable Event Acceptance

**Status:** Proposed
**Date:** 2026-08-11

## Context

The backend needs to consume webhook events from external services.

A webhook differs from a Firebase database listener or timer because the source system is making an HTTP request and expects an HTTP response.

The HTTP connection must therefore remain entirely within the Webhook Listener.

A Cell must not contain, own, or require access to an HTTP connection. Cells are durable application work units and their data is serialised into Cellar; transferring an active HTTP connection into a Cell would be both architecturally inappropriate and potentially impossible.

The Webhook Listener therefore has two distinct responsibilities:

1. Validate and accept the incoming HTTP request.
2. Durably record the resulting application event and create the Cell responsible for processing it.

The actual processing of the webhook event is performed asynchronously by Cellar.

---

# Decision

## 1. The Webhook Listener owns the HTTP lifecycle

The Webhook Listener receives the HTTP request and remains responsible for the entire HTTP exchange.

The listener:

1. receives the request;
2. authenticates the request;
3. validates and decodes the webhook;
4. derives the event identity/idempotency key;
5. durably records the event and creates its initial Cell;
6. commits that transaction;
7. returns the appropriate HTTP response.

The HTTP connection is never passed to a Cell.

Conceptually:

```text id="7y3f2p"
External Service
      │
      │ HTTP POST
      ▼
Webhook Listener
      │
      ├── authenticate
      ├── validate
      ├── decode
      ├── derive event identity
      │
      ▼
Application DB transaction
      │
      ├── record accepted event
      └── create Cell
      │
      ▼
    COMMIT
      │
      ▼
    HTTP 2xx
```

The Cell subsequently executes independently of the HTTP request.

---

# 2. Durable acceptance occurs before HTTP acknowledgement

The central invariant is:

> A successful webhook response MUST NOT be returned until the webhook event and its initial Cell have been durably committed.

Therefore:

```text id="h3t8zc"
DB + Cell commit
      ↓
HTTP 2xx
```

is valid.

The reverse ordering is not:

```text id="g2v5la"
HTTP 2xx
      ↓
DB + Cell commit
```

If the process fails after returning 2xx but before durable acceptance, the external provider may reasonably assume that the webhook was accepted and may not retry it. This could result in permanent event loss.

---

# 3. The Application DB contains the durable Event Queue

The Application DB provides the durable hand-off between the Webhook Listener and Cellar.

The webhook transaction records the accepted event and creates the initial Cell atomically.

Conceptually:

```text id="r8p1nd"
Application transaction
    │
    ├── Event Queue record
    │
    └── Cell creation
```

Both succeed or neither succeeds.

The Event Queue is therefore not intended to become a second asynchronous execution system.

Cellar remains responsible for executing the work.

The Event Queue provides durable application-level record of the accepted external event and the atomic boundary at which the Cell enters Cellar.

---

# 4. The listener does not perform business processing

After validation and durable acceptance, the Webhook Listener does not execute the application work represented by the webhook.

It creates the initial Cell and returns the HTTP response.

The subsequent processing follows the normal Cell architecture:

```text id="j9w4hx"
Webhook Listener
      │
      ▼
Event + Initial Cell
      │
      ▼
Observed/Event Cell
      │
      ▼
Idempotency
      │
      ▼
Dispatch
      │
      ├── Handler Cell
      ├── Handler Cell
      └── Handler Cell
```

The precise naming of these Cells remains subject to the wider Cellar architecture.

---

# 5. Duplicate webhook observations are expected

The Webhook Listener is not required to prevent duplicate observations.

External webhook providers commonly retry requests when:

* the connection is lost;
* the provider does not receive a response;
* the provider times out;
* the backend crashes after accepting the request but before responding.

The backend therefore deliberately permits the same logical webhook to result in multiple Cells.

For example:

```text id="a6j3tr"
Webhook event X
      │
      ▼
Cell A created
      │
      X process dies before HTTP response
      │
      ▼
Provider retries
      │
      ▼
Cell B created
```

Both Cells may legitimately exist in Cellar simultaneously.

This is acceptable.

The Cells will eventually encounter the same local idempotency key and one will establish that the event has already been processed.

The duplicate Cell is therefore harmless.

This is the same fundamental model used for repeated observations by database listeners.

---

# 6. Webhook event identity

Each webhook must have a stable logical identity where the external provider supplies one.

The listener should derive an event key suitable for the backend's idempotency mechanism.

Where the provider supplies a stable event identifier, that identifier should form the basis of the key.

Conceptually:

```text id="k7u2pc"
<listener/provider>:<external-event-id>
```

For example:

```text id="4d1q8w"
SweegoEmail:evt_123456
```

The precise key format is implementation-defined.

The important requirement is:

> Multiple deliveries of the same external event must produce the same logical event key.

---

# 7. Providers without stable event IDs

Not all webhook providers necessarily provide a suitable stable event identifier.

The backend must therefore define an explicit event identity strategy for each such provider.

The implementation must not assume that hashing the complete payload is universally sufficient.

Payloads may change between retries, and different events may theoretically have equivalent payloads.

If no provider-defined event identity exists, the webhook integration specification must define how a stable key is constructed.

This is a provider-specific concern rather than a responsibility of the generic Webhook Listener infrastructure.

---

# 8. Authentication and validation

Authentication and basic validation occur synchronously in the Webhook Listener before the event is accepted.

The listener should establish that:

1. the request originated from the expected provider;
2. the request has a valid authentication/signature;
3. the payload is structurally valid;
4. sufficient information exists to derive the event identity;
5. the event can be represented by the application's webhook event model.

Invalid or unauthenticated requests must not create Cells.

The exact authentication mechanism is provider-specific.

---

# 9. Failure modes

## 9.1 Failure before durable acceptance

```text id="v3p8cm"
HTTP request
    ↓
validation
    ↓
process dies
```

No event has been durably accepted.

The HTTP request does not receive a successful response.

The external provider may retry.

This is safe.

---

## 9.2 Failure after durable acceptance but before response

```text id="m6k1rx"
HTTP request
    ↓
Event + Cell committed
    ↓
process dies
    ↓
no HTTP response
```

The external provider retries.

A second Cell is created for the same logical event.

Both Cells eventually perform the same idempotency operation.

One succeeds; the other observes that the event has already been processed.

This is safe.

---

## 9.3 Successful acceptance and response

```text id="q4z7pd"
HTTP request
    ↓
Event + Cell committed
    ↓
HTTP 2xx
```

The provider considers the webhook accepted.

Cellar subsequently processes the event.

---

## 9.4 Processing failure after acknowledgement

Once the event has been durably accepted and the HTTP response has been returned, downstream processing failures are no longer webhook protocol failures.

They are Cell execution failures.

The Cellar retry/error-handling mechanisms apply.

The external provider does not need to retry the webhook merely because a downstream Handler failed.

This separation prevents external retry behaviour from becoming coupled to internal application processing.

---

# 10. Response semantics

The initial implementation should use normal HTTP success responses to indicate durable acceptance.

The exact status code and response body are provider-specific where required by the provider's webhook contract.

The architectural rule is:

> Successful HTTP acknowledgement means "the backend has durably accepted this event", not "the backend has successfully processed this event".

This distinction must be maintained.

---

# 11. Relationship to the general Listener architecture

Webhook listeners are one of several sources of application observations.

The overall architecture is:

```text id="p2w9fs"
Database Listener ──┐
                    │
Webhook Listener ───┼──► Event/Observation Cell ─► Idempotency
                    │
Timer Listener ─────┘
```

The source-specific listener is responsible for translating its source mechanism into a durable application event.

After that boundary, the processing architecture is shared.

The key differences are:

| Source   | Acceptance mechanism                  | HTTP lifecycle   |
| -------- | ------------------------------------- | ---------------- |
| Database | Observe source change and create Cell | None             |
| Webhook  | Validate request, commit Event + Cell | Listener owns it |
| Timer    | Observe due schedule and create Cell  | None             |

---

# 12. Consequences

## Positive

* HTTP connections remain entirely within the Webhook Listener.
* Cells remain serialisable, durable application work units.
* The webhook response does not depend on downstream business processing.
* External provider retries are naturally handled.
* Duplicate webhook deliveries are safe.
* The Event Queue and Cell creation provide a clear durability boundary.
* The same Cell/idempotency architecture can be reused for webhook processing.
* External webhook reliability is separated from internal Handler reliability.

## Negative

* The Application DB gains a durable Event Queue/event record.
* Webhook integrations must define stable event identity.
* The listener must perform authentication and validation synchronously.
* There is necessarily a small window between database commit and HTTP response in which a process failure can cause a duplicate delivery.
* The architecture therefore depends on downstream idempotency.

That final point is intentional: **duplicate delivery is preferable to possible event loss**.

---

# 13. Architectural invariants

The following are mandatory properties of the design:

1. **The Webhook Listener owns the HTTP connection for its entire lifetime.**

2. **A Cell MUST NOT contain or depend upon an active HTTP connection.**

3. **A successful HTTP response MUST only be sent after durable acceptance.**

4. **Event recording and initial Cell creation MUST be part of the same Application DB transaction.**

5. **The listener MUST NOT wait for downstream Handler completion before acknowledging the webhook.**

6. **Duplicate webhook deliveries are expected and MUST be safe.**

7. **The logical webhook event key MUST be stable across retries of the same external event.**

8. **Webhook authentication and structural validation occur before durable acceptance.**

9. **Downstream processing failures are Cell failures, not webhook HTTP failures.**

10. **The Event Queue is a durable acceptance mechanism, not a second execution queue.**

---

# 14. Open questions

The following are deliberately left for implementation/provider-specific specifications:

* Exact Go interface for `WebhookListener`.
* Exact Event Queue schema.
* Exact Cell type used for an accepted webhook.
* Exact local idempotency interface.
* Provider-specific authentication/signature verification.
* Provider-specific event identity and idempotency-key construction.
* Provider-specific HTTP response requirements.
* Maximum accepted payload size.
* Payload retention and redaction requirements.
* Whether the original webhook payload should be retained in full or transformed into a smaller application event.
* Operational metrics and audit requirements.
* Rate limiting and abuse protection.

These should not alter the fundamental architecture established by this ADR.
