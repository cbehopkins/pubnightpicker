# ADR: Application Structure and Package Responsibilities

## Status

Accepted

## Context

Last Orders is a Firebase-backed application which responds to changes and external events by producing Truths and executing work through Cellar.

The application contains several distinct kinds of responsibility:

* application construction and composition;
* reusable infrastructure components;
* Firebase-specific database access, caches, and listeners;
* units of work performed by the application;
* connectivity between Truths and those units of work;
* externally accessible endpoints.

Without explicit boundaries, these concerns would tend to become mixed together. In particular, there is a risk that:

* Firebase implementation details leak into application behaviour;
* services become responsible for deciding when they should execute;
* application composition accumulates business logic;
* endpoint registration and service activation become implicit;
* reusable infrastructure becomes mixed with application-specific behaviour.

The project should therefore be structured so that the top-level packages broadly describe the major responsibilities of the application and make the architecture visible from the directory structure.

## Decision

The application will be organised into distinct packages representing the following responsibilities.

### Application

The application layer is the composition root.

It is responsible for:

* reading and applying configuration;
* constructing application components;
* connecting components together;
* registering active plugins and endpoints;
* starting long-running infrastructure;
* coordinating application shutdown.

It should contain minimal application behaviour and should primarily be assembly code.

The intended characteristic of this package is that it should be deliberately dull.

The active composition of the application should be visible here.

Conceptually:

```text
App
 ├── construct infrastructure
 ├── construct services
 ├── register plugins
 ├── register endpoints
 ├── start long-running producers
 └── coordinate shutdown
```

The application layer should not contain Firebase-specific processing logic or service-specific implementation behaviour.

---

### Components

The `components` area contains reusable, self-contained infrastructure components.

Examples include components such as:

* `firebaseidempotency`;
* reusable execution or coordination infrastructure where appropriate.

A component should have a specific responsibility and should be reusable independently of a particular Last Orders application behaviour.

The package must not become a general-purpose collection of helpers or miscellaneous utilities.

Application-specific code should not be moved into `components` merely because it is used from more than one location.

---

### Database

The `database` area contains Firebase-specific infrastructure which abstracts or reacts to the database.

This includes two primary categories.

#### Caches

Caches provide local representations of database state.

For example, a venue cache may:

```text
Firebase
   ↓
Initial load
   ↓
Local cache
   ↓
Subscription to changes
   ↓
Updated local representation
```

Consumers interact with the cache rather than needing to understand the Firebase implementation required to maintain it.

#### Listeners

Listeners observe database changes and convert those changes into application Truths.

Conceptually:

```text
Firebase modification
        ↓
Database Listener
        ↓
Truth
```

Listeners are responsible for understanding the Firebase representation and translating relevant changes into application-level events.

They should not be responsible for deciding which application work should subsequently be performed.

Caches and listeners are grouped together because both represent Firebase-specific database infrastructure rather than application behaviour.

The application meaning of a Truth must not be defined in terms of Firebase paths, documents, or implementation details.

---

### Services

Services represent units of work performed by the application.

A service is implemented using Cellar and will normally be represented by a Cellar Cell, commonly a Sequence Cell containing the handlers required to perform the unit of work.

For example:

```text
Send Email Service

Sequence Cell:

    Prepare message
          ↓
    Send message
          ↓
    Verify / locate result
          ↓
    Record outcome
```

Cellar provides the execution semantics for services, including:

* execution;
* Fanout;
* Cell Sequences;
* retry behaviour;
* delayed execution where appropriate.

Services are responsible for describing and performing a unit of work.

They should encapsulate service-specific implementation concerns.

For example, an email service may encapsulate interaction with Sweego, while an alternative implementation might encapsulate interaction with a different provider.

A service should not generally be responsible for deciding which Truth causes it to execute.

That connectivity belongs to the plugin layer.

---

### Plugins

Plugins describe the connectivity between Truths and the Cellar Cells which perform work.

Conceptually:

```text
Truth
   ↓
Plugin connectivity
   ↓
Cellar Fanout
   ↓
Service Cells
```

A plugin answers the question:

> When this Truth occurs, which units of work should be triggered?

Plugins do not replace Cellar's execution or Fanout mechanisms.

They define the application's wiring between:

* sources of Truths;
* Cellar Fanouts;
* service Cells.

This separates the concerns of:

```text
Database / Endpoint:
    What happened?

Plugin:
    What should happen because of it?

Service Cell:
    How is that unit of work performed?
```

Plugin activation and registration should be explicit.

