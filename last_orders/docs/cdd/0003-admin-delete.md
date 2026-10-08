# CDD: Admin Delete Service

> **Terminology note:** this document predates ADR-0010. Where it says "Fact"
> it means what the codebase now calls a **Truth** (ADR-0009); the generic
> `components/facts.Fact` envelope it describes has been folded into
> `internal/lastorders/truths` as `truths.Envelope`, dispatched via a native
> `cellar.Fanout[T]` per Truth type rather than a single shared Fanout.

## 1. Purpose

The Admin Delete Service provides a controlled backend mechanism for deleting a
Firebase Authentication user in response to an administrator-created deletion
request. The requesting administrator is not assumed to have direct Firebase
Authentication deletion privileges.

Before deleting the Firebase Authentication account, the service verifies that
the user's application data has already been removed. The service is designed
for safe retry and convergence, rather than exactly-once execution.

```text
user exists
    -> delete user

user does not exist
    -> desired state already achieved
    -> successful outcome
```

---

## 2. Architectural Position

Admin Delete is a housekeeping service following ADR 0008's application
structure and ADR 0009's Truth boundary.

```text
Firestore change
    |
    v
database/listeners/admindelete
    | constructs immutable evidence
    v
truths.AdminDeleteRequested
    | identity + serialised Fact envelope
    v
components/firebaseidempotency
    |
    v
plugins/admindelete
    | Truth-to-service-Cell connectivity
    v
services/admindelete
    | validate, check current preconditions, and delete Auth user
    v
outcome-specific persistence Cell
    | Firestore transaction
    +-- request terminal state
    +-- audit evidence
```

The listener recognises an observation and constructs a Truth. The plugin
answers which service work follows that Truth. The service Cell handlers perform
Admin Delete work. These responsibilities must not be collapsed into one
listener callback.

### 2.1 Package Responsibilities

| Responsibility | Package |
| --- | --- |
| Typed Admin Delete Truth | `internal/lastorders/truths` |
| Firestore change observation | `internal/lastorders/database/listeners/admindelete` |
| Truth-to-service connectivity | `internal/lastorders/plugins/admindelete` |
| Admin Delete Cells, Auth client, request/audit repository | `internal/lastorders/services/admindelete` |
| Reusable Fact and idempotency infrastructure | `internal/lastorders/components` |
| Construction and explicit registration | `internal/lastorders/app` |

A Firebase Authentication client or Firestore repository used only by Admin
Delete is service-specific implementation, not a reusable `components` package.
It may move to `components` only when a genuinely independent reuse case exists.

---

## 3. Truth and Idempotency

### 3.1 AdminDeleteRequested Truth

The listener constructs a typed Truth in `internal/lastorders/truths`. It means:

> An administrator requested deletion of this Firebase Authentication account.

It does not mean that a Firestore listener received a particular document event.
The source `DocumentSnapshot` is transient listener input and must not be
persisted as Truth evidence.

The Truth holds an application-owned immutable snapshot containing the evidence
needed to understand the request:

```go
type AdminDeleteRequestSnapshot struct {
    RequestID        string `json:"request_id"`
    TargetUID        string `json:"target_uid"`
    TargetEmail      string `json:"target_email"`
    RequestedByUID   string `json:"requested_by_uid"`
    RequestedByEmail string `json:"requested_by_email"`
    Reason           string `json:"reason"`
    SchemaVersion    string `json:"schema_version"`
    CreatedAt        string `json:"created_at"`
}

type AdminDeleteRequested struct {
    Request AdminDeleteRequestSnapshot `json:"request"`
}

func (truth AdminDeleteRequested) Identity() string {
    return truth.Request.RequestID
}
```

`requestId` identifies the deletion request and is the Truth identity.
`targetUid` identifies the account being operated on; it is evidence, not the
idempotency key. The exact timestamp representation follows the application
model in force when this service is implemented, but it must be serialisable and
independent of the Firebase SDK.

### 3.2 Listener and Fact Transport

The listener observes `admin_delete_requests` for `ADDED` and `MODIFIED` events.
It considers current document eligibility only to decide whether to construct a
Truth. Only `status == "pending"` is eligible; all other states are ignored.

