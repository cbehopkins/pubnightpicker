# New Poll Notification Service — Working Specification

**Status:** Working specification / design capture
**Purpose:** Capture the current architectural understanding of the New Poll notification flow so that it can be refined alongside the specifications for the other listeners.

---

## 1. Purpose

When a new poll document is created, the backend must identify users who have opted in to receive notifications when a poll opens and deliver the appropriate notification through:

* push notification; and
* email.

Push and email are treated as separate delivery pipelines, although their overall architecture is intentionally similar.

The system must support users with multiple notification endpoints. A single user may therefore result in multiple individual delivery operations.

The service is built using the **Cellar** execution architecture, with a local SQLite database providing durable application-level state for individual notification attempts.

---

# 2. Trigger

The process is initiated by the **New Poll listener**.

The New Poll listener responds to the creation of a new poll document in Firebase.

The Firebase-generated poll document ID is the identity/idempotency key for the New Poll event.

Conceptually:

```text
Firebase
  poll/{pollId} created
        │
        ▼
   New Poll listener
        │
        ▼
notification processing
```

The detailed downstream actions of the New Poll listener are described below.

---

# 3. General Notification Model

For each notification channel, the system follows a three-stage pattern:

1. **Recipient discovery**

   * Query Firebase.
   * Determine who should receive the notification.
   * Materialise the intended recipients/endpoints into the local application database.

2. **Fan-out**

   * Iterate over the materialised notification records.
   * Create one Cell for each individual delivery operation.

3. **Individual delivery**

   * Each delivery Cell is responsible for exactly one external delivery.
   * It performs the external request.
   * It records the outcome in the application database.

The resulting structure is therefore:

```text
                    New Poll
                       │
             ┌─────────┴─────────┐
             │                   │
            Push               Email
             │                   │
       Discovery Cell       Discovery Cell
             │                   │
        Fan-out Cell         Fan-out Cell
             │                   │
        ┌────┼────┐        ┌────┼────┐
        ▼    ▼    ▼        ▼    ▼    ▼
      Push Push Push     Email Email Email
      Cell Cell Cell     Cell  Cell  Cell
```

The number of individual delivery Cells is arbitrary and depends on the number of eligible endpoints/recipients.

---

# 4. Application Notification Table

The local SQLite database contains an application-level table representing individual notification intents/deliveries.

The table is intended to provide durable state across the different Cell stages.

At minimum, a notification record contains the conceptual following information:

| Field                  | Purpose                                                     |
| ---------------------- | ----------------------------------------------------------- |
| UID                    | Unique identity/correlation identifier for the notification |
| User ID                | User for whom the notification is intended                  |
| Endpoint / destination | Specific push endpoint or email address                     |
| Message                | Notification content to be delivered                        |
| Status                 | Current application-level state                             |
| Poll ID                | Poll that caused the notification                           |
| Channel                | Push or email                                               |

The precise schema and state machine are to be defined later.

The UID is particularly important for email because it can also be embedded into the outbound email as a custom header, allowing the external email provider to be correlated with the local notification record.

---

# 5. Push Notification Pipeline

## 5.1 Push — Cell 1: Recipient Discovery

The New Poll listener creates the first Push Cell.

Its responsibility is to determine all users who have opted into push notifications for the poll-open event.

The Cell queries the Firebase user document collection and identifies users whose notification preferences indicate that they want to receive push notifications when a poll opens.

For every eligible push endpoint, it creates an application-table record.

The initial state of each record is:

```text
pending
```

Each record receives a unique UID.

### Multiple endpoints

A user may have multiple push endpoints.

These are treated independently.

For example:

```text
User A
 ├── Endpoint 1
 ├── Endpoint 2
 ├── Endpoint 3
 └── Endpoint 4
```

produces four individual notification records.

Consequently, the later fan-out stage creates four delivery Cells for that user.

An endpoint should appear only once in the recipient set for the particular notification operation.

### Completion

Cell 1 does not perform any push delivery.

Its responsibility ends once the recipient records have been successfully materialised in SQLite.

Its commit also creates **Push Cell 2**.

---

# 6. Push — Cell 2: Fan-Out

Push Cell 2 operates over the notification records created by Cell 1 for this New Poll operation.

