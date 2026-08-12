# CDD: Stale Poll Action Audit Cleanup

## 1. Purpose

The Stale Poll Action Audit Cleanup service removes old records from the poll action audit collection.

The service exists to enforce the configured retention period for historical poll action audit data.

It is a housekeeping operation and does not participate in poll lifecycle processing.

The service is deliberately designed to be safely replayable. It does not require exactly-once execution or persistent execution state.

---

# 2. Scope

### In scope

* Identifying stale poll action audit records.
* Applying the configured retention period.
* Deleting stale records.
* Replaying cleanup after partial execution.
* Running as scheduled housekeeping work.

### Out of scope

* Creating poll action audit records.
* Modifying or interpreting audit records.
* Poll lifecycle behaviour.
* Poll completion.
* Audit record archiving.
* Audit record batching or transactional deletion.

---

# 3. Architecture

The cleanup is implemented as a Cellar housekeeping operation.

Conceptually:

```text
Scheduled Housekeeping
        │
        ▼
Stale Audit Cleanup Cell
        │
        ▼
Query poll_action_audit
        │
        ▼
Records older than retention cutoff
        │
        ▼
Delete each record
```

The Cell is a unit of housekeeping work rather than a representation of an individual audit record.

The Cell does not need to create subordinate Cells for individual deletions.

Deletion of each matching Firestore document is independently replay-safe.

---

# 4. Source Collection

The service operates on:

```text
poll_action_audit
```

Each audit document contains an `at` field representing the time at which the audit event occurred.

The `at` field is the authoritative field for retention purposes.

The cleanup service must not infer age from:

* Firestore document ID;
* poll date;
* audit action type;
* document creation time;
* any other field.

---

# 5. Retention Configuration

The retention period is expressed in days.

The default configuration is:

```text
POLL_ACTION_AUDIT_RETENTION_DAYS
```

with a default value of:

```text
90
```

The effective retention period is supplied to the housekeeping operation.

A negative retention period is invalid and must result in a configuration error.

Zero is valid and means that records strictly older than the current cutoff are eligible for deletion.

---

# 6. Cutoff Calculation

At the beginning of an execution, the service establishes a single reference time:

```text
now
```

using UTC.

The retention cutoff is:

```text
cutoff = now - retention_days
```

The cleanup query selects records satisfying:

```text
at < cutoff
```

The comparison is deliberately **strictly older than**.

Therefore:

```text
at == cutoff
```

is retained.

Conceptually:

```text
older than cutoff     → delete
exactly at cutoff     → keep
newer than cutoff     → keep
```

---

# 7. Time Semantics

Audit retention is based on an absolute elapsed duration rather than a local calendar date.

The service therefore uses UTC for cutoff calculation.

Unlike event recurrence, this service has no London/Cambridge calendar semantics.

The retention rule is simply:

```text
current UTC time - configured number of days
```

---

# 8. Cleanup Cell Behaviour

When the cleanup Cell executes:

1. Determine the current UTC reference time.
2. Calculate the retention cutoff.
3. Query `poll_action_audit` for records where:

   ```text
   at < cutoff
   ```
4. Iterate over the matching records.
5. Delete each matching document.
6. Complete successfully when the cleanup operation has processed the query results.

The Cell does not mutate retained audit records.

It does not rewrite or compact audit records.

---

# 9. Deletion Semantics

Each stale audit document is deleted individually.

The deletion operation is:

```text
document_reference.delete()
```

The cleanup does not require a transaction spanning multiple audit records.

It does not require a batch containing the entire result set.

This is intentional.

The audit records are independent historical records, and deleting one record does not depend upon another.

---

# 10. Idempotency

The cleanup operation is inherently idempotent through its selection predicate.

The service does not maintain a separate list of records that have already been processed.

Instead, the Firestore collection represents the current state of the cleanup work.

For every execution:

```text
find records where at < cutoff
```

A record that has already been successfully deleted no longer exists and therefore cannot be selected by a subsequent execution.

Conceptually:

```text
Execution 1

stale records:
    A
    B
    C

delete A
delete B
process terminates
```

A replay performs:

```text
Execution 2

stale records:
    C

delete C
```

No explicit recovery state is required.

---

# 11. Partial Failure and Replay

The service must tolerate termination during deletion.

For example:

```text
query stale records
        │
        ├── delete A
        ├── delete B
        │
        X process dies
        │
        ▼
      replay
        │
        ├── A absent
        ├── B absent
        └── C still present → delete C
```

