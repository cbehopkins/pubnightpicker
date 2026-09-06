# CDD: Firebase Notification Profile Projection

## 1. Purpose

The backend requires local access to user notification preferences and push subscription information when determining the recipients of a notification.

The authoritative representation of this information is held in Firebase.

This component maintains a local database projection of the Firebase data required by the notification system.

The local representation is a **materialised projection**, not an authoritative store and not a general-purpose cache.

Its purpose is to provide a simple and efficient local query interface while isolating the notification system from the structure of the Firebase data.

---

## 2. Authority

Firebase is the authoritative source for:

* notification preferences;
* push subscription information;
* the association between subscriptions and users;
* the active/inactive state represented in Firebase.

The local database is derived state.

The local projection MUST therefore be capable of being discarded and reconstructed from Firebase without changing the meaning of the authoritative data.

The notification system MUST NOT treat the local projection as authoritative.

---

## 3. Projection Maintenance

A projection service subscribes to the relevant Firebase query/listener.

The service:

1. receives the initial Firebase data;
2. interprets the Firebase documents;
3. translates them into the local notification-oriented representation;
4. applies subsequent Firebase changes to the local database.

The projection service owns this translation.

Changes to the Firebase document structure SHOULD therefore be isolated to the projection service rather than propagated throughout the notification system.

---

## 4. Local Representation

The local database MUST contain the information required by notification recipient population.

The exact schema is an implementation concern, but it is expected to represent concepts such as:

* user notification preferences;
* push endpoints/subscriptions;
* the user associated with each endpoint;
* whether an endpoint is currently active.

The local representation SHOULD be structured for the queries required by notification population rather than being a direct copy of the Firebase schema.

---

## 5. Consistency

The projection is eventually consistent with Firebase.

There may therefore be a period during which the local projection does not yet reflect the latest Firebase state.

The notification system MUST NOT assume that a local projection query represents the instantaneous state of Firebase.

Once a notification's endpoint population has been materialised, subsequent changes to Firebase MUST NOT modify that already-created population.

This makes projection consistency relevant when a notification is expanded, but not during subsequent delivery.

---

## 6. Idempotency

Applying a Firebase change more than once MUST produce the same local state as applying it once.

Projection updates SHOULD therefore use stable Firebase document identifiers and idempotent create/update/delete operations.

The projection MUST tolerate duplicate delivery of Firebase change notifications where the underlying listener semantics permit this.

---

## 7. Recovery

The projection service MUST be able to recover from interruption and restore convergence with Firebase.

On startup or recovery it MUST establish a valid initial view and subsequently process Firebase changes.

The implementation MUST ensure that a lost or corrupted local projection can be reconstructed from Firebase.

The precise Firebase listener, checkpointing, replay, or rebuild mechanism is an implementation concern.

---

## 8. Failure Semantics

Failure to apply a Firebase change MUST NOT result in silent divergence of the projection.

The projection service MUST retry or otherwise recover failed updates.

If the local database is unavailable, projection updates may be delayed, but the service MUST eventually converge once the failure is resolved.

A projection outage does not alter Firebase's authoritative state.

---

## 9. Interface to the Application

The rest of the backend SHOULD access notification profile information through the local projection rather than directly querying Firebase.

For example, notification population may conceptually request:

```text
GetEligiblePushEndpoints(...)
```

The interface SHOULD expose notification-oriented concepts rather than Firebase document structures.

The notification pipeline therefore depends on the projection's local interface, not on Firebase's schema.

---

## 10. Ownership

The responsibilities are divided as follows:

| Concern                         | Authority / Owner       |
| ------------------------------- | ----------------------- |
| Firebase notification data      | Firebase                |
| Firebase → local interpretation | Projection service      |
| Local notification projection   | Projection service      |
| Notification population         | Notification pipeline   |
| Notification truth              | Notification pipeline   |
| Delivery state                  | Endpoint delivery cells |

The notification pipeline MUST treat the projection as read-only.

---

## 11. Non-Goals

This component is not:

* a general application cache;
* a second authoritative store for notification preferences;
* a replacement for Firebase;
* a distributed cache requiring invalidation or TTL management;
* responsible for sending push notifications;
* responsible for determining notification truth.

Its sole purpose is to maintain a useful local materialised representation of Firebase notification data.
