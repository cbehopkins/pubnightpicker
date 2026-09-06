# CDD: Poll Auto-Completion

## 1. Purpose

The Poll Auto-Completion service completes eligible polls when completion rules
identify a safe, deterministic winner. When no safe winner exists, it creates
normal notification work indicating that manual completion is required.

The service is designed around durable Truths, replayable Cells, and a
conditional Firestore state transition rather than exactly-once execution.

> Auto-completion must never overwrite an already-completed poll.

---

## 2. Scope

### In Scope

* Scheduled discovery of polls eligible for automatic completion.
* Typed Truth construction and idempotent dispatch of that observation.
* Determining a clear winner from the current poll, vote, and venue state.
* Conditional completion of an open poll.
* Manual-completion notification work for ambiguous polls.
* Safe replay and races with manual completion.

### Out of Scope

* Poll creation and voting UX.
* Manual completion UX.
* Restaurant auto-selection.
* Push delivery implementation.
* Downstream notifications following a completed-poll observation.
* Recurring event creation or advancement.

---

## 3. Architectural Position

Auto-completion follows the package responsibilities defined by ADR 0008 and
the Truth boundary defined by ADR 0009.

```text
Cellar timer
    |
    v
truths.DailyPollAutoCompleteDue
    | identity + serialised Fact envelope
    v
components/firebaseidempotency
    |
    v
plugins/autocomplete
    | dispatches Discovery service work
    v
services/autocomplete
    | discovers eligible polls and constructs per-poll Truths
    v
truths.PollAutoCompletionDue
    | idempotency + plugin dispatch
    v
services/autocomplete
    | candidate evaluation
    +-- clear winner --> conditional Close Cell
    +-- ambiguous ----> manual-completion notification work
```

The timer callback and Discovery service construct Truths. The plugin decides
which service Cell follows each Truth. The service handlers perform the
application-specific decision and mutation work. A timer callback or plugin
must not contain the complete completion workflow.

### 3.1 Package Responsibilities

| Responsibility | Package |
| --- | --- |
| Typed auto-completion Truth | `internal/lastorders/truths` |
| Timer callback | `internal/lastorders/database/listeners/autocomplete` |
| Truth-to-service connectivity | `internal/lastorders/plugins/autocomplete` |
| Discovery, Candidate, Close, and ambiguous handlers | `internal/lastorders/services/autocomplete` |
| Fact envelope and idempotency | `internal/lastorders/components` |
| Timer, plugin, and handler construction/registration | `internal/lastorders/app` |

The poll/vote Firestore access used only by auto-completion is service-specific
infrastructure. It must not be added to `components` without an independent
reuse case. The existing venue cache does not include the `food` field, so food
eligibility remains an explicit authoritative Firestore read unless that cache
contract is deliberately extended.

---

## 4. DailyPollAutoCompleteDue and PollAutoCompletionDue Truths

`DailyPollAutoCompleteDue` means:

> Automatic poll-completion discovery is due on this London calendar date.

It is the time-derived Truth identified in ADR 0009. The timer callback creates
it and does not query or complete polls itself:

```go
type DailyPollAutoCompleteDue struct {
    ObservedOn string `json:"observed_on"`
}

func (truth DailyPollAutoCompleteDue) Identity() string {
    return truth.ObservedOn
}
```

The plugin dispatches this Truth to the Discovery service Cell. Discovery then
queries eligible polls and constructs one `PollAutoCompletionDue` Truth for each
matching poll.

`PollAutoCompletionDue` means:

> This open poll, scheduled for this London calendar date, became due for
> automatic-completion consideration.

It does not mean that a particular timer callback fired or that a particular
Firestore query returned a document. The timer is only the initial mechanism
that discovers the Truth.

Discovery converts Firestore data into immutable,
application-owned, serialisable evidence. The source `DocumentSnapshot` must
not be persisted as Truth evidence.

```go
type PollAutoCompletionSnapshot struct {
    PollID      string   `json:"poll_id"`
    PollDate    string   `json:"poll_date"`
    Completed   bool     `json:"completed"`
    VenueIDs    []string `json:"venue_ids"`
    ObservedOn  string   `json:"observed_on"`
}

type PollAutoCompletionDue struct {
    Poll PollAutoCompletionSnapshot `json:"poll"`
}

func (truth PollAutoCompletionDue) Identity() string {
    return truth.Poll.PollID + "_" + truth.Poll.PollDate
}
```

`PollID` identifies the poll document. `PollDate` identifies the scheduled poll
occurrence. Together they define this Truth occurrence; duplicate discovery of
the same poll on the same scheduled date establishes no new dispatch. A later
poll with a different document ID is a distinct occurrence.

The evidence records the poll state which made it eligible at discovery. It does
not make the poll, votes, or venue data permanently frozen for completion.
Candidate and Close deliberately query current state where their decision or
mutation needs it.

### 4.1 Fact Transport and Idempotency

