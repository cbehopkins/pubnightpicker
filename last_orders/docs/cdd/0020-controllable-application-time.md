# CDD: Controllable Application Time

**Status:** ready-for-agent; behavioural contract agreed, implementation pending  
**Date:** 2026-11-17  
**Related:** [Scheduled and Timer-Based Observations](../adr/0002-timers.md);
[Application Token Sources](0010-rate-limiting.md);
[Injectable External Dependencies](0011-injectable-external-dependencies.md);
[Poll Auto-Completion](0004-auto-complete.md);
[Landlord Shutdown Draining](0018-landlord-shutdown-draining.md);
[On-Demand Meet Projections](0019-on-demand-meet-projections.md);
[Cellar Durable Named Timers](../../../cellar/docs/adr/0014-durable-timers.md)

## Problem Statement

E2E journeys cannot deterministically exercise behaviour caused by time passing.
Waiting for real midnight or 16:00 is impractical, and changing only the date read
by business logic does not make Cellar's scheduled work eligible to execute.

The immediate example is automatic voting completion at 16:00 in Europe/London.
A journey must establish that voting remains open before its scheduled discovery,
advance time across the deadline, and observe the normal backend pipeline attempt
completion. A later example is yesterday's Meet leaving Current Meets at London
midnight once the backend-owned Meet projections are available.

The capability must not expose a remotely mutable clock in production builds or
require changing the host, browser, Firebase emulator or Firestore server clock.

## Solution

Provide an injectable application clock shared by business-date decisions and
Cellar's durable scheduling. It uses system time by default. An E2E-only build
provides a frozen implementation which the harness initialises before backend
startup and advances through a local control endpoint.

Advancing application time makes due work eligible through the ordinary production
scheduler, handlers, idempotency and authoritative database mutations. It is not a
command to run autocomplete directly. Tests await observable results separately.

Durable retries intentionally advance with application time. Network timeouts,
authentication checks, heartbeats, process supervision and test wait timeouts
continue to use real time. Firestore-generated timestamps remain unchanged.

## User Stories

1. As an E2E author, I want to choose the backend's initial instant before startup, so that startup decisions are deterministic.
2. As an E2E author, I want application time to remain frozen until advanced, so that machine speed cannot move a scenario across a business deadline.
3. As an E2E author, I want to advance time through a local endpoint, so that journeys can exercise time-driven behaviour without waiting for wall-clock time.
4. As an E2E author, I want backwards advancement rejected without changing time, so that already-observed obligations cannot become future work again.
5. As an E2E author, I want explicit-offset instants and London calendar interpretation, so that dates are independent of the host timezone.
6. As an E2E author, I want voting to remain open before its scheduled autocomplete deadline, so that a test can detect premature discovery.
7. As an E2E author, I want crossing 16:00 to exercise the real durable scheduler, so that tests cover scheduling as well as completion rules.
8. As an E2E author, I want eligible voting to complete through the existing pipeline, so that a successful clock command cannot masquerade as business success.
9. As an E2E author, I want ambiguous voting to retain its existing manual-completion behaviour, so that clock control does not alter winner selection.
10. As an E2E author, I want missed calendar occurrences coalesced, so that a large jump does not replay every missed daily job.
11. As an E2E author, I want catch-up to preserve the latest due occurrence's date, so that a jump cannot complete today's voting before today's 16:00 deadline.
12. As an E2E author, I want durable retries to advance with the clock, so that delayed work can be exercised without separate scheduling time domains.
13. As an E2E author, I want a failed retry to schedule relative to the advanced instant, so that a frozen clock does not cause a tight retry loop.
14. As an E2E author, I want real-time bounded waits for observable outcomes, so that a stalled backend fails the test rather than hanging with frozen time.
15. As an E2E author, I want fresh backend, durable and emulator state per time-controlled scenario, so that earlier clocks and idempotency records cannot affect later scenarios.
16. As an E2E author, I want daylight-saving coverage, so that London midnight and 16:00 follow calendar rules rather than fixed UTC offsets.
17. As a backend developer, I want ordinary startup to use system time without extra configuration, so that production behaviour does not depend on test tooling.
18. As a backend developer, I want existing construction and test seams reused, so that clock control does not introduce test branches throughout business logic.
19. As an operator, I want clock mutation absent from ordinary binaries, so that runtime configuration cannot enable it in production.
20. As an operator, I want E2E clock control restricted to local emulator runs, so that a test binary cannot accidentally act against production Firebase.
21. As an operator, I want authentication, networking and shutdown deadlines to retain real-time bounds, so that freezing application time cannot freeze process control.
22. As a developer of backend-owned Current Meets, I want to reuse this clock for London date transitions, so that later midnight journeys need no additional time-control mechanism.

