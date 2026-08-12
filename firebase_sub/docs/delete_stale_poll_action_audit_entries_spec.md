# delete_stale_poll_action_audit_entries Specification

## 1. Purpose

Remove aged poll action audit records from Firestore based on a retention window.

## 2. Function contract

Function:
- delete_stale_poll_action_audit_entries(db, now=None, retention_days=POLL_ACTION_AUDIT_RETENTION_DAYS)

Defaults:
- POLL_ACTION_AUDIT_RETENTION_DAYS comes from env var POLL_ACTION_AUDIT_RETENTION_DAYS
- default value is 90 when env var is not set

References:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L56)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L178)

## 3. Collection and field names used

Collection queried:
- poll_action_audit

Date field used for age comparison:
- at

Query predicate:
- at < cutoff_time

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L49)
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L179)
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L187)

## 4. Exact cutoff calculation

Cutoff formula:
- cutoff_time = (now if provided else datetime.now(UTC)) - timedelta(days=retention_days)

Interpretation:
- records strictly older than cutoff are deleted
- records exactly at cutoff are not deleted because comparison is strict less-than

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L187)

## 5. Validation and failure behaviour

Input validation:
- if retention_days < 0, raise ValueError with message containing retention_days

Deletion behaviour:
- iterate all matching documents from query stream
- delete each by audit_doc.reference.delete()

Error handling:
- no internal try/except in this function
- Firestore query/delete exceptions propagate to caller

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L185)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L193)

## 6. Scheduling context

This function is registered as a housekeeping task and runs when the housekeeping runner executes periodic maintenance.

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L607)

## 7. Test-verified behaviour

Verified by unit tests:
- deletes all returned stale documents
- performs no deletes when query returns empty
- rejects negative retention_days

Reference:
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py#L308)
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py#L327)
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py#L342)
