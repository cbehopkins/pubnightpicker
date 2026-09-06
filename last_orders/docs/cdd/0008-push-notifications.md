# CDD Card: Push Notification Pipeline

## Purpose

Define the architectural boundaries for the backend push-notification pipeline without prematurely fixing the internal cell decomposition.

The pipeline consists of three linked systems:

1. **Notification Truth**
2. **Endpoint Population**
3. **Endpoint Delivery**

The boundaries between these systems are architectural; whether individual stages are implemented as separate cells is an implementation decision.

---

## 1. Notification Truth

Notification truth represents the fact that a notification should exist as a consequence of some backend event/truth.

The creation of notification truth must be **idempotent and replayable**.

Replaying the source event must therefore produce the same notification identity rather than creating duplicate notifications.

Notification truth is concerned with:

* what notification exists;
* its notification type;
* the data/payload required to represent it;
* its identity in relation to the originating backend truth.

It is **not** responsible for knowing how many endpoints will receive the notification or for performing push delivery.

---

## 2. Endpoint Population

Endpoint population determines which concrete push endpoints should receive a particular notification.

Population uses:

* the notification truth;
* the user's notification preferences;
* the current notification-profile cache containing endpoint and preference information.

The resulting population represents the recipients selected for **that notification at the point population occurs**.

Once population has been committed, subsequent changes to preferences or endpoint configuration do not alter the population of an already-created notification.

### Persistence and atomicity

Endpoint population must be durably associated with the delivery work produced from it.

The architecture permits the population operation to produce, as a single cell result, application database writes which include:

* endpoint-population records, where required;
* creation of endpoint delivery cells.

These writes may be committed atomically in a single transaction.

The architecture **does not mandate** whether endpoint population and delivery-cell creation are implemented:

* within the same cell; or
* as separate cells.

Either design is valid provided that the resulting operations are idempotent and replayable.

### Replay requirement

Replaying population must not produce duplicate endpoint-population records or duplicate delivery cells.

Stable identities/uniqueness constraints should therefore be used for the notification/endpoints and delivery work.

---

## 3. Endpoint Delivery

Each delivery unit is responsible for delivering one notification to one concrete push endpoint.

Its responsibility begins after endpoint population has selected the endpoint.

It does not re-evaluate:

* notification preferences;
* endpoint eligibility;
* notification population.

The delivery unit should contain sufficient durable information to perform its delivery without depending on the notification-profile cache.

The delivery mechanism is **idempotent per notification/endpoint pair**.

### Delivery outcomes

A delivery attempt has three fundamental outcomes:

**Accepted**

The Web Push service accepts the notification.

The delivery is complete and the delivery cell becomes terminal.

**Transient failure**

The attempt cannot currently be completed, but the endpoint is not known to be invalid.

The cell remains retryable and should be retried according to the backend's retry policy.

**Permanent failure**

The endpoint is known to be unusable for this delivery.

The delivery becomes terminal and the endpoint should be invalidated/deactivated in the authoritative endpoint store.

---

## Endpoint state vs delivery state

Endpoint validity and delivery state are separate concepts.

For example:

```text
Endpoint E1
    ACTIVE

Notification N1 → E1
    TRANSIENT FAILURE
    → retry
    → ACCEPTED

Endpoint E1
    ACTIVE
```

Whereas:

```text
Endpoint E1
    ACTIVE

Notification N1 → E1
    PERMANENT FAILURE
    → delivery terminal

Endpoint E1
    INVALID
```

A failed delivery must not automatically imply that every delivery involving the endpoint has failed; endpoint invalidation is a separate piece of state.

---

## Overall pipeline

```text
Backend Truth
     │
     ▼
┌──────────────────────┐
│ 1. Notification      │
│    Truth              │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ 2. Endpoint           │
│    Population         │
│                       │
│ preferences + cache   │
└──────────┬───────────┘
           │
           │ atomically creates
           │ durable delivery work
           ▼
┌──────────────────────┐
│ 3. Endpoint Delivery │
│                       │
│ one notification ×    │
│ one endpoint          │
└──────────┬───────────┘
           │
           ▼
      Web Push
```

## Architectural invariants

* Notification truth is replayable and idempotent.
* Endpoint population is replayable and idempotent.
* Population is evaluated once for a notification rather than being re-evaluated during delivery.
* Population and delivery-cell creation must be atomically committed when produced by the same operation.
* The architecture does not mandate whether population and delivery-cell creation use one cell or multiple cells.
* Delivery is independently idempotent for each notification/endpoint pair.
* Transient provider failures cause retry.
* Permanent endpoint failures cause endpoint invalidation rather than retry.
* The authoritative endpoint store remains the source of truth; any notification-profile cache is derived state.
* Delivery does not depend on re-querying the notification-profile cache after population.
