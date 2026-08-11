# Completed Poll Notification Service — Working Specification

**Status:** Working specification / design capture
**Purpose:** Capture the current architectural understanding of the Completed Poll listener and its downstream notification processing.

---

## 1. Purpose

When a poll is completed, the backend must notify the appropriate recipients of the completed event.

A completed poll has an associated venue configuration consisting of:

* a **pub venue**; and
* optionally, a **restaurant venue**.

The restaurant is optional because not all pubs serve food. In some cases the completed event therefore requires both a pub and a restaurant venue.

The system must also support **rescheduling** of an already-completed poll.

A rescheduled event is semantically different from the initial completion because the notification content must explicitly draw attention to the fact that the event has been changed.

The Completed Poll service therefore needs to:

1. identify completed polls;
2. determine the canonical venue configuration;
3. establish whether that configuration has already been processed;
4. determine whether this represents an initial completion or a rescheduling;
5. trigger the appropriate notification services with that information.

---

# 2. Trigger

The service is implemented as a listener on the **Firebase poll collection**.

The listener responds to poll document modification events.

The poll document contains an explicit `completed` field.

While a poll is active, the poll document can change frequently, particularly as the list of candidate pubs changes.

Once the poll has been completed, the poll document is not normally expected to change.

A modification to an already-completed poll is therefore effectively a **rescheduling operation**.

The listener can therefore use the `completed` field as an efficient filter:

```text id="xq3znd"
Poll document modified
        │
        ▼
completed == true?
        │
        ├── no → ignore
        │
        └── yes → process as Completed Poll event
```

This avoids processing the substantial amount of normal poll modification activity that occurs while a poll is active.

---

# 3. Completed Venue Configuration

A completed poll contains the venue information selected for the event.

There are two possible configurations.

### Pub only

```text id="ax3p9f"
pollId
  └── pubVenueId
```

### Pub and restaurant

```text id="j4v8se"
pollId
  ├── pubVenueId
  └── restaurantVenueId
```

The restaurant venue is optional.

The venue configuration is significant to event identity because changing either venue represents a change to the event that may require a new notification.

---

# 4. Idempotency Identity

The Completed Poll listener requires an idempotency key which uniquely represents the completed event configuration.

The key is based on:

* poll ID;
* pub venue ID;
* optionally, restaurant venue ID.

Conceptually, the resulting canonical string is:

```text id="l2m8gx"
pollId + pubVenueId
```

when there is no restaurant, or:

```text id="f8wq2c"
pollId + pubVenueId + restaurantVenueId
```

when a restaurant is also associated with the event.

The exact delimiter/canonical formatting remains to be specified.

The resulting key is stored in the listener's idempotency table.

---

# 5. Why Venue Configuration Forms Part of the Identity

The venue configuration is deliberately part of the idempotency identity because a completed event can subsequently be rescheduled.

For example, the initial completion might produce:

```text id="w7y3ra"
poll 123
pub = Red Lion
```

A subsequent rescheduling might produce:

```text id="r6k2mv"
poll 123
pub = Kings Arms
```

These are different completed-event identities and therefore both require processing.

Likewise, a restaurant may subsequently be added:

```text id="c4p9tz"
poll 123
pub = Red Lion
restaurant = Italian Restaurant
```

This is also a new event identity even though the pub has not changed.

Similarly, changing the restaurant produces another distinct event identity.

Therefore the idempotency mechanism naturally supports repeated rescheduling:

```text id="m1h7qw"
poll + pub A
      ↓
poll + pub A + restaurant B
      ↓
poll + pub C + restaurant B
      ↓
poll + pub C + restaurant D
```

Each distinct completed venue configuration has its own idempotency identity.

---

# 6. Initial Completion vs Rescheduling

The listener must distinguish between:

### Initial completion

The poll has not previously been completed.

Example:

```text id="j2v9cx"
completed = false
        │
        ▼
completed = true
pub = A
```

This is an initial completion.

### Rescheduling

The poll was previously completed and is subsequently modified while remaining completed.

For example:

```text id="b7r4nk"
Initial:
completed = true
pub = A
```

followed by:

```text id="s3q8wp"
Rescheduled:
completed = true
pub = B
```

or:

```text id="z6t2hm"
Rescheduled:
completed = true
pub = A
restaurant = C
```

or any subsequent change to the completed venue configuration.

The important distinction is:

