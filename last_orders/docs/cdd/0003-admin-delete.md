# CDD: Admin Delete Service

## 1. Purpose

The Admin Delete Service provides a controlled backend mechanism for deleting a Firebase Authentication user in response to an administrator-created deletion request.

The service exists because the requesting administrator is not assumed to have direct Firebase Authentication deletion privileges.

Before deleting the Firebase Authentication account, the service verifies that the user's application data has already been removed.

The service is designed for **safe retry and convergence**, rather than exactly-once execution.

In particular, deletion of a Firebase Authentication user is treated as idempotent:

```text
user exists
    → delete user

user does not exist
    → desired state already achieved
    → successful outcome
```

---

# 2. Architectural Position

Admin Delete is a **housekeeping service** running on the new backend architecture.

It is composed of:

1. **Admin Delete Listener**
2. **Admin Delete Cell Handler**
3. **Admin Delete Service**
4. **Firebase Auth adapter**
5. **Audit/metrics persistence**

The architectural flow is:

```text
Firestore
    │
    │ admin_delete_requests change
    ▼
Admin Delete Listener
    │
    │ eligible request
    ▼
Cellar
    │
    │ Admin Delete Cell
    ▼
Admin Delete Handler
    │
    ▼
Admin Delete Service
    │
    ├── validate request
    ├── validate application-data preconditions
    ├── evaluate execution mode
    └── delete Firebase Auth user
             │
             ▼
      Application result
             │
             ▼
       Firestore transaction
         ├── request state
         └── audit record
```

The listener is responsible for **recognising work**.

The Cell handler is responsible for **executing work**.

The service is responsible for **admin-delete domain behaviour**.

The Firebase Auth adapter is responsible for **external-system interaction**.

These responsibilities must not be collapsed into a single listener callback.

---

# 3. Architectural Principles

The service follows these principles.

### 3.1 Listeners recognise work

The listener must not perform Firebase Authentication deletion.

It should:

* receive a Firestore change;
* determine whether the change represents eligible work;
* apply cheap service-level gates;
* create the corresponding Cell.

### 3.2 Cells provide retryable execution

The Cell is the unit of execution and retry.

The Cell handler may be executed more than once.

The service must therefore be safe to re-enter after an incomplete or ambiguous previous attempt.

### 3.3 Services contain domain behaviour

Validation, precondition checking, state transitions and interpretation of Firebase Authentication results belong to the Admin Delete Service rather than the listener.

### 3.4 External systems are behind adapters

Firebase Authentication must be accessed through an application adapter rather than directly from listener infrastructure.

This keeps the service testable and makes the distributed transaction boundary explicit.

### 3.5 Durable state is application state

The Firestore request status represents the durable outcome of the deletion request.

Transient Cell execution state belongs to Cellar.

The application must not duplicate Cellar's execution state merely because a deletion is currently being attempted.

---

# 4. Source Request

Deletion requests are stored in:

```text
admin_delete_requests/{requestId}
```

The Firestore document ID is the **request identity**.

The request identity and target user identity are distinct:

```text
requestId = identity of the deletion request

targetUid = identity of the Firebase Authentication user
```

The service must not use `targetUid` as the request's idempotency key.

Multiple deletion requests for the same UID are therefore conceptually possible unless a separate business rule prevents them.

The current request contract includes fields such as:

```text
schemaVersion
targetUid
targetEmail
requestedByUid
requestedByEmail
reason
scrubbedAppData
status
createdAt
updatedAt
lastError
usersDocExists
userPublicDocExists
authDeletedAt
```

The authoritative Firestore security rules currently require administrator-created requests to contain a valid `schemaVersion`, `targetUid`, requesting identity and other request metadata.

The service does not implement administrator authorisation for request creation.

---

# 5. Trigger

The Admin Delete Listener observes:

```text
admin_delete_requests
```

for:

* `ADDED`
* `MODIFIED`

events.

The listener considers only the current document state.

Only:

```text
status == "pending"
```

is eligible for new work.

All other states are ignored.

This provides the first layer of idempotency:

```text
pending
    → eligible

anything else
    → ignored
```

A request must not generate new Cell work merely because its Firestore document was modified.

---

# 6. Listener Gates

The listener applies operational gates before creating work.

## 6.1 Service enablement