The listener must not delete a Firebase Authentication user. It receives the
Firestore change, applies the listener gates in section 4, constructs
`AdminDeleteRequested` from the observed document, and serialises that typed
Truth into the existing generic `components/facts.Fact` envelope:

```text
firebaseidempotency.NewCellRequest(
    listenerName,
    truth.Identity(),
    Fact{Name: AdminDeleteRequestedName, Payload: serialisedTruth},
)
```

The generic Fact is durable transport for the typed Truth. It is not the
application-level observation itself. Idempotency enforces the Truth identity;
it does not define it. A modified request may cause another observation, but the
same `requestId` must not establish a second dispatch of the same Truth.

### 3.3 Service Cell Payload and Current State

The service Cell receives the serialised `AdminDeleteRequested` Truth. This
preserves the exact request evidence observed by the listener across
persistence, restart, delay, and retry. It is deliberately not an
identifier-only payload.

Truth evidence answers what was observed. Firestore reads answer what is true
now. A service handler may deliberately read Firestore only for current
conditions: to confirm the request is still pending, check the pause/capability
gates, check that application data is absent, or conditionally persist an
outcome. It must not reload the request to reconstruct Truth evidence or
silently substitute a later `targetUid`.

---

## 4. Listener Gates

### 4.1 Service Enablement

The service may be disabled by deployment or runtime configuration. When
disabled, no Cell is created, the request is not mutated, and it remains
`pending`. This allows the service to be deployed but inactive without
destroying work.

### 4.2 Kill Switch

The operational kill switch is:

```text
system_config/admin_delete
paused: boolean
```

When `paused == true`, no new Cell is created, the request remains `pending`,
and no failure audit is written merely because processing is paused. Clearing
the switch makes a pending request eligible for observation again.

---

## 5. Source Request

Deletion requests are stored in:

```text
admin_delete_requests/{requestId}
```

The Firestore document ID is the request identity. It is distinct from the
target user's identity, and multiple deletion requests for one target UID are
conceptually possible unless a separate business rule prevents them.

The request contract includes:

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

Firestore rules authorise and validate administrator-created requests. The
service does not implement authorisation for request creation.

---

## 6. Service Cells and Outcomes

The Admin Delete plugin connects `AdminDeleteRequested` to the Admin Delete
service Cell. It decides what work follows the Truth; it does not implement the
deletion behaviour or replace Cellar Fanout.

The service handler processes the Truth and either returns a retryable Cellar
failure or creates an outcome-specific persistence Cell. That child Cell has a
typed payload containing the request ID, determined outcome, and evidence
needed to persist it.

```text
AdminDeleteRequested service Cell
    |
    +-- validate immutable Truth evidence
    +-- check current request eligibility and operational gates
    +-- check current application-data preconditions
    +-- dry-run: determine dry_run_validated
    +-- real delete: invoke Firebase Authentication
                     |
                     v
          outcome-specific persistence Cell
                     |
                     v
       Firestore transaction: request state + audit document
```

Invalid, precondition-failed, blocked, and dry-run outcomes create their
corresponding persistence Cell without invoking Firebase Authentication. A
successful Auth deletion or `UserNotFound` creates the `auth_deleted`
persistence Cell.

`Complete{NewCells: ...}` durably schedules the child Cell with the parent's
Cellar progress. It is not a distributed transaction with Firestore or Firebase
Authentication.

---

## 7. Validation and Preconditions

`targetUid` is required and must be a non-empty string. If it is invalid:

```text
outcome = invalid_request
status  = invalid_request
```

No Firebase Authentication operation is attempted.

Before Firebase Authentication deletion, the service checks that both current
application documents are absent:

```text
users/{targetUid}
user-public/{targetUid}
```

If either exists:

```text
outcome = failed_precondition
status  = failed_precondition
```

Firebase Authentication deletion must not be attempted. The persistence payload
records which documents remained. The service does not scrub application data;
that belongs to the separate user-data deletion process.

---

## 8. Dry-Run and Real-Delete Gates