The application composition layer should visibly register the active plugins.

Go `init()` functions must not be used as the primary mechanism for implicitly activating plugins.

Importing a package should not silently change the active behaviour of the application.

---

### Endpoints

The `endpoints` area contains externally accessible entry points into the application.

This initially includes HTTP endpoints such as the Sweego webhook receiver.

An endpoint is responsible for:

* receiving an external request or event;
* validating and translating it into the application's representation;
* passing the resulting work into the appropriate application flow.

Conceptually:

```text
External request
        ↓
Endpoint
        ↓
Truth / application input
        ↓
Cellar Fanout / processing
```

Endpoints should not contain the implementation of the downstream service behaviour.

Endpoint registration should be explicit in the application composition layer.

Go `init()` functions must not be used to implicitly register active endpoints.

---

## Intended Dependency and Behaviour Flow

The application is intended to follow the conceptual flow:

```text
Database Listener ──┐
                    │
HTTP Endpoint ──────┼──→ Truth
                    │       │
Other Inputs ───────┘       ↓
                         Plugin
                            ↓
                      Cellar Fanout
                            ↓
                       Service Cells
                            ↓
                    Sequence Handlers
                            ↓
                     External Systems
```

This represents a separation of responsibilities:

### Inputs

Determine and communicate:

> Something happened.

### Truths

Represent:

> What happened.

### Plugins

Determine:

> What work should happen because of it?

### Service Cells

Perform:

> The unit of work.

### Sequence Handlers

Perform:

> The individual steps required to complete that unit of work.

---

## Package Structure

The intended high-level project structure is approximately:

```text
cmd/
    last-orders/
        main.go

internal/
    app/
        ...

    components/
        firebaseidempotency/
        ...

    database/
        caches/
            ...
        listeners/
            ...

    services/
        email/
            ...
        notification/
            ...
        recurrence/
            ...

    plugins/
        email/
            ...
        notification/
            ...
        recurrence/
            ...

    endpoints/
        sweego/
            ...
```

The precise internal structure of each area may evolve as implementation requirements become clearer.

The purpose of the top-level structure is to make the major architectural responsibilities visible rather than to impose an unnecessarily rigid hierarchy.

---

## Lifecycle

The `app` layer owns application lifecycle coordination.

Long-running components are primarily infrastructure which produces or accepts work, such as:

* database listeners;
* active caches;
* HTTP servers and endpoints;
* any required Cellar execution infrastructure.

Service Cells represent units of work and are not automatically treated as permanently running application services.

Startup should generally ensure that consumers and execution infrastructure are ready before producers begin introducing work.

Shutdown should generally:

1. stop producing or accepting new work;
2. allow in-flight work to drain where appropriate;
3. stop execution infrastructure;
4. close underlying resources.

A generic lifecycle framework will not initially be introduced unless repeated lifecycle requirements demonstrate the need for one.

---

## Consequences

### Positive

* The major responsibilities of the application are visible from the package structure.
* Application composition is explicit and easy to inspect.
* Firebase implementation details are isolated within database infrastructure.
* Truths are separated from decisions about what work should occur.
* Plugins explicitly describe connectivity between Truths and Cellar work.
* Services encapsulate units of work using Cellar's native execution model.
* Endpoints remain separate from downstream application behaviour.
* Plugin and endpoint activation is explicit rather than relying on import side effects.
* The architecture uses Cellar's native Fanout and Cell Sequence mechanisms rather than duplicating them.

### Negative

* The distinction between some areas may require judgement as the application evolves.
* `components` requires discipline to avoid becoming a miscellaneous utility package.
* A service and its corresponding plugin are intentionally separate, which may initially feel more verbose.
* Application composition must explicitly register and connect the active components.
* The precise APIs used to connect Truths, Fanouts, and Cells may evolve as Cellar integration matures.

## Result

Last Orders is structured around explicit responsibility boundaries.

The application composition layer assembles the system.

Database infrastructure converts Firebase state and changes into usable application state and Truths.

Endpoints introduce external events into the application.

Plugins describe what work should be triggered by Truths.

Services represent units of work implemented using Cellar Cells and Sequences.

Cellar provides the execution model, including Fanout, sequencing, retries, and execution state.

The resulting architecture is intended to make both the project's directory structure and its runtime behaviour understandable by following the flow:

```text
Input
  ↓
Truth
  ↓
Plugin connectivity
  ↓
Cellar Fanout
  ↓
Service Cell
  ↓
Sequence Handlers
  ↓
External effect
```