The service may be disabled by deployment/runtime configuration.

When disabled:

* no Cell is created;
* the request is not mutated;
* the request remains `pending`.

This allows the service to be deployed but inactive without destroying work.

## 6.2 Kill switch

The service has an operational kill switch:

```text
system_config/admin_delete
```

with:

```text
paused: boolean
```

When `paused == true`:

* no new Cell is created;
* the request remains `pending`;
* no failure audit is written merely because processing is paused.

When the kill switch is cleared, the request becomes eligible again.

The kill switch therefore **pauses processing rather than changing application state**.

---

# 7. Cell Payload

An eligible request produces an Admin Delete Cell containing sufficient information to identify the work.

At minimum:

```text
requestId
targetUid
```

The Cell must not contain a copied snapshot of the complete request unless there is a specific architectural reason to do so.

The request document remains the durable source of request state.

This prevents the Cell payload becoming a second, potentially stale representation of the request.

---

# 8. Cell Execution

The Admin Delete Cell Handler invokes the Admin Delete Service.

Conceptually:

```text
Cell
  │
  ▼
AdminDeleteService.process(requestId)
  │
  ├── load request
  ├── verify eligibility
  ├── validate request
  ├── check application preconditions
  ├── evaluate execution mode
  └── perform Auth operation when permitted
```

The service must re-check important conditions at execution time.

The listener is not a trusted snapshot of the request.

This is particularly important because:

* the request may have changed between event delivery and Cell execution;
* multiple events may be delivered;
* the Cell may be retried;
* the service may have been paused between scheduling and execution.

---

# 9. Request Validation

`targetUid` is required.

It must be a non-empty string.

If the request is invalid:

```text
outcome = invalid_request
status  = failed_terminal
```

No Firebase Authentication operation is attempted.

The service records an audit event identifying the validation failure.

The exact validation rules for Firebase UID syntax may be tightened later, but the minimum contract is:

```text
targetUid exists
targetUid is a string
targetUid is non-empty
```

---

# 10. Application-Data Preconditions

Before Firebase Authentication deletion is attempted, the service verifies that application data has already been scrubbed.

The following documents must not exist:

```text
users/{targetUid}

user-public/{targetUid}
```

These correspond to the current Firestore data contract.

Both must be absent.

If either exists:

```text
outcome = failed_precondition
status  = failed_precondition
```

and Firebase Authentication deletion must not be attempted.

The service records which documents remain, for example:

```text
usersDocExists = true
userPublicDocExists = false
```

The service does **not** perform the scrubbing itself.

That responsibility belongs to the separate user-data deletion/scrubbing process.

---

# 11. Dry-Run Mode

The service supports a dry-run execution mode.

Dry-run performs:

* request validation;
* application-data precondition checks;
* audit recording;
* result/state recording.

Dry-run does not call Firebase Authentication.

A successful dry-run validation produces:

```text
outcome = dry_run_validated
```

and the request may transition to:

```text
dry_run_validated
```

This state represents a durable result of the request's validation rather than transient Cell execution.

Whether a dry-run validated request may subsequently be promoted to real deletion is an operational decision and must be explicitly controlled.

---

# 12. Real-Delete Gate

Real Firebase Authentication deletion requires an explicit runtime capability.

There are two independent controls:

### Service enablement

Controls whether the service processes requests at all.

### Real-delete capability

Controls whether the service is permitted to perform destructive Firebase Authentication operations.

Therefore:

```text
service disabled
    → no processing

service enabled + dry-run
    → validation only

service enabled + real delete disabled
    → no Auth deletion

service enabled + real delete enabled
    → Auth deletion permitted
```

The existing operational configuration name is:

```text
ENABLE_ADMIN_DELETE_REQUESTS
```

for the service-level gate.

The runtime option:

```text
enable-real-auth-delete
```

controls destructive execution.

---

# 13. Auth Delete

When all validation and preconditions have succeeded and real deletion is permitted, the service invokes:

```text
FirebaseAuth.delete_user(targetUid)
```

The Firebase Authentication adapter owns the mechanics of this operation.

The Admin Delete Service owns interpretation of the result.

---

# 14. Idempotent Authentication Semantics

The following outcomes are considered successful:

### Normal deletion

Firebase Authentication confirms deletion.

Result:

```text
auth_deleted
```