Dry-run performs validation, current application-data precondition checks, and
durable result/audit persistence. It does not call Firebase Authentication.
Successful validation produces the terminal outcome:

```text
pending -> dry_run_validated
```

`dry_run_validated` is a durable result, not transient Cell execution state. It
must not be promoted in place to a real delete. A later destructive deletion
requires an explicit new request, and therefore a new Truth identity.

Real deletion needs both service enablement and the explicit runtime capability
`enable-real-auth-delete`:

```text
service disabled                 -> no processing
service enabled + dry-run        -> validation only
service enabled + real disabled  -> auth_delete_blocked
service enabled + real enabled   -> Auth deletion permitted
```

The service-level configuration name is `ENABLE_ADMIN_DELETE_REQUESTS`.

---

## 9. Authentication Delete and Retry

When validation and preconditions succeed and real deletion is permitted, the
service invokes the service-owned Firebase Authentication client:

```text
FirebaseAuth.DeleteUser(targetUid)
```

The client owns the external call mechanics. The service owns result
interpretation. A pre-delete user-existence check is not required: it cannot
eliminate the race with deletion and adds no convergence guarantee.

The following are successful outcomes:

```text
Firebase confirms deletion -> auth_deleted
Firebase reports UserNotFound -> auth_deleted, idempotent = true
```

Firebase Authentication and Firestore cannot participate in one atomic
transaction. The following is therefore valid:

```text
1. DeleteUser request is sent.
2. Firebase deletes the user.
3. The process terminates before receiving the response.
4. Cellar retries the service Cell.
5. DeleteUser returns UserNotFound.
6. The service persists auth_deleted.
```

Correctness comes from retry, idempotent external operation, and durable
application result. `UserNotFound` must never become a retryable failure.

Known permanent invalid-request or policy failures are terminal. Uncertain
transport or Firebase service failures remain retryable according to Cellar's
normal retry policy. The exact Firebase SDK error mapping must be defined before
production enablement. An ambiguous external failure must not become
`auth_deleted`.

---

## 10. Durable Request State

The request status represents durable application outcome:

```text
pending
invalid_request
failed_precondition
auth_delete_blocked
dry_run_validated
auth_deleted
auth_delete_failed
```

The state transitions are:

```text
pending
    +-- invalid request -------> invalid_request
    +-- precondition failure --> failed_precondition
    +-- real deletion blocked -> auth_delete_blocked
    +-- dry-run success -------> dry_run_validated
    +-- Auth success ----------> auth_deleted
    +-- UserNotFound ----------> auth_deleted
    +-- terminal Auth failure -> auth_delete_failed
```

All outcomes other than `pending` are terminal and are not eligible for new
Cells. The service must conditionally enforce valid transitions so a stale
persistence Cell cannot overwrite a terminal or superseded request.

The application must not use an `auth_deleting` request state. Cellar owns
transient execution state; persisting it would allow a crash to leave a request
permanently claiming that work is in progress.

---

## 11. Persistence and Audit

The outcome-specific persistence handler owns a Firestore transaction which:

1. conditionally writes the request's terminal state; and
2. writes the corresponding audit document.

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

The Firestore transaction prevents a committed terminal request state without
its matching audit evidence. It is separate from Cellar's local transaction. If
Firestore commits but the persistence Cell terminates before Cellar records its
completion, retrying the persistence Cell is safe: it conditionally preserves
the terminal state and rewrites the same audit document.

Audit documents are stored in:

```text
admin_delete_request_audit/{requestId}
```

The document ID is deterministically derived from the unique request ID. There
is one audit document per request, describing its terminal outcome. It is not an
attempt log or mutable workflow state. A persistence retry may harmlessly
rewrite the same terminal evidence.

An audit document contains at least:

```text
requestId
outcome
at
```

It may also contain:

```text
targetUid
reason
error
idempotent
usersDocExists
userPublicDocExists
requestedByUid
```

The request document describes current durable state. Its matching audit
document preserves the historical evidence for that terminal result.

---

## 12. Metrics

Operational metrics are best effort and may maintain:

