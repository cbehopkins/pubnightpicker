# Admin Delete Request Processing Specification

## 1. Purpose

Provide a controlled mechanism for privileged backend workers to delete a Firebase Auth user after an admin-triggered request is written to Firestore.

The requesting admin is assumed not to have direct Firebase Auth delete privileges.

## 2. Scope and non-goals

In scope:
- Request intake from Firestore documents
- Request validation and lifecycle management
- Optional Firebase Auth deletion
- Audit logging and operational metrics
- Kill-switch and runtime gating controls

Out of scope:
- Front-end UX
- Admin authorisation model for creating request documents
- Data scrubbing implementation details (this service only checks preconditions)

## 3. Actors and responsibilities

- Admin client: writes a delete request document.
- Admin delete worker: watches request documents and processes eligible requests.
- Firebase Auth: target system for user account deletion.
- Firestore: source of truth for requests, audit records, kill-switch, and metrics.

## 4. Trigger and eligibility

The worker observes document changes in collection admin_delete_requests:
- Process on ADDED and MODIFIED events.
- Only process documents whose current status is pending.
- Ignore all non-pending documents.

Reference wiring:
- [firebase_sub/runtime/event_producers.py](firebase_sub/runtime/event_producers.py)
- [firebase_sub/plugins/admin_delete_request.py](firebase_sub/plugins/admin_delete_request.py)
- [firebase_sub/database/admin_delete_requests.py](firebase_sub/database/admin_delete_requests.py)

## 5. Runtime controls

Two independent controls gate behaviour.

Control A: environment gate
- Name: ENABLE_ADMIN_DELETE_REQUESTS
- If false, the listener is disabled and requests are ignored.

Control B: runtime gate
- CLI/runtime option: enable-real-auth-delete
- If false, the worker runs in dry-run mode (validation and auditing only).
- If true, real Firebase Auth deletion is allowed, subject to Control A and kill-switch.

Kill-switch:
- Document: system_config/admin_delete
- If paused is true, request processing is skipped early.