## Implementation Decisions

### Clock Ownership and Scope

- Application composition resolves one clock and passes it explicitly to consumers.
  The ordinary implementation reads system time; do not mutate a package-global
  clock or introduce independent overrides in each consumer.
- Keep the consumer-facing read interface minimal, initially a current-instant
  operation equivalent to `Now() time.Time`. Mutable-clock operations belong to
  the E2E controller, not to ordinary business consumers.
- Calendar decisions interpret instants in Europe/London. Use timezone rules,
  not a fixed UTC offset or an assumption that every local day lasts 24 hours.
- Application time includes business dates, recurrence evaluation, initial
  poll-history cut-offs, application-owned daily token resets, durable timer
  deadlines and durable retry deadlines. Thread the clock through their existing
  collaborators rather than replacing those collaborators with simplified E2E
  implementations.
- Audit deadline producers as well as consumers, including sibling modules used
  for durable email and notification work. A retry built from real time but
  compared against advanced application time is not a conforming integration.
- Keep elapsed-time bounds for external operations, SDK authentication and token
  verification, SSE heartbeats, scheduler polling, shutdown and harness waits on
  real time. An external provider's own clock and retry policy remain external.
- Firestore server timestamps, document update times and transaction preconditions
  remain authoritative and unchanged. These journeys do not assert their exact
  values or compare them to the simulated instant for business eligibility.
- Normal builds default to system time. A supplied clock must not be discarded
  by a nested constructor which silently returns to system time.

### Cellar Scheduling

- Extend Cellar's existing construction, durable timer and result-application
  interfaces to share the injected clock. Existing consumers which do not supply
  a clock retain system-time behaviour.
- Use that clock consistently when creating and rescheduling timers, checking
  `NotBefore`, constructing retries and turning relative retry delays into
  absolute deadlines, including sequence retries and the SQLite and memory stores.
- Use one scheduling time domain. Advancing a day may release both a daily timer
  and a pending five-minute retry. This is accepted, not a bug to prevent with
  separate clocks, per-Cell clock flags or another scheduler.
- After a failed attempt at application instant X, a positive retry delay creates
  a deadline after X. With the clock frozen, that next attempt remains deferred
  until the harness advances far enough.
- Notify the existing scheduling machinery after advancement so it can recheck
  eligibility promptly; do not invoke business callbacks from the control endpoint.
- Preserve atomic claiming, worker-capacity bounds, persistent next deadlines,
  timer identity, restart recovery, callback-error behaviour and production-stop
  fencing. Do not bypass these mechanisms to accelerate a test.
- Shutdown's allowance remains real-time bounded. Drain eligibility uses current
  application time, without independently advancing it or bringing deferred work
  forward to empty the database. This refines the time source, not the draining
  lifecycle or claim-release guarantees.

### Calendar Occurrences and Autocomplete

- Retain the 16:00 Europe/London daily autocomplete schedule and the existing
  Truth, discovery, candidate-evaluation and conditional-completion pipeline.
- The calendar timer supplies occurrence context to its callback. For a late
  execution, coalesce to the latest scheduled occurrence at or before application
  now, rather than replaying each missed date or substituting the destination date.
  Schedule the next occurrence strictly after the sampled application instant.
- Carry that occurrence's London date into the daily discovery Truth and its
  existing date-based identity. Preserve the occurrence date through asynchronous
  execution even if application time advances again before a handler runs.
- For example, Monday 15:59 to Tuesday 15:59 makes Monday's 16:00 occurrence due;
  Tuesday's voting must not become eligible through Tuesday's date. Tuesday
  16:00 or later permits Tuesday's occurrence. Discovery can still include older
  open voting within the persisted processing floor.