For each individual endpoint/notification record, Cell 2 creates a new Cell representing one push delivery.

Conceptually:

```text
Notification table

UID-1 → User A → Endpoint A1
UID-2 → User A → Endpoint A2
UID-3 → User B → Endpoint B1
UID-4 → User C → Endpoint C1

                 │
                 ▼

          Push Cell 2
                 │
        creates individual Cells
                 │
       ┌─────────┼─────────┐
       ▼         ▼         ▼
   Cell UID-1 Cell UID-2 Cell UID-3 ...
```

The individual delivery Cells receive sufficient information to identify their notification record.

After successfully creating the delivery Cells, Cell 2 updates the corresponding application-table records to indicate that their delivery Cells have been created.

The creation of the Cells and the corresponding local database state update should be treated as a transactional operation where supported by the Cellar/application architecture.

Cell 2 does not itself perform push requests.

---

# 7. Push — Cell 3: Individual Delivery

An individual Push Cell represents exactly one push delivery to one endpoint.

Its responsibility is deliberately narrow:

1. Read the corresponding application notification record.
2. Obtain the endpoint and message.
3. Make the request to the external push-notification service.
4. Receive the response.
5. Record the resulting application-level status in SQLite.

There may be an arbitrary number of these Cells for a single New Poll.

There may also be multiple Cells associated with the same user when that user has multiple endpoints.

---

# 8. Push Error Handling

Exactly-once external delivery is **not required for the initial implementation**.

If an individual push request fails, the Cellar execution/retry mechanism may retry the Cell.

This means that, in an ambiguous failure scenario, the same push notification may potentially be delivered more than once.

This is an accepted trade-off for the initial implementation.

The current priority is:

> reliable processing and simple architecture rather than attempting to guarantee exactly-once push delivery.

If duplicate push notifications prove problematic in practice, the delivery mechanism can subsequently be enhanced.

---

# 9. Email Notification Pipeline

The email pipeline mirrors the push pipeline structurally.

It consists of:

1. Email recipient discovery Cell.
2. Email fan-out Cell.
3. One individual Email Delivery Cell per email destination.

The same application-table model and UID concept apply.

The primary difference is that email delivery has an additional optional verification stage during service bring-up.

---

# 10. Email — Recipient Discovery

The first Email Cell queries Firebase users for users who have opted into receiving poll-open notifications by email.

The relevant email address and associated notification information are materialised into the local application notification table.

Each individual email destination receives its own notification record and UID.

As with push, multiple eligible destinations are represented independently.

---

# 11. Email — Fan-Out

The Email Fan-Out Cell creates one Email Delivery Cell for each individual notification record.

The resulting topology is:

```text
Email notification records
          │
          ▼
   Email Fan-Out Cell
          │
    ┌─────┼─────┐
    ▼     ▼     ▼
  Email  Email  Email
  Cell   Cell   Cell
```

Each Email Delivery Cell is responsible for one email.

---

# 12. Email — Individual Delivery

An Email Delivery Cell:

1. Reads its notification record.
2. Constructs the email.
3. Includes the notification UID as a custom header in the outbound email request.
4. POSTs the email to the email service/provider.
5. Receives the response.
6. Records the result in the application notification table.

The UID therefore serves both as:

* the local identity/correlation identifier; and
* an externally visible correlation identifier embedded in the email.

The external provider is known to expose sufficient information to subsequently query sent email and inspect the relevant headers.

---

# 13. Email Verification Cell

During initial service bring-up, an Email Delivery Cell that successfully receives its POST response may create an additional Cell.

This **Email Verification Cell** exists primarily as a diagnostic/sanity-check mechanism.

It is not currently intended to be part of the permanent production delivery path.

Its purpose is to verify that the email submitted to the provider can subsequently be found through the provider's query API.

The Verification Cell will:

1. Query the email provider for messages sent to the relevant email address.
2. Restrict the query to an appropriate time window.
3. Identify the email corresponding to the notification UID/custom header.
4. Confirm that the email we submitted is present in the provider's records.
5. Record the verification result locally.

Conceptually:

```text
Email Delivery Cell
       │
       │ successful POST
       ▼
record delivery result
       │
       ▼
create Verification Cell
       │
       ▼
query email provider
       │
       ▼
find message containing our UID
       │
       ▼
record verification result
```

