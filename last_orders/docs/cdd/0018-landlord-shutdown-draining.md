# CDD: Best-Effort Landlord Shutdown Draining

## 1. Status and Scope

Behavioural contract agreed. Landlord shutdown draining, in-flight claim
reconciliation, deferred-work recovery and recurrence claim release follow the
contract in sections 3-7. The owner-checked release protocol is approved, with its
required interface and schema changes described in section 7.

This design applies to Landlord's notifications and housekeeping, not HandPump's
public API. It extends [ADR-0007](../adr/0007-lifecycle.md) for environments where
multiple Landlord instances may overlap and local SQLite storage may disappear.
It does not introduce a shared Cellar database, message bus or general claim lease.

## 2. Guarantees and Accepted Loss

Retaining the SQLite database remains the preferred deployment model: Cellar can
recover partially executed work, including future retries, after restart.

An ephemeral deployment cannot provide that guarantee. After a successful
Firestore claim, losing the local database can permanently lose the associated
work, even if the instance disappears immediately after claiming. A replacement
instance normally suppresses that observation because the remote claim survives.
This loss is explicitly accepted. Shutdown draining reduces it during orderly
termination; it does not make ephemeral storage durable or guarantee exactly-once
external effects.

## 3. Configuration and Deadline

Provide one duration option, `-shutdown-drain-timeout`:

- Zero disables draining and retains the existing shutdown behaviour.
- A positive value enables draining and bounds the whole graceful shutdown,
  including stopping producers, rather than starting another allowance afterwards.
- Negative values are invalid.

The operator must choose a duration below the host's termination allowance.
Forced termination can still interrupt shutdown. Establish the target host's
signal delivery and termination allowance before relying on this mode.

## 4. Shutdown Order and Eligible Work

On a graceful shutdown request:

1. Prevent fresh Firestore claim attempts and stop external work producers,
   including database listeners and work-accepting HTTP endpoints. Await their
   shutdown so callbacks cannot continue adding observations.
2. Resolve already-issued claim attempts where possible, within the same deadline.
3. Keep Cellar and the clients, configuration and cached state required by accepted
   work available. The shutdown signal must not immediately cancel worker contexts.
4. Execute runnable work associated with confirmed claims, including ordinary
   Cellar steps and further work units created by that execution.
5. Stop execution and close resources on quiescence, deadline expiry, an existing
   scheduler-stopping error or forced termination.

Quiescence means both no eligible runnable work and no in-flight execution which
can create more eligible work. An empty queue while a worker is active is not
sufficient. Idempotency owns the decision for work that encounters the closed
claim gate: a refused Populate Remote Sequence is safely relinquished rather than
kept claim-blocked for restart. This is not a scheduler handler-name exclusion.

Eligibility uses the current time throughout draining, not a shutdown-time
snapshot. A scheduled retry becomes eligible if its time arrives while other work
is still running. Once quiescent, exit without waiting for a future retry or timer.
Close periodic observation timer production at the timer source before entering the
drain. A callback admitted before closure may settle and its accepted work follows
ordinary drain and claim-gate rules. Queued occurrences dispatched after closure do
not start scans; callbacks finishing concurrently with closure cannot persist another
occurrence. Do not distinguish periodic work in scheduler eligibility by timer ID,
handler name, or decoded Cell payload. Future timer state remains deferred and
recoverable rather than being brought forward or deleted.

Email and webpush work should be allowed to finish their happy-path persistent
steps. External failures may still record Retry Cells. Respect existing delays,
rate limits and retry semantics; do not bring future work forward to empty the
database. Future work can arise from normal rate limiting, not only errors.

Do not delete unfinished or deferred work as part of ordinary draining. It remains
recoverable if storage survives; its loss is accepted if storage disappears.
Preserve Cellar's existing scheduler-stopping error behaviour rather than adding
error isolation or promising that unrelated work continues after such an error.