- Startup discovery remains a recovery opportunity after required projections
  are ready. It must not consume today's daily identity before today's deadline.
  Use the latest due 16:00 occurrence's date for startup recovery as well: before
  16:00 that is yesterday; at or after 16:00 it is today. This preserves convergence
  between startup and scheduled discovery without suppressing the later daily scan.
- This explicitly refines the existing startup-recovery contract, which scans
  through the current London date regardless of time of day. Update its owning
  ADR and autocomplete design during implementation rather than retaining two
  conflicting rules. It is required for the agreed deadline and catch-up safety.
- Do not change winner selection, manual-completion fallback, conditional writes,
  per-Meet occurrence idempotency or the persisted processing floor. Only the
  source of time and the date up to which discovery is due change.
- Existing business code may retain historical storage names such as `polls`.
  They do not rename a Meet or change the glossary's distinction between a Meet
  and its voting activity.

### E2E Control Contract and Production Exclusion

- Compile the mutable clock implementation, its startup controls and its remote
  mutation endpoint only with an explicit Go `e2e` build tag. Ordinary binaries
  contain neither the mutable-clock implementation nor the control route. A
  runtime environment switch alone does not satisfy this requirement.
- The E2E harness supplies an explicit initial instant before any date-dependent
  initialisation, recovery, projection admission or timer scheduling occurs.
- The clock remains frozen between explicit advancements. Accept instants with
  explicit offsets, preserve instant precision needed by the tests, and reject
  invalid or backwards requests without mutation. Advancing to the current
  instant is an idempotent no-op, not another calendar occurrence.
- Reading and advancement are concurrency-safe. Serialise competing advancements
  so that time never regresses. Already-running work may finish; this is not a
  globally atomic snapshot of all backend activity.
- A successful advance acknowledges the accepted current instant and scheduling
  notification. It does not promise business completion, global quiescence or
  delivery to every subscription. Tests wait for their own observable outcome.
- Serve control only on an explicit loopback listener. Require the existing local
  demo-project and Firestore/Auth-emulator safeguards, no production credentials,
  and dummy external delivery. Fail startup if those conditions are not met; do
  not silently fall back to a real project or real delivery.
- A production build check must establish compile-time exclusion and absence of
  control registration, not merely that the feature defaults to disabled. Keep
  release builds untagged and verify their build inputs cannot inherit `e2e`.
- Where a journey uses more than one backend process, advance every participating
  application clock before awaiting final outcomes. Do not assume a process-local
  override automatically controls another process. Cross-process atomic advance
  and runtime mode-switching are not promised.

### Harness Isolation and Scope of the First Slice

- Time-controlled scenarios start fresh backend processes with fresh local durable
  state and isolated emulator data. Reuse build artifacts, not running backend
  state. Resetting the clock alone is insufficient and is not supported.
- The current harness shares backend and emulator state within its backend group;
  add an explicitly isolated path for time-controlled scenarios without requiring
  unrelated journeys to change lifecycle.
- Retain process-tree ownership, occupied-port refusal, demo projects, allowlisted
  child environments, dummy delivery and failure-safe cleanup. Never reset a
  developer's emulator, import or export its data, or terminate unrelated services.
- Derive scenario dates and processing floors from the configured application
  instant in London, not the runner host's date. Reject stale configuration
  rather than silently filtering all test Meets out of processing scope.
- The first vertical slice establishes voting through normal supported setup,
  observes it open at 15:59, advances to 16:00 and observes eligible completion
  through the existing pipeline. Establish fixture readiness before the crossing;
  do not seed completed results or invoke an autocomplete-only test route.
- This work does not introduce browser-clock manipulation. The immediate journey
  must not depend on browser-generated Today labels matching a simulated backend
  date. Backend-owned Current Meets midnight coverage follows its migration.

## Testing Decisions

- Use the highest useful seam: the existing Cucumber/Playwright journey controls
  only time and observes real backend behaviour through supported application
  interfaces. This is the agreed 15:59-to-16:00 vertical slice, not a new interview
  or a requirement to mock each internal collaborator.
- Prefer API results and subscription state for assertions as available. An
  authenticated emulator read can follow existing journey practice where a
  required read interface does not yet exist; it must not write expected outputs.