The timer callback and Discovery service serialise their typed Truths into the
existing generic `components/facts.Fact` envelope and create locally idempotent
Cells using:

```text
idempotency.NewCellRequest(
    componentName,
    truth.Identity(),
    Fact{Name: truthName, Payload: serialisedTruth},
)
```

The Fact is durable transport for the typed Truth, not the application-level
observation. Idempotency enforces the Truth identity; it does not define it.
Discovery can be replayed safely because re-observation of the same Truth does
not create another fanout dispatch. `components/idempotency` is authoritative
for these timer-derived and backend-internal observations; Firebase idempotency
is reserved for observations whose source authority is Firestore.

---

## 5. Discovery and Timer Lifecycle

A durable Cellar Timer invokes its callback once per day at the
configured completion time. The operational schedule is:

```text
16:00 Europe/London
```

The callback constructs `DailyPollAutoCompleteDue` and submits it through the
idempotency sequence. The Discovery service handles that Truth and queries for
polls satisfying:

```text
completed == false
date == today in Europe/London
```

For every matching poll, Discovery constructs `PollAutoCompletionDue` and
submits it through its own idempotency sequence. Discovery does not determine a
winner or complete a poll.

The timer has a stable handler name. `internal/lastorders/app` constructs,
registers, and schedules it explicitly during application startup, following
the existing durable-timer lifecycle pattern. The precise initial scheduling
mechanism must preserve the configured London-time behaviour; the timer's
persisted schedule is authoritative after it has been created.

The timer callback must not mutate poll state. A Discovery query failure retries
the Discovery Cell and must not produce partial completion work before a
complete eligible-poll result is available.

---

## 6. Candidate Evaluation

The auto-completion service's Candidate handler receives the immutable
`PollAutoCompletionDue` Truth. It first deliberately reads the current poll
state. If the poll is already completed, no further work is created and the
Cell succeeds as a no-op.

The handler then reads current vote and venue state. These reads answer current
questions which are intentionally distinct from the Truth evidence:

```text
polls/{pollId}.pubs          -> current candidate venue IDs
votes/{pollId}               -> current votes
pubs/{venueId}.food          -> current food eligibility
```

The keys of `polls/{pollId}.pubs` are venue IDs. For every candidate venue,
its vote count is the number of entries in `votes/{pollId}[venueId]`. A missing
or non-list field counts as zero.

### 6.1 Winner Rules

A poll with exactly one candidate venue has that deterministic winner; no vote
document is required.

A multi-venue poll has a clear winner only when:

1. at least one candidate has more than zero votes; and
2. exactly one candidate has the highest vote count.

Therefore, all-zero votes and tied highest votes are ambiguous. A tie has no
implicit tiebreaker.

For a multi-venue poll, the clear vote winner is automatically eligible only
when the current venue document exists and `food == true` as a boolean. A
missing document, missing field, non-boolean value, or `false` is ambiguous.

### 6.2 Candidate Outcome Cells

Candidate evaluation creates one of two typed child Cells using
`cellar.Complete{NewCells: ...}`:

```go
type CompletionClosePayload struct {
    PollID          string `json:"poll_id"`
    SelectedVenueID string `json:"selected_venue_id"`
}

type CompletionAmbiguousPayload struct {
    PollID string `json:"poll_id"`
    Reason string `json:"reason"`
}
```

A clear winner creates a Close Cell. An ambiguous result creates an Ambiguous
Cell. Reasons include `no_votes`, `tie`, `winner_not_food_eligible`, and
`winner_venue_missing`.

The candidate result is a proposal based on the state it observed, not an
unconditional command to overwrite a poll. Child Cells are replayable and must
remain safe if the source state changes before they execute.

---

## 7. Conditional Completion

The Close handler in `services/autocomplete` receives
`CompletionClosePayload`. It deliberately reads the current poll state and is
the only auto-completion component that may transition a poll from open to
completed.

Conceptually:

```text
IF completed == false
THEN
    completed = true
    selected = selectedVenueId
ELSE
    leave the poll unchanged
```

The service-owned Firestore access performs the strongest appropriate
transactional or compare-and-swap operation, with `completed == false` as the
condition. The write updates only `completed` and `selected`; it must preserve
unrelated poll fields. Restaurant selection is not part of this CDD.

If the current poll is already completed, Close succeeds without mutation. This
is not an error, even where the existing selected venue differs from the
candidate result.

### 7.1 Manual Completion Race

```text
Candidate selects Venue A
    |
    v
Human completes poll with Venue B
    |
    v
Close observes completed == true
    |
    v
successful no-op; Venue B remains selected
```

Manual completion wins this race because the existing completed poll is the
authoritative state. The Close handler must never overwrite it.

### 7.2 Duplicate Close Work

A Candidate or Close Cell may be replayed. More than one Close attempt may
therefore exist, but only the attempt which sees `completed == false` can make
the transition. Every later attempt sees a completed poll and succeeds as a
no-op.

