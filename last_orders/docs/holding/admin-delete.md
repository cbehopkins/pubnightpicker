# Admin Delete Service Specification

## 1. Purpose

Provide a controlled backend mechanism for deleting a Firebase Authentication user in response to an administrator-created deletion request.

The service exists because the requesting administrator is not assumed to have direct Firebase Authentication deletion privileges.

The service must ensure that application data belonging to the target user has already been removed before attempting to delete the corresponding Firebase Authentication account.

The design must be safe to retry. In particular, Firebase Authentication deletion is treated as an idempotent operation for this service.

---

## 2. Scope

### In scope

* Monitoring administrator deletion requests in Firestore.
* Identifying eligible deletion requests.
* Creating Cellar work for eligible requests.
* Validating the deletion request.
* Checking application-data deletion preconditions.
* Deleting the Firebase Authentication user.
* Recording the resulting request state.
* Recording an append-only audit trail.
* Supporting dry-run operation.
* Supporting an operational kill switch.
* Handling retries and ambiguous external-operation outcomes.

### Out of scope

* Front-end administration UX.
* The mechanism by which an administrator is authorised to create a deletion request.
* The implementation of application-data scrubbing itself.
* Atomic transactions spanning Firestore and Firebase Authentication.
* Detailed metrics implementation.

---

# 3. Architecture

The service consists of two principal components:

1. **Admin Delete Listener**
2. **Admin Delete Cell Handler**

The listener is responsible for recognising eligible requests and creating Cellar work.

The Cell is responsible for performing the actual deletion operation and producing the resulting application transaction.

The listener must not perform the Firebase Authentication deletion itself.

---

# 4. Request Collection

Deletion requests are stored in:

```text
admin_delete_requests/{requestId}
```

The Firestore document ID is the **request identity**.

The request contains, at minimum:

```text
targetUid: string
status: string
```

Other request metadata may be retained, including:

```text
targetEmail
requestedByUid
reason
schemaVersion
createdAt
```

The exact request schema is to be specified separately.

`requestId` identifies the requested operation.

`targetUid` identifies the Firebase Authentication user being operated upon.

These concepts must not be conflated.

---

# 5. Listener Behaviour

The Admin Delete Listener monitors:

```text
admin_delete_requests
```

for:

* `ADDED`
* `MODIFIED`

events.

Only requests whose current status is:

```text
pending
```

are eligible for processing.

All other request states are ignored.

Before creating a Cell, the listener must apply the service-level operational gates.

---

# 6. Operational Gates

## 6.1 Environment/service gate

The service may be disabled through its deployment/runtime configuration.

When disabled:

* no Cell is created;
* the request is not mutated;
* the request remains pending.

This allows the service to be disabled without destroying pending work.

## 6.2 Kill switch

A runtime kill switch must be available to prevent new deletion work from being initiated.

The kill-switch state is authoritative for deciding whether a pending request may proceed.

When the kill switch is active:

* no Cell is created;
* the request remains pending;
* no failure state is recorded merely because the service is paused.

When the kill switch is subsequently removed, the pending request remains eligible for processing.

## 6.3 Real-delete gate

The service must support a dry-run mode.

When running in dry-run mode, the Cell performs all validation and precondition checks but does not call Firebase Authentication to delete the user.

Real Authentication deletion requires the service to be explicitly configured to permit it.

This provides an independent safety mechanism during development, testing and operational bring-up.

---

# 7. Cell Creation

For each eligible request, the listener creates an Admin Delete Cell containing at least:

```text
requestId
targetUid
```

The Cell is the unit of execution and retry.

The listener should not perform application-data checks or Firebase Authentication deletion before creating the Cell.

Those operations belong to the Cell handler so that they occur as part of the retryable unit of work.

---

# 8. Admin Delete Cell

The Admin Delete Cell performs one complete deletion attempt.

The handler must:

