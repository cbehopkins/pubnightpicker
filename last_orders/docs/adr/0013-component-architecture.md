# ADR: Component Architecture and Deployment

**Status:** Proposed  
**Decision scope:** Last Orders backend  
**Components:** Landlord, Handpump

## Context

Last Orders currently runs as a Go application distributed in a single Docker image. Landlord is responsible for existing background and operational functionality, including notifications, email and housekeeping.

Handpump is being introduced as the API layer that gradually takes over data-access responsibilities from the frontend. These responsibilities include serving venue data, managing Meet updates and voting, and enforcing backend authentication and authorisation.

The architecture needs to support independent development and testing of these components without imposing unnecessary deployment complexity on a small service.

The components may eventually need to scale independently or be divided into more specialised services. That possibility should not force the initial implementation to adopt a distributed deployment.

## Decision

### 1. Logical component boundaries

Landlord and Handpump are distinct logical components with separate responsibilities.

**Landlord** owns existing background and operational responsibilities, including notifications, email, housekeeping and other established services.

**Handpump** owns the HTTP API, request authentication, authorisation decisions, data-access operations, local read caches and client update streams.

Components should interact through explicit interfaces rather than depend on each other's internal implementation.

### 2. Development and execution

Each component must be independently runnable during development, debugging and testing.

The application should provide a production entry point that composes the required components into a single running application.

The precise command-line interface is an implementation decision. One possible arrangement is:

- Run Landlord independently.
- Run Handpump independently.
- Run the production composition containing both.

The production composition must manage startup, shutdown, component failures and health reporting deliberately.

### 3. Initial deployment

Initially, Last Orders will be distributed as a single Docker image with a single production entry point running the required components in one deployment.

The initial HandPump production deployment will run as a single backend
instance. Component boundaries and instance-local state should not prevent
future multi-instance operation, but this ADR does not claim that multi-instance
deployment is supported or guaranteed initially.

This is a deliberate operational simplification. It does not mean that Landlord and Handpump are considered the same logical component.

The initial deployment does not require separate Docker images, independent scaling or separate production processes for each component.

### 4. Future evolution

The architecture should not preclude running Landlord and Handpump separately in the future.

Potential future changes include:

- Running multiple Handpump instances.
- Deploying Handpump instances configured for particular responsibilities.
- Separating Landlord's email, push-notification or housekeeping functionality into dedicated components.
- Running scheduled housekeeping independently of the continuously running API.

These are deferred possibilities, not current requirements.

## Consequences

### Positive

- Development and debugging can target individual components.
- Production distribution remains straightforward.
- Component responsibilities remain clear.
- Future deployment separation remains possible without requiring the logical architecture to be redesigned.

### Negative

- A process-level failure initially affects all components running within that process.
- Components cannot be independently deployed or scaled in the initial production arrangement.
- The production composition requires explicit lifecycle and failure management.

These limitations are acceptable for the current scale of Last Orders.

## Deferred decisions

- Whether components should eventually run in separate operating-system processes.
- Whether separate Docker images are justified.
- Whether individual Handpump responsibilities should be deployed independently.
- Whether background jobs should become dedicated services.
- Whether a dedicated messaging or event-distribution system is required as the application grows.