Report the shutdown outcome and remaining runnable, in-flight and deferred work
where available. A successful drain must not be reported as completion of future
work.

## 5. Claims and Uncertain Responses

The existing Firebase idempotency Sequence is persisted before its remote claim:

```text
Check -> Populate Remote -> Emit Truth -> Fanout -> application work
```

Populate Remote uses Firestore create-if-absent. The record's owner identifies the
persisted Sequence, not the process. A competing owner terminates without Truth
emission. Retrying the same persisted Sequence can recognise its own remote claim.
A fresh database cannot inherit that Sequence or its lost workload.

Stopping listeners alone does not stop claim attempts: an already queued Sequence
may not have reached Populate Remote yet, and executing application work may
produce another idempotency Sequence. Neither may acquire a fresh claim during
draining.

A claim request issued before shutdown may commit after shutdown begins. If it
succeeds, its work is our responsibility and should be drained. A lost response
does not prove that the write failed. A claim request admitted before shutdown is
allowed to finish as an in-flight execution. If its CreateKey call returns an
error after the claim fence closes, Landlord performs a read-only owner lookup
using the persisted idempotency Sequence owner. A matching owner advances the
Sequence so its Truth can be emitted; a different non-empty owner terminates the
Sequence without emitting the Truth. A missing or ownerless record, or a failed
lookup, remains unresolved and returns the existing scheduler-stopping ErrorResult.
Reconciliation never calls CreateKey and therefore cannot establish a missing
claim during shutdown.

A missing record is not proof that the original request can no longer commit.
Landlord does not retry or wait beyond the existing shutdown deadline to resolve
that uncertainty; loss remains within the accepted best-effort policy. Because the
claim handler remains an active Cellar dispatch until reconciliation finishes, it
is included in drain quiescence accounting.

A Populate Remote Sequence refused by the closed gate performs a read-only owner
lookup; it never calls CreateKey. A failed lookup or an existing ownerless record
is unresolved and follows the existing scheduler-stopping failure path without
retiring the Sequence or cache row. If the record is absent, or belongs to another
owner, the local Sequence and its matching cache row are retired atomically; a
competing owner's remote record is never deleted. If the record belongs to this
Sequence, the same local transaction retires the Sequence and cache row and adds
a cleanup Cell. That Cell conditionally deletes only the recorded owner. A
replacement owner or inconclusive deletion is reported as incomplete through the
existing scheduler-stopping failure path; no implicit retry loop is added. A
failed local retirement rolls back the Sequence termination and cleanup Cell
together. This explicit relinquishment is separate from the recurrence release
protocol in section 7.

## 6. Recurrence Boundary

The current recurrence pipeline is not one claim covering all mutations:

```text
claim EventVenueObserved -> evaluate
  stale -> claim StaleEvent -> update venue occurrence date
  due   -> claim CreateEventPoll -> create poll, votes and attendance
```

After updating the venue date, another listener observation or periodic
re-evaluation is needed to evaluate that changed evidence. Poll creation does not
directly execute poll notifications: the separate new-poll listener observes the
created document and establishes PollOpened independently.

A same-day restart can observe unchanged venue evidence but be suppressed by the
existing `EventVenueObserved` claim A, even when claim B was never established.
Therefore merely leaving an event pending does not guarantee immediate takeover.
A changed venue date produces different observation evidence and can be processed
independently. See [CDD-0005](0005-event-recurrence.md).

## 7. Releasing an Unfulfilled Recurrence Claim

During draining, the scheduler uses ordinary atomic `ClaimNext` for every
runnable Cell; it does not filter by handler name or inspect Cell payloads. The
closed Firebase claim gate is the recurrence cancellation signal. When an
`EventVenueObserved` evaluator runs after that gate closes, it stops before
creating either a `StaleEvent` or `CreateEventPoll` request and relinquishes
claim A instead.