> A rescheduling is a modification of an already-completed poll which results in a new valid completed venue configuration.

The `rescheduled` semantic should be determined by the listener and passed explicitly to downstream notification services.

Downstream services should not be required to infer this independently.

---

# 7. Listener Processing

The conceptual processing flow is:

```text id="u8p4ye"
Poll modified
      │
      ▼
completed == true?
      │
      ├── no → ignore
      │
      └── yes
           │
           ▼
Read completed venue configuration
           │
           ▼
Construct canonical idempotency key
           │
           ▼
Check idempotency table
           │
      ┌────┴────┐
      │         │
   exists     absent
      │         │
      ▼         ▼
    ignore    continue
                 │
                 ▼
       Determine initial completion
              or reschedule
                 │
                 ▼
       Record idempotency identity
                 │
                 ▼
       Trigger notification services
```

The exact transactional boundary between recording the idempotency identity and creating the downstream Cells remains to be defined.

---

# 8. Downstream Notification Services

A successfully identified Completed Poll event triggers three distinct notification services:

1. **Mailing-list notification**
2. **Personal email notification**
3. **Personal push notification**

These are logically separate downstream operations.

---

# 9. Mailing-List Notification

The first notification service sends the completed-poll notification to the generic Google Groups mailing list.

This is a shared mailing-list destination which users may subscribe to independently.

The service must receive sufficient information from the Completed Poll listener to determine whether the notification represents:

* an initial completion; or
* a rescheduling.

The message template/content differs between these cases.

For an initial completion, the notification can simply announce the selected venue.

For a rescheduling, the notification must explicitly draw attention to the fact that the event has changed.

The exact message and subject templates remain to be defined.

---

# 10. Personal Email Notifications

The second notification service sends email to individual users who have opted into Completed Poll email notifications.

This uses the same general architecture established for New Poll email notifications:

```text id="r2d7kx"
Completed Poll
      │
      ▼
Email discovery Cell
      │
      ▼
Local notification table
      │
      ▼
Email fan-out Cell
      │
      ├── Individual Email Cell
      ├── Individual Email Cell
      ├── Individual Email Cell
      └── ...
```

The discovery Cell queries the Firebase user collection for users whose notification preferences indicate that they want personal email notifications for completed polls.

The mechanism is otherwise analogous to New Poll email notification processing.

The relevant difference is:

* a different user-preference Boolean is queried; and
* the completed-poll notification uses the appropriate completed/rescheduled message template.

The individual email delivery mechanism, UID handling, retry behaviour and optional verification mechanism follow the New Poll email architecture.

---

# 11. Personal Push Notifications

The third notification service sends push notifications to users who have opted into Completed Poll push notifications.

Again, this follows the same general architecture established for New Poll push notifications:

```text id="v9s2lh"
Completed Poll
      │
      ▼
Push discovery Cell
      │
      ▼
Local notification table
      │
      ▼
Push fan-out Cell
      │
      ├── Individual Push Cell
      ├── Individual Push Cell
      ├── Individual Push Cell
      └── ...
```

The discovery Cell queries the Firebase user collection for users whose notification preferences indicate that they want push notifications for completed polls.

A user may have multiple push endpoints.

Each endpoint receives an independent delivery Cell.

For example:

```text id="q7n4bc"
User A
 ├── Endpoint 1 → Push Cell
 ├── Endpoint 2 → Push Cell
 ├── Endpoint 3 → Push Cell
 └── Endpoint 4 → Push Cell
```

The mechanism is otherwise analogous to New Poll push notification processing.

The relevant difference is:

* a different user-preference Boolean is queried; and
* the notification content reflects whether the event is an initial completion or rescheduling.

---

# 12. Rescheduling Notification Semantics

Rescheduling is a first-class piece of information passed to downstream notification services.

It is not merely an implementation detail of the Completed Poll listener.

This is required because a rescheduled event requires materially different communication.

For an initial completion, the notification may use normal announcement language:

> Tonight's pub will be the Red Lion.

For a rescheduled event, the notification must explicitly identify the change and attract the recipient's attention.

Conceptually:

```text id="e3x6kj"
Initial completion
    ↓
normal completed-event template

Rescheduling
    ↓
RESCHEDULED / attention-grabbing template
    ↓
explicitly identify changed event/venue
```

The precise wording is not yet specified.

The important architectural requirement is:

> **All downstream notification services must know whether the Completed Poll event is an initial completion or a rescheduling.**

This includes the mailing-list notification, personal email notification and personal push notification services.

---

# 13. Relationship to New Poll Notification Architecture

The personal email and push portions of Completed Poll should reuse the notification architecture established for New Poll.

The distinction is primarily in the inputs:

| Aspect              | New Poll                         | Completed Poll                         |
| ------------------- | -------------------------------- | -------------------------------------- |
| Trigger             | Poll created                     | Poll modified while completed          |
| User preference     | New-poll notification preference | Completed-poll notification preference |
| Message             | New poll announcement            | Completed/rescheduled announcement     |
| Recipient discovery | Firebase users                   | Firebase users                         |
| Local state         | Notification table               | Notification table                     |
| Fan-out             | One Cell per destination         | One Cell per destination               |
| Push endpoints      | Multiple per user supported      | Multiple per user supported            |
| Email verification  | Available during bring-up        | Same mechanism                         |
| External delivery   | Push/email provider              | Push/email provider                    |

The delivery machinery should therefore be considered reusable infrastructure rather than separately implemented business logic.

---

# 14. Idempotency and Rescheduling

The Completed Poll listener's idempotency key provides two functions:

1. Prevent the same completed venue configuration from being processed more than once.
2. Allow a genuinely changed completed venue configuration to be processed again.

For example:

```text id="c8k5rp"
poll 123 + pub A
```

is processed once.

A subsequent identical modification producing:

```text id="w1f6nz"
poll 123 + pub A
```

is ignored.

But:

```text id="v4m9tx"
poll 123 + pub B
```

is a new event.

And:

```text id="p7q2hs"
poll 123 + pub A + restaurant C
```

is also a new event.

This means rescheduling does not require a separate idempotency mechanism.

---

# 15. Deliberate Architectural Boundaries

The Completed Poll listener is responsible for determining **what happened**.

It establishes:

* that the poll is completed;
* the completed venue configuration;
* the event identity;
* whether that identity has already been processed;
* whether this represents initial completion or rescheduling.

The downstream notification services are responsible for determining **how to communicate it**.

They establish:

* which recipients have opted in;
* which delivery channels should be used;
* the appropriate message/template;
* the mechanics of delivery.

This separation should be maintained.

In particular, downstream services should receive an explicit semantic indication of rescheduling rather than attempting to reconstruct it from poll state.

---

# 16. Open Implementation Details

The following details have not yet been finalised:

* Exact canonical formatting of the composite idempotency key.
* Exact validation rules for a valid completed venue configuration.
* Exact mechanism for determining whether a poll was previously completed.
* Precise transaction boundary for recording idempotency and creating downstream Cells.
* Exact downstream Cell creation/orchestration.
* Notification message templates.
* Rescheduling subject/body/push templates.
* Exact application-table schema for completed-poll notifications.
* Exact notification state machine.
* Retry/reconciliation details for email and push.

These should be specified when the implementation design is developed.

---

# 17. Summary

The Completed Poll service listens for modifications to the Firebase poll collection.

It ignores normal active-poll modifications and processes modifications where:

```text id="r8v5kw"
completed == true
```

It constructs a canonical identity from the poll and its completed venue configuration:

```text id="a6t3pz"
pollId + pubVenueId
```

or:

```text id="n9w2xf"
pollId + pubVenueId + restaurantVenueId
```

The identity is stored in the idempotency mechanism.

A previously unseen identity causes the Completed Poll event to be processed.

The listener also determines whether the event is:

* an initial completion; or
* a rescheduling of an already-completed event.

That semantic distinction is passed to three downstream notification services:

```text id="k5c1ve"
                   Completed Poll
                         │
             ┌───────────┼───────────┐
             │           │           │
             ▼           ▼           ▼
        Mailing List   Personal    Personal
                       Email        Push
             │           │           │
             │           └─────┬─────┘
             │                 │
             │          reusable notification
             │             infrastructure
             │
             ▼
       appropriate initial-
       completion or
       rescheduling template
```

The core architectural principle is:

> **A completed poll is identified by its poll ID and completed venue configuration. A new venue configuration represents a new event identity, allowing rescheduling to be processed naturally through the idempotency mechanism. The listener determines the semantic event, including whether it is a rescheduling, while downstream notification services determine how that event is communicated.**
