# ADR-0006: Application-Owned Startup and Component Initialisation

## Status

Accepted

## Context

The backend consists of several architectural layers that must be initialised in a controlled order.

The application uses a single physical SQLite database shared by:

* Cellar, which owns its own persistence domain;
* application components, each of which owns its own logical persistence domain.

The shared physical database is required because Cellar must be able to atomically commit Cell lifecycle changes together with application state changes.

Examples include:

* creating a replacement Cell together with an application state update;
* transitioning an idempotency record together with creation of a Handler Cell;
* deleting a Cell together with application-side work.

The application therefore needs to own the lifetime of the physical database and provide that database infrastructure to Cellar.

The backend also contains Listeners that observe external systems and create Cells. These Listeners must not begin producing work until the persistence and Cell execution infrastructure is ready.

The startup process therefore establishes a dependency chain:

```text
Physical Database
       │
       ▼
   Base Store
       │
       ├───────────────┐
       ▼               ▼
Application         Cellar
Components          Store
       │               │
       └───────┬───────┘
               ▼
        Cell Handlers
               │
               ▼
           Cellar Runtime
               │
               ▼
           Listeners
```

## Decision

The application owns the startup lifecycle of the backend.

The application will:

1. Open and configure the physical SQLite database.
2. Construct the Base Store.
3. Initialise all application persistence components.
4. Provide the Base Store to Cellar.
5. Initialise Cellar's persistence domain and runtime.
6. Register all Cell Handlers.
7. Start Cell execution.
8. Start Listeners and other external work producers.
9. Start any remaining background services that depend upon the preceding infrastructure.

### 1. Database creation

The application creates and opens the physical database.

Cellar does not independently create or own the physical database.

There is therefore one database connection/persistence boundary for the process.

### 2. Base Store construction

The application constructs the Base Store over the opened database.

The Base Store provides the common persistence infrastructure from which independent application components and Cellar obtain access.

The Base Store itself does not own application semantics.

### 3. Application component initialisation

Each application component is initialised against the Base Store.

A component is responsible for its own logical persistence domain, including:

* schema creation;
* indexes;
* component-specific persistence access objects;
* component-specific configuration.

Components remain logically independent even though they share the physical database.

For example:

```text
Base Store
    │
    ├── Firebase Idempotency Store
    ├── Local Idempotency Store
    ├── Venue Cache Store
    └── Future Component Stores
```

The existence of one component must not require another component to exist.

### 4. Cellar initialisation

Once the Base Store and required application components exist, the application provides the Base Store to Cellar.

Cellar then initialises its own persistence domain.

Cellar remains responsible for:

* Cell persistence;
* Cell lifecycle;
* Cell claiming;
* Cell completion;
* retry/recovery;
* Cell Handler execution.

Cellar does not own the physical database lifecycle.

### 5. Handler registration

All required Cell Handlers are registered before external work producers are started.

A Listener must never be able to create a Cell referencing a Handler that has not yet been registered.

Handler registration therefore precedes Listener activation.

### 6. Cell execution

The Cellar runtime is started before Listeners are activated.

This ensures that Cells created by a Listener can immediately enter the normal Cell lifecycle.

The system must not expose external Listener inputs while the Cell execution machinery is unavailable.

### 7. Listener activation

Listeners are started only after:

* the physical database is available;
* the Base Store is available;
* required application components are initialised;
* Cellar is initialised;
* required Cell Handlers are registered;
* Cell execution is operational.

Listeners may then begin observing external systems and creating Cells.

This applies to all Listener types, including:

* database Listeners;
* timer Listeners;
* webhook Listeners;
* future Listener types.

The Listener abstraction is architectural rather than requiring a single common Go interface.

### 8. Background services

Other background services may be started after their dependencies are ready.

Examples include:

* cache population;
* housekeeping;
* maintenance tasks;
* periodic reconciliation.

Such services must not be started before the components they depend upon have been initialised.

---

## Startup invariant

The startup process establishes the following invariant:

> **Once a Listener is capable of creating a Cell, the Base Store, required application persistence components, Cellar, and all referenced Cell Handlers are operational.**

This is the primary correctness requirement of startup ordering.

---

## Failure during startup

Startup is considered unsuccessful if a required component cannot be initialised.

Examples include:

* database cannot be opened;
* Base Store cannot be constructed;
* application schema cannot be initialised;
* Cellar schema cannot be initialised;
* required Handler registration fails;
* Cellar cannot start.

In these cases the application must not start external Listeners.

The preferred behaviour is for the process to fail startup rather than enter a partially operational state.

---

## Shutdown

Shutdown should occur in the reverse dependency order where practical.

The application should first prevent new external work from entering the system by stopping Listeners.

It should then allow or manage outstanding Cell execution according to the Cellar shutdown contract.

Finally, application components and the physical database may be closed.

Conceptually:

```text
Stop Listeners
      │
      ▼
Stop Cell execution
      │
      ▼
Stop background services
      │
      ▼
Close application components
      │
      ▼
Close Base Store / database
```

The precise Cellar shutdown semantics are defined by the Cellar specification.

---

## Consequences

### Positive

* The application owns the physical database lifecycle.
* Cellar and application components share a single transaction boundary.
* Logical persistence domains remain independent.
* Listeners cannot generate Cells before the machinery required to process them exists.
* Startup failures are detected before external work is accepted.
* Adding a new application component does not require architectural changes to Cellar.
* The ordering gives a clear dependency structure for future services.

### Negative

* Startup becomes an explicit orchestration responsibility of the application.
* Components must expose initialisation behaviour.
* The application must understand the dependency ordering between infrastructure components.
* A failure in any mandatory component prevents the entire backend from starting.

These costs are intentional: a partially operational backend is considered more dangerous than a backend that fails clearly during startup.

---

## Rejected alternatives

### Cellar owns database creation

Rejected.

Cellar is an infrastructure component of the backend, not the owner of the application's physical persistence lifecycle.

Application ownership also makes the shared transaction boundary explicit.

### Each component owns its own SQLite database

Rejected.

Separate physical databases would prevent Cellar from atomically committing Cell lifecycle changes with application state changes.

### Start Listeners immediately and allow them to queue work during startup

Rejected.

This introduces unnecessary startup race conditions:

* Cells may reference unregistered Handlers;
* application stores may not yet exist;
* Cellar may not yet be able to execute work;
* webhook/database events may arrive while the system is only partially initialised.

The system instead establishes a fully operational processing pipeline before accepting external work.

---

## Relationship to other architecture documents

This ADR depends upon and complements:

* **CDD: Base Store** — defines the shared physical persistence boundary and logical component domains.
* **Cellar CDDs** — define Cell lifecycle, Store, Handler, and transactional behaviour.
* **Listener CDDs** — define the behaviour of individual Listener types.
* **Idempotency CDDs** — define individual idempotency component behaviour.

This ADR intentionally does not define the internal implementation of those components.

It defines only how they are assembled into a running backend.
