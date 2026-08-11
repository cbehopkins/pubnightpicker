# CDD: Base Store

## 1. Purpose

The Base Store is the application's persistence boundary over the single physical SQLite database shared by the application and Cellar.

The Base Store provides the common persistence infrastructure from which independent components obtain access to their own persistent state.

The Base Store exists primarily to provide:

* one physical database for the process;
* transactional coordination between Cellar and application components;
* a mechanism for components to initialise and access their own schema;
* the database connection used by Cellar;
* the foundation for atomic application work performed as part of Cell completion.

The Base Store does **not** define application semantics.

---

## 2. Logical persistence domains

Although there is one physical SQLite database, the database is divided conceptually into **N independent persistence domains**.

Each domain has a single owner.

Examples include:

```text
Physical SQLite Database
│
├── Cellar domain
│   └── Cellar-owned tables
│
├── Firebase Idempotency domain
│   └── Firebase idempotency tables
│
├── Local Idempotency domain
│   └── Local timer idempotency tables
│
├── Venue Cache domain
│   └── Venue cache tables
│
└── Future component domains
    └── ...
```

The number of domains is not fixed by the Base Store.

New components may introduce additional persistence domains without requiring the Base Store architecture to change.

The important distinction is therefore:

> **One physical database does not imply one logical data model.**

Each component should treat its persistence domain as its own world.

---

## 3. Ownership

The application creates and owns the Base Store.

Cellar is given access to that Base Store during startup rather than independently creating the physical database.

This gives the system one physical transaction boundary while preserving logical ownership boundaries.

The application is responsible for:

* opening the database;
* configuring the SQLite connection;
* constructing the Base Store;
* initialising application components;
* providing the Base Store to Cellar;
* starting the application;
* shutting the database down cleanly.

Cellar is responsible for:

* creating and maintaining its own schema;
* managing Cell lifecycle;
* providing its Cell execution API;
* using the supplied database infrastructure.

Individual application components are responsible for:

* defining their own schema;
* initialising their own schema;
* defining their own persistence semantics;
* exposing an appropriate component-specific access interface.

---

## 4. Component independence

A component must not depend upon the implementation details of another component's persistence domain.

For example:

```text
FirebaseIdempotency
        │
        └── firebase_idempotency tables

VenueCache
        │
        └── venue_cache tables
```

The Venue Cache must not query the Firebase Idempotency tables.

The Firebase Idempotency component must not query Venue Cache tables.

Neither should need to know how the other component stores its data.

This allows components to be developed, tested, replaced, or removed independently.

---

## 5. Base Store responsibilities

The Base Store provides the common physical database infrastructure required by these domains.

Conceptually:

```go
type BaseStore interface {
    DB() *sql.DB
}
```

The final interface may deliberately expose less than `*sql.DB`; the important architectural requirement is that the Base Store provides the database capability required by components and Cellar without embedding component-specific behaviour.

The Base Store must not contain methods such as:

```go
CheckIdempotency(...)
GetVenue(...)
MarkTimerProcessed(...)
```

Those belong to their respective components.

---

## 6. Component access objects

A component should normally construct a component-specific access object from the Base Store.

For example:

```go
type FirebaseIdempotencyStore struct {
    db *sql.DB
}

func NewFirebaseIdempotencyStore(
    base BaseStore,
) (*FirebaseIdempotencyStore, error) {
    // Initialise component schema.
    // Return component-specific persistence interface.
}
```

Likewise:

```go
type VenueCacheStore struct {
    db *sql.DB
}

func NewVenueCacheStore(
    base BaseStore,
) (*VenueCacheStore, error) {
    // Initialise venue cache schema.
    // Return venue-cache-specific persistence interface.
}
```

This creates a clear dependency structure:

```text
                    Base Store
                        │
          ┌─────────────┼─────────────┐
          │             │             │
          ▼             ▼             ▼
   Idempotency      Venue Cache    Future Component
      Store            Store           Store
```

The Base Store provides infrastructure.

The component stores provide meaning.

---

## 7. Component-specific interfaces

The interfaces exposed by components should describe their domain rather than expose generic SQL operations.

For example, an idempotency component might expose operations such as:

```go
type FirebaseIdempotency interface {
    Get(key IdempotencyKey) (State, error)
    ...
}
```

while a Venue Cache might expose:

```go
type VenueCache interface {
    Get(venueID string) (Venue, bool, error)
    Delete(venueID string) error
    ...
}
```

The exact interfaces belong in the CDD for those components.

The Base Store should not attempt to standardise these APIs.

---

## 8. Transactional participation

Cellar provides the application transaction boundary used when application state must change atomically with Cell completion.

The existing contract is:

```go
type ApplicationWork func(tx ApplicationTx) error
```

with:

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

A component that needs atomic participation in Cell completion should provide operations capable of operating against an `ApplicationTx`.

For example:

```go
type FirebaseIdempotencyTx interface {
    MarkPresent(tx ApplicationTx, key string) error
}
```

The component therefore owns the semantics while Cellar owns the transaction lifecycle.

---

## 9. Atomicity across domains

The principal reason for sharing a physical database is that a Cell completion can atomically modify multiple logical domains.

For example:

```text
Cellar domain:
    remove current Cell

Firebase Idempotency domain:
    transition Pushed -> Present

Cellar domain:
    create Handler Fanout Cell

COMMIT
```

These remain conceptually separate operations owned by separate domains.

Physically, however, they may participate in the same SQLite transaction.

This gives the system the desired guarantee:

> Either the complete work unit becomes durable, or none of its constituent changes become durable.

Logical independence therefore does **not** require separate physical databases.

---

## 10. Schema ownership

Every persistence domain owns its own tables.

A component is responsible for:

* defining its schema;
* creating its tables;
* creating its indexes;
* ensuring schema availability during startup;
* performing migrations where required.

A component must not modify another component's schema.

Cellar similarly owns its own tables and schema.

The Base Store provides the physical database but does not own the schemas belonging to individual components.

---

## 11. No fixed component set

The Base Store must not contain assumptions about the set of application components.

In particular, it must not assume that the application contains:

* a particular number of idempotency components;
* a venue cache;
* a poll store;
* a webhook store;
* any other specific application service.

The component set is an application concern.

This permits the architecture to grow from:

```text
Cellar
+
one application component
```

to:

```text
Cellar
+
N independent application components
```

without changing the persistence architecture.

---

## 12. Architectural invariant

The following invariant applies:

> **There is one physical Base Store, containing N logically independent persistence domains, each owned by a component.**

The Base Store provides the common physical transaction boundary.

Components provide domain-specific persistence behaviour.

Cellar provides Cell lifecycle persistence.

No component should need to understand the internal persistence model of another component.

---

## 13. Relationship to Cellar

Cellar should receive access to the application's Base Store during startup.

The application therefore controls the physical database lifecycle:

```text
Open database
      │
      ▼
Create Base Store
      │
      ├── Initialise application components
      │
      └── Give Base Store to Cellar
                    │
                    ▼
              Initialise Cellar
```

This ordering ensures that all persistence infrastructure exists before Listeners are permitted to generate Cells.

---

## 14. Non-goals

The Base Store does not:

* implement idempotency;
* implement caching;
* implement application business logic;
* provide a universal application repository;
* expose Cellar tables to application code;
* define the interfaces of individual components;
* determine which components exist.

It is deliberately a small piece of infrastructure.

Its job is to make the shared physical persistence boundary available while allowing the logical persistence domains above it to remain independent.