1. Validate the request data.
2. Validate the application-data deletion precondition.
3. Respect dry-run configuration.
4. Attempt Firebase Authentication deletion when permitted.
5. Produce the appropriate application transaction/audit result.

The handler must be safe to execute multiple times.

---

# 9. Request Validation

`targetUid` is required.

If `targetUid` is missing, empty or otherwise invalid:

* no Authentication deletion is attempted;
* the request is transitioned to a terminal invalid-request state;
* an audit record is written identifying the validation failure.

The exact validation rules for a Firebase UID are implementation-defined, subject to being stricter than merely accepting an arbitrary empty/non-empty string if appropriate.

---

# 10. Application-Data Preconditions

Before attempting Firebase Authentication deletion, the Cell must verify that the target user's application data has already been removed.

The following documents must not exist:

```text
users/{targetUid}
user-public/{targetUid}
```

If either document exists:

* Firebase Authentication deletion must not be attempted;
* the request is transitioned to a failed-precondition state;
* an audit record records which documents still exist.

The absence of these documents is a prerequisite for Authentication deletion.

The service does not perform application-data scrubbing itself.

---

# 11. Firebase Authentication Deletion

If all validation and preconditions succeed, and real deletion is enabled, the Cell attempts:

```text
Firebase Authentication: delete targetUid
```

The operation is considered successful when:

1. Firebase Authentication reports successful deletion; or
2. Firebase Authentication reports that the user does not exist.

The second case is explicitly treated as an idempotent success.

For example:

```text
delete UID X
    ↓
UserNotFound
    ↓
desired end state already exists
    ↓
auth_deleted
```

This behaviour is essential to safe Cell retry.

---

# 12. Retry and Failure Semantics

The service must not depend upon exactly-once execution.

A Cell may be retried after an ambiguous failure.

For example:

```text
Cell
  ↓
Firebase Auth deletion succeeds
  ↓
worker/process fails before result is committed
  ↓
Cell retried
  ↓
Firebase Auth reports UserNotFound
  ↓
treat as successful deletion
```

This is an intentional part of the design.

The service therefore relies on **convergence through idempotent re-execution**, rather than attempting to provide an impossible atomic transaction spanning Firebase Authentication and Firestore.

Transient Authentication failures should remain retryable according to the normal Cellar retry policy.

Permanent/terminal failures should be recorded as such.

The precise retry classification for Firebase Authentication errors is to be specified when the Authentication adapter is implemented.

---

# 13. State Model

The request state represents the durable outcome of the deletion request, rather than the internal execution state of the Cell.

The proposed durable states are:

```text
pending
invalid_request
failed_precondition
auth_delete_blocked
auth_deleted
auth_delete_failed
```

The exact naming is subject to implementation review.

In particular, the service should **not require an `auth_deleting` state** merely to represent that a Cell is currently executing.

Cellar already provides execution state for the work itself.

This avoids creating a durable state which can become permanently stuck if a worker dies immediately after recording `auth_deleting`.

Conceptually:

```text
pending
   │
   ├── invalid request ───────► invalid_request
   │
   ├── precondition failed ───► failed_precondition
   │
   ├── dry-run ────────────────► validation/result state
   │
   └── Auth deletion
          │
          ├── success ────────► auth_deleted
          ├── UserNotFound ───► auth_deleted
          └── terminal error ─► auth_delete_failed
```

Only the appropriate transitions from the current request state may be made.

The exact state-transition table will be formalised during implementation.

---

# 14. Idempotency

The Firestore request document ID (`requestId`) identifies the deletion request.

Only pending requests are eligible for new processing.

A request in a terminal state must not normally cause another Cell to be created.

The Firebase Authentication operation itself is independently idempotent:

```text
existing user → delete
missing user  → successful desired state
```

This combination provides the required protection against duplicate processing.

The `targetUid` must not be used as the request's sole idempotency key, because two separately-created requests may refer to the same target user.

Whether the application should prevent multiple deletion requests for the same UID is a separate business rule and is not part of this idempotency contract.

---

