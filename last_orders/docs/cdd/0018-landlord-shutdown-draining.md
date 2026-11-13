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
existing EventVenueObserved claim, even when its downstream claim never happened.
Therefore merely leaving an event pending does not guarantee immediate takeover.
A changed venue date produces different observation evidence and can be processed
independently. See [CDD-0005](0005-event-recurrence.md).

## 7. Releasing an Unfulfilled Recurrence Claim

Release is permitted only for an `EventVenueObserved` claim whose corresponding
fanout Cell is still `READY`. Its recurrence evaluator has not run, so no
`StaleEvent` or `CreateEventPoll` request has been generated and no recurrence
application data has been changed. The current `EventVenueObserved` registry has
only this evaluator. If the fanout Cell is claimed, missing, or has advanced past
this pre-evaluation state, do not release under this protocol.

This restriction is intentional. After evaluation, downstream idempotency work
has no durable parent link to the observation, and its idempotency key may be
shared by multiple observations. Do not delete downstream Sequences as part of
releasing an upstream claim; doing so could discard work that is not exclusively
owned by the observation being released.

The approved release protocol is:

1. Stop fresh claim attempts as part of shutdown. The release candidate must be
  the exact `READY` `EventVenueObserved` fanout Cell; a claimed or missing Cell
  is ineligible. Do not start release if ownership or execution state is
  inconclusive.
2. Persist the Firebase idempotency Sequence owner in the local cache in the same
  SQLite transaction in which the Check step establishes the local identity and
  advances the Sequence. The value is `StepPayload.Owner`, which is also the
  Firestore document's `owner`; it is not the ID of the emitted fanout Cell. Add
  a nullable `owner` column to `firebase_idempotency_records`. Existing
  `(listener, event_key)` identity is unchanged. New rows for all listeners
  retain their Sequence owner because the Check implementation is shared, but
  release remains restricted to the exact `event_venue_observed` row for this
  candidate. Legacy rows with no owner are ineligible; never infer or
  reconstruct an owner.
3. In one SQLite transaction, delete only the identified `READY` fanout Cell and
  its matching `event_venue_observed` cache row, checking the exact listener,
  event key, and owner. The transaction fails without changing either record if
  the Cell is missing or no longer `READY`, or the cache row does not match.
  Provide a narrowly scoped Cellar Store operation to perform this lifecycle
  transition together with `ApplicationWork`. `Kill` is not used: it terminates
  a currently executing Cell, while this candidate is `READY`. Do not use the
  offline `ForceDelete` operation as a substitute.
4. Only after the SQLite transaction commits, delete the Firestore claim in a
  Firestore transaction that reads the document and deletes it only if its owner
  matches the recorded `StepPayload.Owner`. A missing document is already
  absent; a different owner is preserved. Never use an unconditional delete or
  a separate unprotected read followed by delete.
5. Report release success only when the owner-checked deletion is confirmed or
  the document is confirmed absent. A different owner, failed request, or
  inconclusive response is not a successful handover. Do not restore retired
  local work or promise a retry.

SQLite retirement is deliberately first. If Firestore is deleted first and the
process stops before local retirement, retained SQLite could resume the old
evaluation after another instance has acquired the identity. With local-first
ordering, interruption before the SQLite commit leaves both stores untouched.
Interruption after that commit prevents the old work from resuming, but the
remote claim may remain and suppress future work; if the Firestore response is
inconclusive, report release as incomplete even if the document may in fact be
absent. This best-effort loss is accepted, and no durable cleanup retry is
promised.

| Interruption point | Required recovery outcome |
| --- | --- |
| Before local retirement commits | The remote claim and fanout Cell remain; ordinary retained-database recovery applies. |
| During the SQLite transaction | Cell retirement and cache-row deletion roll back together; the remote claim is untouched. |
| After SQLite commit, before or during remote deletion | The old Cell cannot resume. The remote claim may remain or may be absent; report incomplete release unless absence/deletion is confirmed. |
| After confirmed remote deletion | The old Cell is retired and a fresh instance can retry unchanged evidence. |
| A replacement claim owns the document | Conditional deletion preserves it; report no successful release. The retired local Cell does not resume. |

A fresh-database instance observing after confirmed release can attempt the
unchanged observation again. An already-running instance may have cached the
claim and continue suppressing it after deletion, so release is not a guaranteed
handover. This is a narrow release of an unfulfilled observation, not a generic
claim rollback or a change to recurrence's separate downstream claim boundaries.

## 8. Implementation Verification and Follow-Up

Implementation should demonstrate bounded shutdown, no fresh claims, preservation
of in-flight happy-path execution, runnable child work, quiescence with concurrent
workers, current-time retry eligibility and retained-database recovery. For
gate-refused idempotency work, verify termination and local cache retirement,
read-only owner reconciliation, conditional cleanup only for the persisted owner,
replacement-owner preservation and a subsequent observation against retained and
fresh databases. Verify that unresolved lookup, failed local retirement and
inconclusive remote deletion retain the required scheduler-stopping semantics.
For recurrence claim release, verify the `READY` pre-evaluation eligibility rule;
owner mismatch and delayed cleanup against a replacement claim in the emulator;
rollback when the SQLite retirement transaction fails; interruption before and
after the SQLite/Firestore boundary with retained storage; and fresh-instance
retry versus an already-running instance with a cached claim. Do not release a
claimed or missing fanout Cell or delete downstream Sequence work.

Conduct a separate multi-instance safety review, including local-only idempotency
such as daily auto-completion. The existing remote claim gate plus this drain
contract is not proof that every Landlord operation is multi-instance safe.