This is the desired behaviour.

A partial execution is not itself an error requiring compensation.

The next execution simply reconciles the collection against the retention policy again.

---

# 12. Concurrent Execution

Multiple cleanup Cells may theoretically execute concurrently.

The service must not depend upon exclusive ownership of stale records.

Two executions may both observe the same stale document.

Deleting the same Firestore document more than once must not result in an incorrect application state.

The desired result is simply:

```text
stale document exists
        │
        ├── execution A deletes it
        │
        └── execution B deletes it / observes it already absent
```

The document's desired final state is:

```text
does not exist
```

There is no need for a claim/lock/state transition on the audit document.

---

# 13. Moving Cutoff

The cutoff is calculated from the execution's reference time.

Therefore the cutoff naturally moves forward as time passes.

For example:

```text
Day 1:
    cutoff = Day - 90

Day 2:
    cutoff = Day - 90
```

Records that become stale between executions are naturally picked up by a later run.

The service must not persist the previous cutoff as durable processing state.

---

# 14. Audit Record Integrity

The service deletes stale audit records but does not alter records which remain within the retention period.

In particular, it must not:

* update `at`;
* change the action type;
* rewrite historical fields;
* mark records as deleted;
* add cleanup metadata.

The audit collection remains append-only from the perspective of poll lifecycle services.

The housekeeping service simply removes records which have passed their configured retention period.

---

# 15. Error Handling

Configuration errors must be rejected before attempting cleanup.

A negative retention period is invalid and must raise a configuration error.

Firestore query or deletion failures are not converted into successful completion.

If an individual deletion failure prevents the cleanup Cell from completing, the Cell should fail according to normal Cellar failure/retry semantics.

A retry then re-evaluates the collection and attempts remaining stale records.

The service must not mark a failed cleanup as successful merely because some records were deleted.

---

# 16. No Persistent Cleanup State

The service must not introduce a cleanup state machine such as:

```text
pending
claimed
deleting
deleted
```

for individual audit records.

This would add unnecessary durable state to an operation whose work can already be derived directly from the database.

The predicate:

```text
at < cutoff
```

is sufficient to determine what work remains.

This is a deliberate architectural simplification.

---

# 17. No Exactly-Once Requirement

The service does not require exactly-once execution.

It requires only that repeated execution converges on the desired state:

```text
all records with at < cutoff are absent
all records with at >= cutoff are retained
```

This is the correctness contract.

The housekeeping scheduler and Cellar runtime may therefore retry or overlap executions without requiring special coordination.

---

# 18. Safety Properties

The implementation must preserve the following invariants.

### Invariant 1 — Retention boundary

Only records satisfying:

```text
at < cutoff
```

may be deleted.

### Invariant 2 — Boundary equality

A record whose `at` value is exactly equal to the cutoff must not be deleted.

### Invariant 3 — No premature deletion

Records newer than the cutoff must remain untouched.

### Invariant 4 — Replay safety

Repeating cleanup after partial execution must not produce an incorrect result.

### Invariant 5 — No duplicate work state

The service must not require per-record claim or execution state.

### Invariant 6 — Eventual cleanup

A stale record which remains present because an execution failed must remain eligible for deletion on a subsequent execution.

### Invariant 7 — Historical integrity

Records retained by the policy must not be modified by the cleanup service.

---

# 19. Conceptual Flow

```text
                  Scheduled Housekeeping
                           │
                           ▼
                 Stale Audit Cleanup Cell
                           │
                           ▼
                    determine now (UTC)
                           │
                           ▼
                  cutoff = now - retention
                           │
                           ▼
              query poll_action_audit
                    where at < cutoff
                           │
                           ▼
                  ┌─────────────────┐
                  │ stale records   │
                  └────────┬────────┘
                           │
                     delete each
                           │
                           ▼
                         done
```

On replay:

```text
previously deleted records
        │
        └── no longer match because
            they no longer exist

remaining stale records
        │
        ▼
     deleted
```

---

# 20. Architectural Principle

The fundamental design principle is:

> **When the remaining housekeeping work can be derived directly from durable state, do not create additional durable state to track the work.**

For stale audit cleanup, the database itself tells us what remains to be done:

```text
record exists
AND
record.at < retention cutoff
```

Therefore:

```text
eligible record exists → delete it
eligible record absent  → nothing to do
```

This gives the service simple, deterministic and replay-safe behaviour without claims, locks, job records or per-record state machines.