Operational references:
- [README.md](README.md#L112)
- [firebase_sub/cli/sub_events.py](firebase_sub/cli/sub_events.py)
- [firebase_sub/runtime/config.py](firebase_sub/runtime/config.py)

## 6. Processing workflow

For each eligible request document:

1. Check service enabled.
- If disabled, return without writes.

2. Check kill-switch.
- If paused, return without mutating request or writing audit.

3. Validate request payload.
- Required: targetUid must be a non-empty string.
- If missing/invalid:
  - Write audit outcome invalid_request with reason missing_target_uid.
  - Set request status to failed_terminal with lastError missing_target_uid.
  - Stop.

4. Validate precondition: user data already scrubbed.
- Check whether users/{targetUid} exists.
- Check whether user-public/{targetUid} exists.
- If either exists:
  - Write audit outcome failed_precondition with reason user_docs_still_exist and existence flags.
  - Set request status to failed_precondition with lastError and existence flags.
  - Stop.

5. Mark validation success.
- Write audit outcome dry_run_validated.
- Transition status to dry_run_validated.

6. Dry-run branch.
- If dry_run is true, stop here.

7. Real delete guard.
- If dry_run is false but enable_real_auth_delete is false:
  - Write audit outcome auth_delete_blocked with reason real_auth_delete_not_enabled.
  - Set status auth_delete_blocked with lastError.
  - Stop.

8. Execute real Auth deletion.
- Write audit outcome auth_deleting.
- Transition status to auth_deleting.
- Attempt delete of Firebase Auth user targetUid.

9. Handle Auth deletion result.
- Success:
  - Write audit outcome auth_deleted.
  - Set status auth_deleted and authDeletedAt server timestamp.
- UserNotFoundError:
  - Treat as idempotent success.
  - Write auth_deleted with idempotent true and reason auth_user_not_found.
  - Set status auth_deleted and authDeletedAt server timestamp.
- Any other error:
  - Write auth_delete_failed with error class and message.
  - Set status auth_delete_failed with lastError.

## 7. Request state machine

States:
- pending
- dry_run_validated
- failed_precondition
- failed_terminal
- auth_deleting
- auth_deleted
- auth_delete_failed
- auth_delete_blocked

Allowed transitions:
- pending -> dry_run_validated
- pending -> failed_precondition
- pending -> failed_terminal
- dry_run_validated -> auth_deleting
- dry_run_validated -> auth_delete_blocked
- auth_deleting -> auth_deleted
- auth_deleting -> auth_delete_failed

All other transitions are invalid and must be rejected.

Terminal states:
- failed_precondition
- failed_terminal
- auth_deleted
- auth_delete_failed
- auth_delete_blocked

Reference:
- [firebase_sub/database/admin_delete_requests.py](firebase_sub/database/admin_delete_requests.py)

## 8. Idempotency and reliability expectations

- Non-pending requests are skipped, preventing repeat processing after terminal outcome.
- UserNotFoundError during Auth delete is mapped to successful auth_deleted for idempotency.
- Status transition validation prevents illegal lifecycle rewrites.
- Audit records are append-only (immutable style) via unique composite document IDs.
- Metrics writes are best-effort and must never block processing.

## 9. Firestore schema (inferred)

This section captures observed fields and semantics from code and tests. Treat fields marked optional as implementation-defined.

### 9.1 Collection: admin_delete_requests

Path:
- admin_delete_requests/{requestId}

Observed request fields written by producers/admin client:
- schemaVersion: number, optional (observed value 1)
- targetUid: string, required for processing
- targetEmail: string, optional
- requestedByUid: string, optional
- reason: string, optional
- scrubbedAppData: boolean, optional advisory field
- status: string enum, required for lifecycle routing
- createdAt: timestamp, optional but expected

Observed worker-managed fields:
- status: lifecycle state
- updatedAt: server timestamp on transition
- lastError: string, optional
- usersDocExists: boolean, optional
- userPublicDocExists: boolean, optional
- authDeletedAt: server timestamp, optional

Status enum values:
- pending
- dry_run_validated
- failed_precondition
- failed_terminal
- auth_deleting
- auth_deleted
- auth_delete_failed
- auth_delete_blocked

### 9.2 Collection: admin_delete_request_audit

Path:
- admin_delete_request_audit/{auditRecordId}

Document ID format:
- {requestId}_{outcome}_{timestampMicros}

Base fields:
- requestId: string
- outcome: string
- at: ISO 8601 UTC datetime string

Additional payload fields vary by outcome, including:
- targetUid
- reason
- error
- idempotent
- usersDocExists
- userPublicDocExists

Design intent:
- Immutable event log of transitions and outcomes.

### 9.3 Collection: admin_delete_request_metrics

Path:
- admin_delete_request_metrics/global
- admin_delete_request_metrics/daily-YYYY-MM-DD

Fields:
- updatedAt: server timestamp
- lastOutcome: string
- lastRequestId: string
- total: numeric counter increment
- outcomes: map of outcome -> numeric counter increment

Expected key outcomes tracked for alerting:
- auth_delete_failed
- auth_delete_blocked

### 9.4 Kill-switch document

Path:
- system_config/admin_delete

Fields:
- paused: boolean
- reason: string, optional
- pausedAt: timestamp, optional

Semantics:
- paused true causes immediate skip of request processing.

### 9.5 Precondition collections checked

Paths:
- users/{targetUid}
- user-public/{targetUid}

Semantics:
- Both documents must be absent before Auth deletion can proceed.

## 10. Outcome taxonomy

Outcomes written to audit and metrics include:
- invalid_request
- failed_precondition
- dry_run_validated
- auth_delete_blocked
- auth_deleting
- auth_deleted
- auth_delete_failed

Note:
- invalid_request is an outcome, but request status becomes failed_terminal.

## 11. Minimal behavioural contract for reimplementation

A conforming reimplementation must:
- Watch admin_delete_requests changes (add and modify).
- Process pending only.
- Enforce kill-switch short-circuit.
- Enforce targetUid presence.
- Enforce users/user-public absence precondition.
- Enforce the exact state transition graph above.
- Support dry-run and real-delete modes with dual-gate control.
- Write append-only audit records for every major outcome/transition.
- Emit best-effort global and daily outcome counters.
- Treat Auth user-not-found as successful idempotent deletion.

## 12. Evidence sources

Primary implementation and verification references:
- [firebase_sub/database/admin_delete_requests.py](firebase_sub/database/admin_delete_requests.py)
- [tests/test_admin_delete_requests.py](tests/test_admin_delete_requests.py)
- [tests/integration/test_admin_delete_requests_integration.py](tests/integration/test_admin_delete_requests_integration.py)
- [firebase_sub/plugins/admin_delete_request.py](firebase_sub/plugins/admin_delete_request.py)
- [firebase_sub/runtime/event_producers.py](firebase_sub/runtime/event_producers.py)
- [firebase_sub/cli/sub_events.py](firebase_sub/cli/sub_events.py)
- [firebase_sub/cli/admin_delete_metrics.py](firebase_sub/cli/admin_delete_metrics.py)
- [README.md](README.md#L112)