Ticket 8 refines ticket 5's earlier READY-only rule. The cancelled work is the
`EventVenueObserved` evaluator Cell, whether it was waiting for fanout expansion
or was created by that expansion. This is not permission to cancel recurrence
work that the evaluator already emitted before shutdown.

The release protocol is:

1. Close the Firebase claim gate before entering drain. The recurrence evaluator
  checks the gate before evaluation and again before it emits recurrence work.
2. Derive the exact `event_venue_observed` key from the observation and look up
  its owner in the local Firebase idempotency cache. The cache stores
  `StepPayload.Owner`; the observation payload needs no additional claim fields.
  Missing, ownerless, or unreadable cache state is unresolved: do not release the
  remote claim, and preserve the existing scheduler-stopping error behaviour.
3. Return `Kill` for the currently claimed evaluator Cell, with `ApplicationWork`
  that deletes only the matching cache row and a release Cell for the remote
  claim. Cellar commits the evaluator termination, cache deletion, and release
  Cell together in one SQLite transaction. If that transaction fails, none of
  those changes take effect.
4. The release Cell uses the existing Firestore transaction to delete claim A
  only if the stored owner still matches. A missing document is already absent;
  a replacement owner is preserved. A failed, mismatched, or inconclusive
  release is not success and retains scheduler-stopping failure semantics.

If the process stops before the SQLite transaction commits, the evaluator and
cache row remain and the remote claim is untouched. After commit, the evaluator
cannot resume and the release Cell remains durable in retained SQLite. If remote
deletion was inconclusive, retrying that Cell after restart is owner-safe: an
absent document is success and a replacement owner is preserved. A fresh
database cannot recover the local cleanup Cell, so loss of ephemeral storage can
still leave claim A in Firestore. A fresh instance can retry unchanged evidence
after confirmed deletion; an already-running instance may retain its local
cache and continue suppressing it.

| Interruption point | Required recovery outcome |
| --- | --- |
| Before local retirement commits | The evaluator Cell and cache row remain; the remote claim is untouched. |
| After SQLite commit, before or during remote deletion | The evaluator cannot resume; the owner-checked release Cell remains for retained-database recovery. |
| After confirmed remote deletion | The evaluator is retired and a fresh instance can retry unchanged evidence. |
| A replacement claim owns the document | Conditional deletion preserves the replacement owner; report a scheduler-stopping failure. |

A fresh-database instance observing after confirmed release can attempt the
unchanged observation again. An already-running instance may have cached the
claim and continue suppressing it after deletion, so release is not a guaranteed
handover. This is a narrow cancellation of an `EventVenueObserved` evaluator
that sees shutdown before it emits recurrence work; it does not roll back
`StaleEvent` or `CreateEventPoll` work already emitted.

## 8. Implementation Verification and Follow-Up

Implementation should demonstrate bounded shutdown, no fresh claims, preservation
of in-flight happy-path execution, runnable child work, quiescence with concurrent
workers, current-time retry eligibility and retained-database recovery. For
gate-refused idempotency work, verify termination and local cache retirement,
read-only owner reconciliation, conditional cleanup only for the persisted owner,
replacement-owner preservation and a subsequent observation against retained and
fresh databases. Verify that unresolved lookup, failed local retirement and
inconclusive remote deletion retain the required scheduler-stopping semantics.
For recurrence cancellation, verify that an evaluator seeing the closed claim
gate emits no `StaleEvent` or `CreateEventPoll` request, retires only its own
local cache row, and schedules owner-checked release of claim A. Cover missing
or ownerless cache data, replacement ownership, a remote release failure,
retained-SQLite recovery of the release Cell, and fresh-instance retry after
confirmed release. Verify that recurrence work emitted before gate closure is
not rolled back.

Conduct a separate multi-instance safety review, including local-only idempotency
such as daily auto-completion. The existing remote claim gate plus this drain
contract is not proof that every Landlord operation is multi-instance safe.
