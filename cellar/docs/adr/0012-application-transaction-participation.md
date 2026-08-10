# ADR-0012: Application Transaction Participation

## Status

Draft

## Context

Cellar executes persisted Cells.

A Handler may perform work that changes application state in addition to changing Cellar state.

For example, a Handler may:

1. perform an external operation;
2. record the outcome in the application's database;
3. create a new Cell representing follow-up work;
4. complete the current Cell.

Without transactional coordination, these operations can leave the application in an ambiguous state.

For example:

```text
Application database:
    email_sent = true

Cellar:
    current Cell still exists
    follow-up Cell does not exist
```

The Handler may then execute again after recovery.

Cellar can provide atomicity for its own persisted state, but the application should also be able to make application database changes atomically with Cell completion when both are persisted by the same database.

The capability must not turn Cellar into an application database abstraction or require Cellar to understand application semantics.

---

## Decision

A Handler may return application persistence work as part of its execution Result.

Cellar commits this application work atomically with completion of the current Cell.

Conceptually:

```text
BEGIN

    application persistence work

    delete completed Cell

    create any resulting Cells

COMMIT
```

The `COMMIT` remains the responsibility of Cellar.

The Handler does not begin, commit, or roll back the transaction.

---

## Application Transaction Boundary

The transaction contains two categories of state:

### Cellar-owned state

Cellar controls:

* Cell creation;
* Cell completion;
* Cell retry;
* other Cell lifecycle state.

### Application-owned state

The application may request changes to its own persistent state.

The application transaction must not modify Cellar-owned tables.

---

## Cellar-Owned Namespace

Cellar-owned database objects use a reserved namespace.

V0 reserves:

```text
_cellar_*
```

for Cellar-owned tables and related database objects.

Application transaction work must not modify objects in this namespace.

V0 does not enforce this restriction programmatically.

The restriction is an architectural contract between Cellar and its application.

Future implementations may introduce stronger enforcement.

---

## Database API

Cellar should expose application transaction access as close as practical to the native transaction API of the underlying database.

Cellar should not introduce an application-specific ORM or business-oriented database abstraction.

For example, a SQLite implementation may expose an interface closely corresponding to the native Go database transaction API.

The exact Go API is defined by the Store CDD and its implementation.

---

## Transaction Lifetime

Application transaction access is valid only during execution of the completion operation.

Application code must not retain the transaction or transaction-backed objects after the Handler Result has been applied.

Cellar owns the transaction lifetime.

---

## External Systems

Application transaction participation does not provide atomicity with external systems.

For example:

```text
Application database
        |
        | atomic
        v
Cellar Store
```

but:

```text
Mail service
        |
        | NOT atomic
        v
Cellar Store
```

Handlers remain responsible for designing external side effects to tolerate duplicate execution.

Cellar does not provide distributed transactions.

---

## Failure Semantics

If the Store cannot commit the combined operation:

```text
application changes
+
Cellar completion
+
new Cells
```

the operation is considered unsuccessful.

The Runtime treats Store failure as a fatal infrastructure failure in V0 and shuts down Cellar.

Normal recovery semantics apply when Cellar is restarted.

---

## Rationale

This provides a strong persistence boundary without requiring Cellar to understand application semantics.

A Handler can communicate:

> "These application changes, this completion, and these new Cells are one logical persistence operation."

Cellar can then guarantee that the database either commits all of those changes or none of them.

The design deliberately stops at the database boundary.

External side effects remain the responsibility of the application.

---

## Non-goals

This ADR does not define:

* distributed transactions;
* two-phase commit;
* external API coordination;
* application retry policy;
* application database schema;
* enforcement of application access to Cellar tables.

Those concerns remain outside Cellar's responsibility.

---

## Future Considerations

Future Store implementations may provide stronger isolation between application and Cellar state.

Future versions may also provide mechanisms to enforce the reserved Cellar namespace.

Neither is required for V0.