```text
admin_delete_request_metrics/global
admin_delete_request_metrics/daily-YYYY-MM-DD
```

Metrics may count each terminal outcome. They run only after durable
request/audit persistence and must never fail a deletion, prevent state
persistence, force a retry, or convert success into an application failure.

---

## 13. Safety Invariants

1. Firebase Authentication deletion must never be attempted while either
   `users/{targetUid}` or `user-public/{targetUid}` exists.
2. `UserNotFound` from Firebase Authentication represents successful
   convergence.
3. Repeating an incomplete operation must converge on the desired state.
4. Service disablement and the kill switch leave pending requests pending.
5. The listener only recognises and schedules work; it performs no destructive
   operation.
6. Cellar owns transient execution state.
7. `requestId` identifies the request; `targetUid` identifies the account.
8. Metrics cannot affect correctness.
9. Every terminal request state has matching audit evidence.
10. A request enters `auth_deleted` only when the desired Firebase
    Authentication state has been established.

---

## 14. Responsibility Boundaries

| Responsibility | Owner |
| --- | --- |
| Observe Firestore changes and construct Truth | Admin Delete Listener |
| Apply cheap listener gates | Admin Delete Listener |
| Establish Truth idempotency and emit Fact | Firebase idempotency component |
| Connect Truth to service Cell | Admin Delete plugin |
| Retry execution | Cellar |
| Validate Truth evidence and decide outcome | Admin Delete service handlers |
| Check current application-data preconditions | Admin Delete service handlers |
| Invoke Firebase Authentication | Admin Delete service Auth client |
| Persist request outcome and audit transaction | Admin Delete persistence handler/repository |
| Record metrics | Best-effort metrics publisher |
| Authorise request creation | Firestore rules / application |
| Scrub application data | Separate deletion/scrubbing service |

---

## 15. Testing Contract

The service must be testable without live Firebase Authentication. Tests cover:

* listener construction of immutable `AdminDeleteRequested` evidence and
  idempotency identity based on `requestId`;
* pending `ADDED` and `MODIFIED` requests, plus ignored terminal requests;
* service enablement and kill-switch behaviour;
* missing, empty, and valid target UIDs;
* every application-data precondition combination;
* dry-run without an Auth client invocation;
* successful deletion and `UserNotFound` convergence;
* retryable and terminal Auth failures;
* terminal-state transition enforcement and stale persistence protection;
* a process failure after Firebase succeeds but before result persistence,
  followed by retry to `auth_deleted`;
* request state and its deterministic audit document written together; and
* metrics failure not affecting processing.

---

## 16. Implementation Sequence

1. Add the Truth type, listener snapshot conversion, and listener tests.
2. Register the Truth-to-service connection in the Admin Delete plugin and
   application composition.
3. Implement validation, precondition checks, and outcome payloads.
4. Implement the terminal persistence Cell and its Firestore transaction.
5. Complete the dry-run path.
6. Implement and test the Firebase Authentication client and error mapping.
7. Enable the real-delete gate only after retry semantics are verified.
8. Add best-effort metrics after the correctness path is complete.

---

## 17. Acceptance Criteria

The migration is complete when:

1. A pending Firestore deletion request produces a typed
   `AdminDeleteRequested` Truth.
2. Idempotency dispatches that Truth through the Admin Delete plugin to a
   service Cell.
3. The listener never performs deletion.
4. The service processes immutable Truth evidence and only deliberately reads
   current Firestore conditions.
5. Validation and application-data preconditions are enforced.
6. Dry-run reaches `dry_run_validated` without calling Firebase Authentication.
7. Real deletion requires the explicit capability gate.
8. `UserNotFound` produces `auth_deleted`.
9. A retry after an ambiguous successful Auth deletion converges to
   `auth_deleted`.
10. Each terminal request state and its deterministic audit document are
    persisted in one Firestore transaction.
11. Metrics remain best effort.
12. Disabled service and the kill switch leave pending work untouched.
13. The safety invariants are covered by automated tests.

The resulting architecture is:

> The listener constructs a Truth, the plugin chooses the service work, Cellar
> executes it durably, and the service makes external operations safely
> retryable.