This mechanism is intended to provide confidence during bring-up that:

* the email request was accepted;
* the provider retained the message;
* the message can subsequently be queried;
* the custom correlation header works;
* our local state can be reconciled with the provider's state.

The verification stage should be configurable and should be possible to enable/disable as required for debugging and operational investigation.

---

# 14. Email Error Handling

Email introduces an important additional failure mode compared with push.

A POST may fail or time out without establishing whether the email provider actually received and accepted the message.

For example:

```text
Email Delivery Cell
        │
        ▼
      POST
        │
        ▼
 network failure / timeout
        │
        ▼
Did provider receive it?
```

Because the outbound email contains our UID as a custom header, the provider's search facilities can potentially be used to resolve this ambiguity.

Conceptually:

```text
POST has ambiguous outcome
          │
          ▼
query provider
          │
    ┌─────┴─────┐
    │           │
  found       absent
    │           │
    ▼           ▼
accepted      retry
```

This provides a potential mechanism for avoiding unnecessary duplicate email delivery in ambiguous failure cases.

The exact retry/reconciliation algorithm remains to be specified.

---

# 15. Idempotency

The New Poll listener itself uses the Firebase poll document ID as its event identity/idempotency key.

Once the notification pipeline has been created, individual notification records have their own UID.

Cellar provides the execution-level idempotency and retry mechanisms for Cells.

The application notification table separately records the business-level state of each notification.

These are intentionally distinct concepts:

**Cellar idempotency**

> Has this Cell already been executed?

**Application state**

> What is the current state of this particular notification/delivery?

The system does not currently attempt to guarantee exactly-once delivery to external notification services.

---

# 16. Snapshot Semantics

Recipient discovery occurs in the initial discovery Cell.

The resulting recipient/endpoints are materialised into SQLite.

This means that the intended recipient set for a particular New Poll is effectively captured at the time the discovery operation executes.

Subsequent changes to a user's notification preferences should not implicitly alter the already-materialised notification records for that poll.

This is the intended conceptual model, although the precise semantics may be refined later.

---

# 17. Transactional Boundaries

The architecture makes use of Cell commits to establish durable progress between stages.

In particular:

### Discovery

The recipient records should be durably committed before the fan-out stage proceeds.

### Fan-out

The creation of individual delivery Cells and the corresponding update of the local notification records should be treated as a transactional boundary where the Cellar/application architecture permits.

This prevents the local application state from incorrectly claiming that a delivery Cell exists when it does not, or vice versa.

The exact transaction implementation is deliberately left open at this stage.

---

# 18. Deliberate v0 Trade-Offs

The following are intentional simplifications for the initial implementation:

* Push delivery does not require exactly-once semantics.
* Duplicate push notifications caused by retries are acceptable.
* Email verification is primarily a bring-up/debug facility rather than a permanent production requirement.
* The detailed notification state machine has not yet been finalised.
* The exact retry/reconciliation behaviour for ambiguous email failures remains to be defined.
* The precise SQLite schema remains to be defined.
* The exact transactional implementation of Cell creation plus local state updates remains to be defined.

The architecture should avoid prematurely solving these problems if the initial Cellar model already provides sufficient reliability.

---

# 19. Summary

The New Poll notification system is a fan-out pipeline implemented using Cellar.

For each notification channel:

```text
New Poll
   │
   ▼
Discovery Cell
   │
   │ query Firebase
   │ materialise recipients
   ▼
Local notification table
   │
   ▼
Fan-Out Cell
   │
   │ one Cell per endpoint/destination
   ▼
Individual Delivery Cells
   │
   │ one external request each
   ▼
External notification service
```

Push delivery ends at the individual delivery Cell.

Email delivery may optionally continue through a verification Cell during service bring-up:

```text
Email Delivery Cell
       │
       ▼
 Email provider
       │
       ▼
Verification Cell
       │
       ▼
Query provider and correlate using UID
```

The fundamental architectural principle is:

> **Discover once, materialise the intended deliveries durably, fan them out into independently executable Cells, and keep the state of each individual delivery in the local application database.**

This specification should be treated as a working design record rather than a final implementation contract. The same structure can be used as the template for specifying the remaining New Poll behaviour and the other top-level listeners.
