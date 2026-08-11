# Backend Top-Level Architecture Contract

## 1. Purpose

The backend is an application composed from:

* Cellar;
* application state;
* Cell Handlers;
* listeners;
* scheduled activities;
* HTTP/webhook handlers;
* application services.

The top-level backend is responsible for composing these pieces and starting and stopping their runtime activities.

It should provide orchestration, not application behaviour.

---

## 2. Base DB and Cellar integration

The backend owns the Base DB.

The Base DB is a single physical SQLite database containing two logically separate domains:

* **Cellar DB** — state owned by Cellar;
* **Application DB** — state owned by the application.

The backend creates/opens the Base DB and initialises its application schema before constructing Cellar against that database.

Conceptually:

```text
Backend
   │
   ├── opens Base DB
   │
   ├── initialises Application DB
   │
   └── constructs Cellar using Base DB
                         │
                         └── initialises Cellar DB
```

The logical ownership boundary remains strict even though both domains share the same physical SQLite database.

The application must not directly manipulate Cellar tables.

Cellar must not directly manipulate application tables except through its explicit application transaction mechanism.

---

## 3. Cellar integration contract

The backend supplies Cellar with the application's Base DB.

The exact Cellar constructor is defined by the Cellar specification, but the architectural relationship is:

```go
db := openBaseDB(...)

initialiseApplicationSchema(db)

store, err := cellar.NewStore(db, allocator)
```

The important property is that **the application owns the database**.

Cellar does not own creation of the database file or database connection.

---

## 4. Application transaction contract

Cellar exposes application work through:

```go
type ApplicationWork func(tx ApplicationTx) error
```

The transaction supplied to application work is:

```go
type ApplicationTx interface {
    Exec(query string, args ...any) error
    ExecContext(ctx context.Context, query string, args ...any) error

    Query(query string, args ...any) (ApplicationRows, error)
    QueryContext(ctx context.Context, query string, args ...any) (ApplicationRows, error)

    QueryRow(query string, args ...any) ApplicationRow
    QueryRowContext(ctx context.Context, query string, args ...any) ApplicationRow
}
```

The supporting row interfaces are:

```go
type ApplicationRow interface {
    Scan(dest ...any) error
}

type ApplicationRows interface {
    Close() error
    Next() bool
    Scan(dest ...any) error
    Err() error
}
```

These interfaces deliberately do not expose `*sql.Tx`.

Cellar owns the transaction lifecycle.

An `ApplicationWork` function may perform application database operations, but must not commit or roll back the transaction.

---

## 5. Atomic Cell completion

Cell completion may combine:

* application database changes;
* creation of replacement Cells;
* deletion of the completed Cell.

These operations occur within one SQLite transaction.

Conceptually:

```text
BEGIN

    ApplicationWork
    create replacement Cells
    delete completed Cell

COMMIT
```

If any operation fails, the complete transaction is rolled back.

This is a fundamental backend capability.

It allows application logic to express operations such as:

```text
update application state
+
create next Cell
+
delete current Cell
```

as one atomic operation.

---

# 6. Cell Handlers

A Cell Handler is a Cellar concept.

A Handler receives a Cell and performs the application work associated with that Cell.

The Handler interface itself is defined by the Cellar specification and is not duplicated here.

The backend registers application-specific handlers with Cellar.

A typical Handler will interact with the application through:

* reads from the Application DB;
* application services;
* `ApplicationWork` supplied as part of the Cell result.

The Handler should not directly manage Cellar lifecycle state.

---

# 7. Listeners

A Listener is an architectural concept rather than a universal Go interface.

The backend does not require:

```go
type Listener interface {
    ...
}
```

Instead, a Listener is any runtime component which observes an external source and causes one or more Cells to be created.

Listeners may observe:

* database/document changes;
* wall-clock time;
* inbound HTTP/webhook requests;
* future sources not yet identified.

The observation mechanism is specific to the Listener.

This avoids forcing fundamentally different mechanisms into an artificial common interface.

---

# 8. Database Listeners

A database Listener observes changes in an external database or document store.

For example:

```text
Firebase
   │
   ▼
Database Listener
   │
   ▼
observed Fact
   │
   ▼
Cell
```

The Listener is responsible for detecting the observation and durably creating a Cell representing it.

The Listener must not perform irreversible idempotency acknowledgement before the Cell has been durably created.

For Firebase listeners, idempotency acknowledgement is therefore performed by subsequent Cell processing rather than directly by the Listener.

The exact idempotency mechanism is defined by the Firebase idempotency architecture.

---

# 9. Timer Listeners

A Timer Listener observes the passage of wall-clock time and creates Cells when appropriate.

There are two broad classes of timer-driven behaviour:

### Housekeeping

For housekeeping operations, advancement of wall-clock time is itself sufficient to determine that work is due.

For V0, missed housekeeping intervals may simply be treated as missed.

### Timed application work

Some timed operations may need to execute even if their nominal time has passed.

For example:

```text
Poll auto-complete deadline passed
        │
        ▼
attempt automatic completion
```

V0 may deliberately avoid durable timer-event idempotency.

The architecture should nevertheless permit a future local idempotency mechanism in which a timer occurrence is represented by an idempotency key and filtered through the Application DB.

This is expected to be a V1/V2 refinement rather than a V0 requirement.