### User already absent

Firebase Authentication reports that the user does not exist.

Result:

```text
auth_deleted
idempotent = true
reason = auth_user_not_found
```

The latter is not an error.

It means the external system is already in the desired state.

This rule is essential for safe retry.

---

# 15. Distributed Transaction Boundary

Firebase Authentication and Firestore cannot participate in one atomic transaction for this operation.

The following sequence is therefore valid:

```text
1. Validate request
2. Validate application data has been scrubbed
3. Delete Firebase Auth user
4. Process terminates
5. Firestore result is not committed
6. Cell is retried
7. Firebase Auth reports UserNotFound
8. Service records auth_deleted
```

The architecture deliberately accepts this possibility.

Correctness therefore comes from:

```text
retry
+
idempotent external operation
+
durable application result
```

rather than exactly-once execution.

The service must never turn `UserNotFound` into a retryable failure.

---

# 16. Durable Request State

The request state represents durable application outcome.

The preferred state model is:

```text
pending
invalid_request
failed_precondition
auth_delete_blocked
auth_deleted
auth_delete_failed
```

An optional:

```text
dry_run_validated
```

state exists where dry-run validation is itself a meaningful durable outcome.

The service should not use:

```text
auth_deleting
```

as a required durable state.

The fact that a Cell is currently executing is already represented by Cellar.

Avoiding `auth_deleting` prevents a worker crash from leaving application state permanently claiming that an operation is in progress.

---

# 17. State Transition Rules

The durable state transition model is:

```text
pending
   │
   ├── invalid request ────────► invalid_request
   │
   ├── precondition failure ───► failed_precondition
   │
   ├── real deletion blocked ──► auth_delete_blocked
   │
   └── Auth deletion
          │
          ├── success ─────────► auth_deleted
          │
          ├── UserNotFound ────► auth_deleted
          │
          └── terminal error ──► auth_delete_failed
```

Dry-run may additionally produce:

```text
pending → dry_run_validated
```

The service must enforce valid transitions.

Terminal states must not normally be reprocessed:

```text
invalid_request
failed_precondition
auth_delete_blocked
auth_deleted
auth_delete_failed
```

are not eligible for new Cells.

---

# 18. Retry Semantics

The service must distinguish between:

### Successful outcomes

The requested external state has been achieved.

Examples:

```text
auth_deleted
UserNotFound → auth_deleted
```

### Terminal application failures

The request cannot safely proceed without intervention or correction.

Examples:

```text
invalid_request
failed_precondition
```

### Retryable external failures

The service cannot determine that the desired state has been achieved.

These should remain retryable according to Cellar's normal retry policy.

The exact Firebase Authentication error classification is an implementation concern of the Auth adapter/service and must be defined before production enablement.

The critical rule is:

> An ambiguous external failure must not be converted into `auth_deleted`.

---

# 19. Application Transaction

When the service has determined a durable outcome, it should commit the corresponding Firestore state and audit information together using the application's transaction mechanism where appropriate.

For example:

```text
request
    status = auth_deleted
    authDeletedAt = timestamp

audit
    requestId
    targetUid
    outcome = auth_deleted
    idempotent = true/false
    at = timestamp
```

The purpose is to prevent the application from recording:

```text
request says deleted
```

without corresponding audit evidence.

Audit persistence is therefore part of the durable application result.

---

# 20. Audit Trail

Audit records are append-only historical evidence.

Collection:

```text
admin_delete_request_audit
```

Document identity is derived from the request and event:

```text
{requestId}_{outcome}_{timestampMicros}
```

An audit record contains at least:

```text
requestId
outcome
at
```

and may contain:

```text
targetUid
reason
error
idempotent
usersDocExists
userPublicDocExists
requestedByUid
```

Audit records must not be updated as a substitute for request state.

A request document describes **current durable state**.

Audit documents describe **what happened**.

---

# 21. Metrics

Operational metrics are best effort.

The service may maintain:

```text
admin_delete_request_metrics/global
admin_delete_request_metrics/daily-YYYY-MM-DD
```

with counters and last-outcome information.

Metrics may include:

```text
invalid_request
failed_precondition
dry_run_validated
auth_delete_blocked
auth_deleted
auth_delete_failed
```

Metrics must never determine correctness.

A metrics failure must not:

* fail a deletion;
* prevent request-state persistence;
* force Cell retry;
* convert a successful deletion into an application failure.

The ordering is therefore:

```text
correct application result
        ↓
durable request/audit state
        ↓
best-effort metrics
```

---

# 22. Safety Invariants

The following invariants are mandatory.

### Invariant 1 — Application data first

Firebase Authentication deletion must never be attempted while either:

```text
users/{targetUid}
user-public/{targetUid}
```

exists.

### Invariant 2 — Auth absence is success

`UserNotFound` from Firebase Authentication represents successful convergence.

### Invariant 3 — Safe retry

Repeating an incomplete operation must converge on the desired state.

### Invariant 4 — Pending work survives pauses

Service disablement and the kill switch must leave pending requests pending.

### Invariant 5 — Listener does not perform destructive work

The listener only recognises and schedules work.

### Invariant 6 — Cellar owns execution state

Transient worker execution state must not unnecessarily become durable application state.

### Invariant 7 — Request identity is distinct from target identity

`requestId` identifies the requested operation.

`targetUid` identifies the account being operated upon.

### Invariant 8 — Metrics cannot affect correctness

Metrics failure cannot cause deletion failure or false success.

### Invariant 9 — Audit is historical

Audit records are append-only evidence, not mutable workflow state.

### Invariant 10 — No false success

The request may enter `auth_deleted` only after the service has established that the desired Firebase Authentication state has been achieved.

---

# 23. Responsibility Boundaries

| Responsibility                       | Component                           |
| ------------------------------------ | ----------------------------------- |
| Observe Firestore changes            | Admin Delete Listener               |
| Determine request eligibility        | Admin Delete Listener               |
| Apply service/kill-switch gates      | Admin Delete Listener               |
| Create work                          | Cellar integration                  |
| Retry execution                      | Cellar                              |
| Load and validate request            | Admin Delete Service                |
| Check application-data preconditions | Admin Delete Service                |
| Decide dry-run vs real operation     | Admin Delete Service                |
| Invoke Firebase Auth                 | Firebase Auth Adapter               |
| Interpret Auth result                | Admin Delete Service                |
| Enforce state transitions            | Admin Delete Service                |
| Persist request outcome              | Admin Delete Service                |
| Persist audit evidence               | Admin Delete Service                |
| Record operational metrics           | Metrics component                   |
| Authorise request creation           | Firestore rules / application       |
| Scrub application data               | Separate deletion/scrubbing service |

---

# 24. Listener-to-Service Contract

The listener should expose a deliberately small contract to the rest of the architecture.

Conceptually:

```text
AdminDeleteListener
    └── submit(requestId, targetUid)
```

The listener does not need to know:

* how users are validated;
* how application data is checked;
* how Firebase Auth deletion works;
* what constitutes `UserNotFound`;
* how audit records are constructed;
* how metrics are counted.

Those belong to the service.

The resulting dependency direction is:

```text
Firestore event infrastructure
        ↓
Admin Delete Listener
        ↓
Cellar
        ↓
Admin Delete Handler
        ↓
Admin Delete Service
        ↓
Firebase Auth Adapter
```

rather than:

```text
Firestore listener
    └── everything
```

---

# 25. Testing Contract

The service should be testable without requiring live Firebase Authentication.

At minimum, tests must cover:

### Listener

* ADDED pending request creates Cell.
* MODIFIED pending request creates Cell.
* non-pending request is ignored.
* disabled service leaves request untouched.
* kill switch leaves request untouched.

### Validation

* missing target UID.
* empty target UID.
* valid target UID.

### Preconditions

* neither application document exists.
* `users` document exists.
* `user-public` document exists.
* both documents exist.

### Dry-run

* validation succeeds.
* Auth adapter is not invoked.
* appropriate durable result is recorded.

### Auth deletion

* successful deletion.
* UserNotFound becomes successful idempotent deletion.
* retryable Auth failure.
* terminal Auth failure.

### State transitions

* valid transitions succeed.
* illegal transitions are rejected.
* terminal requests cannot be reprocessed.

### Reliability

* Auth deletion succeeds but application process fails before result persistence.
* retry receives UserNotFound.
* retry converges on `auth_deleted`.

### Observability

* audit is produced for significant outcomes.
* metrics failure does not affect processing.

