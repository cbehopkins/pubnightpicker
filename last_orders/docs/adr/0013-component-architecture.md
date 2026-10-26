# ADR: Component Architecture and Deployment

**Status:** Accepted
**Decision scope:** Last Orders backend  
**Components:** Landlord, HandPump

## Context

Last Orders currently runs as a Go application distributed in a single Docker image. Landlord is responsible for existing background and operational functionality, including notifications, email and housekeeping.

HandPump is being introduced as the API layer that gradually takes over data-access responsibilities from the frontend. These responsibilities include serving venue data, managing Meet updates and voting, and enforcing backend authentication and authorisation.

The architecture needs to support independent development and testing of these components without imposing unnecessary deployment complexity on a small service.

The components may eventually need to scale independently or be divided into more specialised services. That possibility should not force the initial implementation to adopt a distributed deployment.

## Decision

### 1. Logical component boundaries

Landlord and HandPump are distinct logical components with separate responsibilities.

**Landlord** owns existing background and operational responsibilities, including notifications, email, housekeeping and other established services.

**HandPump** owns the HTTP API, request authentication, authorisation decisions, data-access operations, local read caches and client update streams.

Components should interact through explicit interfaces rather than depend on each other's internal implementation.

### 2. Development and execution

Each component must be independently runnable during development, debugging and testing.

The application provides one executable with component subcommands and a
combined production mode:

- `last-orders landlord` runs Landlord.
- `last-orders handpump` runs HandPump.
- `last-orders` (or `last-orders all`) runs the production composition.

The production composition must manage startup, shutdown, component failures and health reporting deliberately.

### 3. Initial deployment

Initially, Last Orders will be distributed as a single Docker image with the
combined production entry point running both components in one deployment.

The initial HandPump production deployment will run as a single backend
instance. Component boundaries and instance-local state should not prevent
future multi-instance operation, but this ADR does not claim that multi-instance
deployment is supported or guaranteed initially.

This is a deliberate operational simplification. It does not mean that Landlord and HandPump are considered the same logical component.

The initial deployment does not require separate Docker images or independent
scaling. Component subcommands remain available for local debugging.

### 3.1 Internal service calls

HandPump owns public HTTP routes. When a route needs state owned by Landlord,
HandPump calls a narrow internal HTTP interface; it does not open or share
Landlord's SQLite database. Email history is the first such use case. In
combined mode, the interface may be served on loopback. In split mode, HandPump
receives Landlord's base URL from runtime configuration (for example
`LANDLORD_INTERNAL_URL`). The email-history route uses
`POST /internal/v1/email-history` with a shared bearer token configured as
`LANDLORD_INTERNAL_TOKEN`; the request carries the authenticated user ID from
HandPump, and Landlord applies the owner filter itself.

The internal listener must bind only to a private service interface. It must not
be routed through Cloudflare, receive a public certificate or be exposed through
the public Scaleway ingress. The public `lastorders.rufty.org.uk` hostname
continues to terminate TLS at Scaleway and route only to HandPump. For a future
split Scaleway deployment, put both containers on a Private Network and set
HandPump's `LANDLORD_INTERNAL_URL` to Landlord's `private_endpoint` URL, formatted as
`http://<container-id>.<private-network-id>.internal`. Configure Landlord's private listener on
the container's application port and disable its public endpoint. The HandPump
container retains the public endpoint and custom domain.

### 4. Future evolution

The architecture should not preclude running Landlord and HandPump separately in the future.

Potential future changes include:

- Running multiple HandPump instances.
- Deploying HandPump instances configured for particular responsibilities.
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

- Whether separate Docker images are justified.
- Whether individual HandPump responsibilities should be deployed independently.
- Whether background jobs should become dedicated services.
- Whether a dedicated messaging or event-distribution system is required as the application grows.