---

# 10. Webhook Listeners

A Webhook Listener receives an inbound HTTP request.

The HTTP connection remains entirely within the Listener.

The Listener is responsible for:

1. receiving the request;
2. validating the request;
3. decoding the webhook payload;
4. creating a durable Cell representing the resulting Fact;
5. returning the HTTP response.

The Cell does not own or retain the HTTP connection.

The response must not depend upon the Cell being executed successfully.

Conceptually:

```text
HTTP request
     │
     ▼
Webhook Listener
     │
     ├── validate/decode
     │
     ├── create Cell ─────► Cellar
     │
     └── HTTP response
```

The Cell is only required to be durably created before the response is sent.

If the process fails after Cell creation but before the response is returned, the external sender may retry the webhook.

The retry may create another Cell.

This is acceptable because the resulting Cells are filtered by the appropriate local idempotency mechanism before application work is performed.

---

# 11. Facts

The term **Fact** is preferred to "Event".

A Fact represents something observed by a Listener which may cause application work.

Examples include:

* a Firebase document being created;
* a Firebase document being modified;
* a webhook being received;
* a timer becoming due.

A Fact does not itself perform work.

The normal conceptual flow is:

```text
Source
  │
  ▼
Listener
  │
  ▼
Fact
  │
  ▼
Cell
  │
  ▼
Cell Handler
```

A Fact does not necessarily need to be represented as a persistent Go object or database record. It is primarily an architectural term describing the observation being turned into work.

---

# 12. Cells as the dispatch boundary

Cells are the boundary between observation and application work.

Listeners do not directly invoke application Handlers.

Instead, a Listener creates a Cell whose Handler performs the next stage of processing.

A Cell may then create further Cells as part of its completion.

For example:

```text
New Poll Listener
       │
       ▼
  Observed Cell
       │
       ▼
 Idempotency Cell
       │
       ▼
 Dispatch Cell
       │
       ├────► Push Notification Cell
       ├────► Email Cell
       └────► Mailing List Cell
```

This keeps the top-level backend free from application-specific dispatch plumbing.

Cell creation is the mechanism by which work enters Cellar.

---

# 13. Idempotency

Idempotency is generally implemented as a filtering stage within the Cell workflow.

It is not required to be a top-level backend component.

Different sources may require different authoritative idempotency stores.

Examples:

* Firebase listeners use Firebase-backed idempotency state;
* timer-driven work may eventually use Application DB state;
* webhook processing uses local Application DB state.

The common architectural pattern is:

```text
Fact
 │
 ▼
Cell
 │
 ▼
Idempotency filtering
 │
 ├── already processed ──► finish
 │
 └── new ────────────────► continue
```

The idempotency operation itself must be designed so that crashes and retries cannot cause permanent loss of the observed Fact.

---

# 14. Application Services

Application services contain reusable business behaviour which may be invoked from multiple entry points.

For example, venue-selection logic may eventually be used by:

* a frontend API request;
* a backend timer;
* another application service.

The business logic should exist in one application service rather than being independently implemented by each caller.

Conceptually:

```text
                    Application Service
                           ▲
              ┌────────────┼────────────┐
              │            │            │
          Cell Handler   HTTP API    Timer
```

An API endpoint is therefore an entry point into application behaviour, not a separate implementation of that behaviour.

This architecture deliberately leaves room for future APIs which allow the frontend to call backend application services directly.

---

# 15. Top-level backend responsibilities

The top-level backend is responsible for composition and lifecycle.

It should:

* open/create the Base DB;
* initialise the Application DB schema;
* construct Cellar;
* register Cell Handlers;
* construct application services;
* construct/start database Listeners;
* construct/start Timer Listeners;
* construct/start Webhook Listeners;
* start the HTTP server;
* manage graceful shutdown.

It should not contain application business logic.

Nor should it contain large amounts of dispatch plumbing.

Adding a new service should primarily involve:

1. implementing the relevant application service and/or Cell Handler;
2. implementing the appropriate Listener if required;
3. registering/composing the components at startup.

---

# 16. Deliberate absence of universal interfaces

The backend intentionally does **not** attempt to define Go interfaces for every architectural concept.

The following are architectural concepts rather than necessarily Go interfaces:

* Listener;
* Fact;
* Application Service;
* Application DB;
* Cell dispatch.

Go interfaces are introduced where there is a genuine substitution or ownership boundary.

Currently established examples include:

```go
type ApplicationWork func(tx ApplicationTx) error

type ApplicationTx interface {
    Exec(query string, args ...any) error
    ExecContext(ctx context.Context, query string, args ...any) error

    Query(query string, args ...any) (ApplicationRows, error)
    QueryContext(ctx context.Context, query string, args ...any) (ApplicationRows, error)

    QueryRow(query string, args ...any) ApplicationRow
    QueryRowContext(ctx context.Context, query string, args ...any) ApplicationRow
}

type ApplicationRow interface {
    Scan(dest ...any) error
}

type ApplicationRows interface {
    Close() error
    Next() bool
    Scan(dest ...any) error
    Err() error
}
```

The Cell Handler contract and Cellar Store contracts are defined by Cellar itself.

Additional interfaces should only be introduced when a concrete implementation boundary or testing/substitution requirement justifies them.
