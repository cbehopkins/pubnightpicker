# Notification Mirror Service — Working Specification

**Status:** Working specification / design capture
**Purpose:** Capture the current architectural understanding of the Notification Mirror listener for inclusion in the backend requirements specification.

---

## 1. Purpose

The Notification Mirror provides a simple **frontend-to-backend liveness/round-trip mechanism**.

Its purpose is to allow the frontend to determine whether the backend is:

* alive;
* monitoring Firebase as expected; and
* capable of responding to a frontend-generated notification request.

The mechanism exists primarily to detect situations where the backend has silently stopped operating.

Without this mechanism, a backend failure may remain unnoticed until users report that other functionality has stopped working.

The Notification Mirror provides a lightweight way for the frontend to detect this condition at important decision points in the application.

---

# 2. High-Level Behaviour

The frontend writes a notification request into Firebase.

The backend monitors the notification request collection.

When the backend observes a new request, it creates a corresponding acknowledgement in the notification ACK collection.

The frontend monitors the ACK collection.

The resulting round trip is:

```text id="q0j3wp"
Frontend
    │
    │ create notification request
    ▼
Firebase
Notification Requests
    │
    │ backend listener
    ▼
Backend
    │
    │ create acknowledgement
    ▼
Firebase
Notification ACKs
    │
    │ frontend listener
    ▼
Frontend
    │
    ▼
Backend responsiveness confirmed
```

The Notification Mirror does not perform any additional business processing.

---

# 3. Firebase Collections

The mechanism uses two Firebase collections.

### Notification Requests

The frontend creates documents in this collection.

These documents represent a request for the backend to acknowledge that it is alive and processing Firebase events.

### Notification ACKs

The backend creates documents in this collection.

An ACK corresponds to a notification request that the backend has observed and processed.

The frontend monitors this collection to determine whether its request has been acknowledged.

---

# 4. Backend Listener

The backend monitors the **Notification Requests** collection.

The listener is interested in the creation of notification request documents.

When a new request is observed, the backend:

1. identifies the request;
2. checks the Notification ACK collection for an existing corresponding ACK;
3. if no ACK exists, creates the ACK;
4. if an ACK already exists, performs no write and records the situation in the backend logs.

The normal processing path is therefore:

```text id="f8m2qy"
Notification Request created
          │
          ▼
Backend listener
          │
          ▼
Check Notification ACKs
          │
     ┌────┴────┐
     │         │
   absent    exists
     │         │
     ▼         ▼
Create ACK   Log anomaly
```

---

# 5. Idempotency

The desired implementation includes an explicit idempotency check.

When a notification request is received, the backend checks whether the corresponding ACK already exists.

The existence of the ACK acts as the durable indication that the request has already been processed.

Conceptually:

```text id="z7c1mv"
Request ID
    │
    ▼
Does corresponding ACK exist?
    │
    ├── yes → already processed
    │          log anomaly
    │          do nothing
    │
    └── no  → create ACK
```

The current backend may not perform this explicit check, but it is a requirement for the redesigned service.

---

# 6. Duplicate / Idempotency Anomaly

If the backend receives a notification request for which an ACK already exists, this indicates that an unusual condition has occurred.

The backend should:

* recognise the request as already acknowledged;
* avoid creating another ACK;
* log an error/anomaly.

No further recovery or business processing is required.

The purpose of the logging is primarily diagnostic.

This is deliberately a very lightweight response.

---

# 7. No Cellar Processing Required

The Notification Mirror is intentionally **not implemented using Cellar Cells**.

Unlike the New Poll and Completed Poll notification pipelines, it does not represent a unit of application work requiring:

* fan-out;
* external service calls;
* complex retry semantics;
* durable workflow state;
* asynchronous business processing.

The operation is simply:

```text id="n5w8se"
Firebase Request
      │
      ▼
Firebase Lookup
      │
      ▼
Firebase ACK Write
```

The listener should therefore operate directly against Firebase.

Cellar should not be introduced merely for architectural consistency.

---

# 8. Frontend Behaviour

The frontend creates a notification request at appropriate decision points.

It then waits for the corresponding ACK.

If the ACK is observed, the frontend considers the backend round trip successful.

If the expected ACK is not observed within an appropriate period, the frontend can alert the user that there may be a problem with the backend/system.

This gives the frontend an actionable indication of backend failure rather than relying on users to discover problems indirectly.

The exact frontend timeout and user-facing behaviour are outside the scope of this backend listener specification.

---

# 9. ACK Payload

The precise contents of the ACK document have **not yet been confirmed**.

The intended behaviour is that the ACK represents/mirrors the corresponding notification request, but the exact fields and whether the document is an exact copy require confirmation from the existing implementation.

This should therefore remain an explicit open requirement rather than being inferred.

To be confirmed:

* which fields from the request are copied;
* whether the ACK is an exact document copy;
* whether any additional ACK-specific fields are added;
* whether the request document ID is reused as the ACK document ID.

---

# 10. Event Semantics

The intended listener trigger is the **creation of a notification request document**.

A subsequent modification to an existing request should not automatically generate another ACK unless explicitly required by the frontend protocol.

The request/ACK relationship should therefore be treated as:

```text id="d4x7pk"
one notification request
        │
        ▼
one corresponding ACK
```

The exact behaviour for modified request documents remains subject to confirmation if the existing frontend implementation relies on modification events.

---

# 11. Responsibilities

### Frontend

The frontend is responsible for:

* creating notification requests;
* waiting for corresponding ACKs;
* determining whether the ACK arrived within the expected period;
* notifying the user if the backend appears unresponsive.

### Backend Notification Mirror

The backend is responsible for:

* monitoring notification requests;
* identifying new requests;
* checking for an existing ACK;
* creating the ACK when required;
* logging duplicate/already-acknowledged requests.

### Firebase

Firebase provides the shared communication mechanism between frontend and backend:

```text id="k3r6vz"
Frontend → Notification Requests → Backend
Backend  → Notification ACKs     → Frontend
```

---

# 12. Non-Responsibilities

The Notification Mirror does not:

* send email;
* send push notifications;
* invoke external notification services;
* perform business processing;
* create a Cellar workflow;
* maintain a complex application state machine;
* retry individual business operations;
* determine whether the wider application is healthy.

It provides a narrow **event-processing and round-trip liveness signal**.

---

# 13. Summary

The Notification Mirror is a deliberately simple Firebase-to-Firebase acknowledgement mechanism.

The frontend writes:

```text id="m8y2qa"
Notification Request
```

The backend observes the request and, if it has not already been acknowledged, writes:

```text id="w4p7nx"
Notification ACK
```

The frontend observes the ACK and considers the backend responsive.

The redesigned implementation should add an explicit idempotency check based on the existence of the corresponding ACK.

If an ACK already exists, the backend should not write another ACK and should log the unexpected duplicate/already-processed condition.

No Cellar Cell is required.

The fundamental architectural principle is:

> **The Notification Mirror is a lightweight Firebase round-trip liveness mechanism, not a business workflow. The backend observes a frontend-created request and mirrors it into an acknowledgement, using the existing ACK as its idempotency state.**