- Await explicit readiness and correlated outcomes with real-time timeouts. Do
  not sleep for the simulated duration, infer success from a clock response or
  promise global idleness. Negative deadline assertions need evidence that the
  earlier relevant discovery/work has settled, not an arbitrary short sleep.
- Reuse existing Cellar timer, scheduler, sequence-result, memory-store and SQLite
  tests for clock consistency, delayed eligibility, persistence and recovery.
  Extend existing app-construction, autocomplete and shutdown tests rather than
  creating a parallel scheduler test framework.
- Follow the existing injected-dependency app test bench for normal/system clock
  defaults and propagation of a supplied clock. Use local emulator wiring only
  for authoritative integration behaviour, never a live Firebase project.
- Reuse the current harness lifecycle and support tests to prove tagged build
  selection, initial-time propagation, per-scenario isolation, safety rejection
  and cleanup on failure. Existing recurrence and poll-lifecycle journeys are
  prior art for deterministic dates, actor separation and outcome observation.
- Cover just-before and exactly-at 16:00; eligible and ambiguous voting; multiple
  missed days coalescing to one latest due daily observation; and a jump ending
  before today's 16:00 which does not complete today's voting.
- Cover pre-16:00 startup followed by the same day's deadline, post-16:00 startup
  recovery, duplicate discovery and retained-database restart. Startup must neither
  complete future-due voting nor suppress the later scheduled scan.
- Cover a due retry after advancement, a newly calculated retry remaining deferred
  while frozen, relative sequence retries and calendar rescheduling. Exercise both
  stores' public behaviour; do not assert private field layout or SQL text.
- Cover London winter/summer dates and both daylight-saving transitions, including
  next-occurrence calculation and application-owned daily token resets. Do not
  treat calendar days as fixed-duration intervals.
- Cover malformed advancement, rejected backwards movement, equal-time no-op,
  concurrent advancement and real-time timeout/shutdown behaviour while frozen.
- Verify normal-build exclusion and E2E runtime safeguards independently. A control
  route returning not-found is useful behavioural evidence but is not sufficient
  proof that the mutable implementation was excluded from the build.
- During implementation, use focused checks before broader relevant gates. This
  CDD records verification requirements; authoring it does not require starting
  emulators, running suites or exercising external delivery.

## Out of Scope

- Migrating frontend Firebase queries to backend interfaces, changing current
  frontend date helpers or introducing Playwright browser-clock control.
- Implementing Current Meets projections, midnight invalidation or their E2E
  scenario before the backend-owned projection migration is available.
- Overriding Firestore server timestamps, emulator wall clocks, host clocks,
  authentication validity or third-party provider clocks.
- A separate operational scheduling clock, per-Cell time-domain metadata, a
  replacement calendar scheduler or replay of every missed daily occurrence.
- Moving time backwards within a scenario, sharing mutable-clock scenarios across
  one durable database, or guaranteeing cross-process atomic time changes.
- Exactly-once external delivery, bypassing normal retries or rate limits, and
  production-accessible time controls guarded only by configuration or credentials.
- New winner-selection rules, a general scheduler rewrite, new persistent process
  state solely to work around an insufficient interface, or unrelated refactoring.

## Further Notes

The revised agreement intentionally advances all durable scheduling, including
retries, with application time. Earlier discussion of keeping retry deadlines on
a second clock is superseded. With frozen time, future retries require another
advance; this is an explicit test-harness responsibility.

The proposed timer extension must preserve Cellar's accepted durable named-timer
contract. The older Last Orders timer ADR contains V0 historical material; it is
not a reason to replace the current durable runtime with ephemeral test timers.

The existing Current Meets projection CDD specifies London-midnight retirement,
a generic stream reset and closure, followed by client reconnection to a fresh
view. Earlier discussion described an update without reconnecting. This CDD does
not override the projection contract: future journeys should use its existing
reset/reconnect behaviour unless that contract is separately revised. The user
must not need a manual browser refresh, but uninterrupted transport is not promised.

Implement in dependency order: Cellar clock propagation and occurrence context;
Last Orders integration and startup-discovery alignment; compile-time-gated local
controls; then isolated harness support and the autocomplete vertical slice.
Keep the exact endpoint spelling and internal clock type names local to
implementation; the behavioural and safety contracts above are authoritative.