# 15. Application Transaction

On successful completion, the Cell produces an application transaction containing the durable request-state update and associated audit information.

For example:

```text
request:
    status = auth_deleted
    authDeletedAt = ...

audit:
    requestId
    targetUid
    outcome = auth_deleted
    idempotent = true/false
    timestamp
```

The request-state update and audit record should be committed together using the application's existing transaction mechanism.

This ensures that the durable application state and its corresponding audit evidence are kept consistent.

---

# 16. Distributed Transaction Boundary

Firebase Authentication and Firestore cannot be treated as one atomic transaction by this service.

The following sequence is therefore inherently possible:

```text
1. Validate application preconditions
2. Delete Firebase Auth user
3. Process terminates before Firestore result is committed
```

The design must explicitly tolerate this condition.

The recovery mechanism is Cell retry combined with the `UserNotFound → auth_deleted` rule.

The service must therefore **never interpret a repeated `UserNotFound` response as an error requiring manual intervention**.

---

# 17. Audit Trail

The service should maintain an append-only audit record for significant outcomes.

The audit record should contain at least:

```text
requestId
targetUid
outcome
timestamp
```

Additional information may include:

```text
reason
error
idempotent
usersDocExists
userPublicDocExists
requestedByUid
```

Audit records are historical evidence and must not be treated as mutable request state.

The precise Firestore collection and document-ID strategy will be specified during implementation.

---

# 18. Metrics

Operational metrics may record outcomes such as:

```text
invalid_request
failed_precondition
auth_delete_blocked
auth_deleted
auth_delete_failed
```

Metrics are explicitly **best effort**.

A metrics failure must never:

* cause the deletion to fail;
* prevent the request state from being updated;
* cause the Cell to be retried.

Metrics are operational observability, not part of the deletion's correctness mechanism.

---

# 19. Dry-Run Behaviour

In dry-run mode, the Cell performs:

* request validation;
* application-data precondition checks;
* audit/result generation;

but does not invoke Firebase Authentication deletion.

Dry-run must therefore exercise the majority of the safety path without performing the destructive external operation.

Dry-run should not require a separate permanent execution state merely to indicate that the Cell was run without deletion.

---

# 20. Safety Properties

The implementation must preserve the following invariants.

### Invariant 1 — Application data first

Firebase Authentication deletion must not be attempted while either required application document still exists.

### Invariant 2 — Safe retry

Repeating a deletion attempt must converge on the same desired state.

### Invariant 3 — Auth absence is success

A missing Firebase Authentication user is equivalent to a successfully deleted user.

### Invariant 4 — Pending requests survive operational pauses

Disabling the service or activating the kill switch must not destroy or permanently disable pending requests.

### Invariant 5 — Cache/metrics/audit failures must not create false deletion success

Only the successful application result may transition the request to `auth_deleted`.

### Invariant 6 — Cell execution state is not application state

Transient execution conditions should be handled by Cellar rather than being unnecessarily represented as durable request states.

---

# 21. Summary of Intended Architecture

```text
Firestore
    │
    │ admin_delete_requests
    ▼
┌──────────────────────────┐
│ Admin Delete Listener    │
│                          │
│ pending?                 │
│ service enabled?         │
│ kill switch clear?       │
└────────────┬─────────────┘
             │
             ▼
      Admin Delete Cell
             │
             ├── validate request
             │
             ├── check users/{uid}
             │
             ├── check user-public/{uid}
             │
             ├── dry-run?
             │
             └── Firebase Auth delete
                       │
             ┌─────────┴─────────┐
             │                   │
          success            UserNotFound
             │                   │
             └─────────┬─────────┘
                       │
                       ▼
              Cell application result
                       │
                       ▼
              Firestore transaction
                 ├── request state
                 └── audit record

Metrics are best-effort and independent.
```

The fundamental design principle is:

> **Don't try to make the distributed operation exactly-once. Make the operation safely repeatable and let Cellar's retry semantics provide convergence.**