---

## 8. Ambiguous Completion

The Ambiguous handler receives `CompletionAmbiguousPayload`. It does not modify
the poll. Notification integration is deliberately deferred because no normal
notification Truth, Fact, or service contract currently exists for this event.
The initial handler records the ambiguity through structured logging and
completes successfully. It must not create a special-purpose notification
mechanism or cause notification failure to complete the poll.

The following conditions never cause automatic completion:

* no votes;
* a tie for highest vote count;
* a multi-venue winner with `food != true`;
* a missing winner venue document; or
* no valid deterministic winner.

The poll remains open for manual action.

---

## 9. Downstream Events, Audit, and Metrics

A successful conditional transition from `completed: false` to `completed: true`
with `selected` set produces the normal completed-poll observation flow.
Auto-completion does not directly implement completion email, completion push,
reschedule notification, or other post-completion actions.

Auto-completion may create the normal immutable poll-action audit record:

```text
actionType = complete
actorUid = backend:auto
pollId
pollDate
selectedVenueId
at
```

Audit is historical evidence, not a completion-correctness mechanism. If audit
persistence is best effort, an audit failure must not reverse a successful poll
completion. Its exact persistence strategy must preserve the broader
poll-action-audit contract.

Operational metrics are best effort and must never fail a completion, alter poll
state, or force a Cell retry.

---

## 10. Failure and Replay Semantics

| Situation | Required behaviour |
| --- | --- |
| Discovery failure | Retry through the durable timer lifecycle. |
| Candidate read or evaluation failure | Retry Candidate evaluation. |
| Close transient Firestore failure | Retry Close; its conditional mutation is safe. |
| Close finds poll already completed | Successful no-op. |
| Ambiguous notification failure | Retry through normal notification work; do not complete the poll. |

Correctness is not exactly-once execution. It follows from immutable Truth
evidence, idempotent Truth dispatch, replayable service Cells, and the
conditional Close mutation.

---

## 11. Safety Invariants

1. Automatic completion never changes a poll whose `completed` field is already
   `true`.
2. Only a deterministic winner is selected automatically.
3. A multi-venue automatic winner has current `food == true`.
4. Candidate decisions are provisional and grant no right to overwrite later
   poll state.
5. Close is the only auto-completion mutation boundary and is conditional.
6. Replaying any Cell does not corrupt poll state.
7. Manual completion wins races with automatic completion.
8. The poll never becomes completed without a valid `selected` venue.
9. Ambiguity leaves the poll open.
10. Downstream completion effects follow durable completed-poll state.

---

## 12. Required Data Contracts

| Document | Required fields | Relevant fields |
| --- | --- | --- |
| `polls/{pollId}` | `date`, `completed` | `pubs`, `selected` |
| `votes/{pollId}` | None | `venueId -> array of voter UIDs` |
| `pubs/{venueId}` | None | `food` |
| `poll_action_audit/{auditId}` | Existing audit contract | `pollId`, `actionType`, `actorUid`, `at`, `pollDate`, `selectedVenueId` |

The core completion decision requires no additional Firestore schema. Missing
or non-list vote fields count as zero.

---

## 13. Testing Contract

Tests must cover:

* construction of the daily and per-poll Truths, immutable evidence, and their
    respective identities;
* idempotent rediscovery and a stable timer registration/lifecycle;
* discovery of today's open polls and exclusion of completed or other-date polls;
* single-venue, all-zero, tied, and uniquely highest-vote decisions;
* missing, false, and non-boolean `food` values;
* deliberate current-state re-evaluation after a delayed Truth;
* Close preserving unrelated fields and succeeding as a no-op for an already
  completed poll;
* the manual-completion race retaining the manual selection;
* replay of Candidate and Close Cells;
* deferred ambiguous handling leaving the poll unchanged; and
* audit and metrics failures not changing completion correctness.

---

## 14. Acceptance Criteria

The feature is complete when:

1. The daily timer constructs `DailyPollAutoCompleteDue` and idempotency
    dispatches it through the auto-completion plugin to Discovery.
2. Discovery constructs a typed `PollAutoCompletionDue` Truth for every
    eligible poll, and idempotency dispatches it through the plugin to Candidate.
3. Candidate evaluation uses current poll, vote, and venue state to produce a
   clear-winner or ambiguous outcome Cell.
4. Only Close conditionally transitions an open poll to completed.
5. Manual completion cannot be overwritten.
6. Ambiguous polls remain open and are recorded without automatic completion.
7. Replay and transient failure converge without corrupting poll state.
8. Downstream completion effects are driven by durable completed-poll state.
9. The safety invariants are covered by automated tests.

The resulting architecture is:

> A timer creates a due Truth, Discovery creates per-poll Truths, the plugin
> chooses the service work, Cellar executes it durably, and conditional
> Firestore completion makes races and retries safe.