---

# 26. Implementation Sequence

The service should be implemented incrementally.

### Phase 1 — Listener

Implement:

```text
Firestore event
    ↓
Admin Delete Listener
    ↓
log eligible request
```

No deletion and no Cell creation initially.

### Phase 2 — Cell creation

Implement:

```text
eligible request
    ↓
Admin Delete Cell
```

The Cell may initially log its payload.

### Phase 3 — Service validation

Implement:

```text
request validation
application-data preconditions
state transition handling
```

### Phase 4 — Dry-run

Implement the complete non-destructive workflow.

This establishes the majority of the safety architecture without enabling Auth deletion.

### Phase 5 — Auth adapter

Implement and test the Firebase Authentication adapter.

### Phase 6 — Real deletion gate

Enable real deletion only after the dry-run path and retry semantics are verified.

### Phase 7 — Audit and metrics hardening

Complete operational observability once the correctness path is established.

---

# 27. Migration Relationship to Existing Implementation

The existing Admin Delete implementation is the behavioural reference for the migration.

It currently performs work through:

```text
admin_delete_requests
```

and maintains:

```text
admin_delete_request_audit
admin_delete_request_metrics
system_config/admin_delete
```

The new architecture must preserve externally observable safety behaviour while moving responsibilities into the new boundaries.

In particular, the following existing behaviours are retained:

* pending-only processing;
* environment/service gating;
* kill-switch behaviour;
* application-data preconditions;
* dry-run support;
* real-delete gating;
* Auth `UserNotFound` idempotency;
* append-only audit;
* best-effort metrics;
* request state validation.

The following architectural behaviour is intentionally changed:

* the listener no longer performs the deletion;
* Cellar owns execution and retry;
* durable request state does not need an `auth_deleting` state;
* Firebase Authentication is accessed through an adapter;
* transient execution state is not represented as application state.

---

# 28. Acceptance Criteria

The Admin Delete Service may be considered migrated to the new architecture when:

1. A pending Firestore deletion request is detected by the new listener.
2. The listener creates an Admin Delete Cell rather than performing the deletion.
3. The Cell invokes the Admin Delete Service.
4. Validation and application-data preconditions are enforced.
5. Dry-run execution completes without touching Firebase Authentication.
6. Real deletion requires the explicit real-delete gate.
7. Firebase Authentication `UserNotFound` produces `auth_deleted`.
8. A retry after an ambiguous successful Auth deletion converges successfully.
9. Request state and audit evidence are persisted correctly.
10. Metrics remain best effort.
11. Disabled service and kill-switch leave pending work untouched.
12. Existing safety invariants are covered by automated tests.
13. No legacy `sub_events` implementation is required for the migrated workflow.

The milestone is therefore:

> **Admin Delete runs entirely through the new Listener → Cellar → Handler → Service architecture, with the legacy implementation no longer responsible for processing deletion requests.**

---

# 29. Architectural Summary

The essential design is:

```text
                 Firestore
                     │
                     │ change
                     ▼
          ┌─────────────────────┐
          │ Admin Delete        │
          │ Listener            │
          │                     │
          │ pending?            │
          │ enabled?            │
          │ kill switch clear?  │
          └──────────┬──────────┘
                     │
                     │ Cell
                     ▼
              ┌──────────────┐
              │    Cellar    │
              └──────┬───────┘
                     │
                     ▼
          ┌─────────────────────┐
          │ Admin Delete        │
          │ Handler             │
          └──────────┬──────────┘
                     │
                     ▼
          ┌─────────────────────┐
          │ Admin Delete        │
          │ Service             │
          │                     │
          │ validate            │
          │ preconditions       │
          │ dry-run             │
          │ state transitions   │
          └───────┬───────┬─────┘
                  │       │
                  │       └──────────────┐
                  ▼                      ▼
          ┌───────────────┐      ┌───────────────┐
          │ Firebase Auth │      │   Firestore   │
          │    Adapter    │      │               │
          └───────────────┘      │ request       │
                                  │ audit         │
                                  └───────────────┘

                         ┌────────────────┐
                         │ Metrics        │
                         │ (best effort)  │
                         └────────────────┘
```

The core architectural rule is:

> **The listener recognises work, Cellar executes work, the service owns the domain decision, and external operations are made safely retryable.**
