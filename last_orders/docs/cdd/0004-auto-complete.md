# CDD: Poll Auto-Completion

## 1. Purpose

The Poll Auto-Completion service automatically completes eligible polls when the configured completion rules identify a safe, deterministic winner.

When the rules cannot safely identify a winner, the service does not complete the poll and instead creates work to notify users that manual completion is required.

The service is designed around **replayable Cells and conditional state transition**, rather than exactly-once execution.

The fundamental safety rule is:

> **Auto-completion must never overwrite an already-completed poll.**

---

# 2. Scope

### In scope

* Scheduled discovery of polls eligible for automatic completion.
* Determining whether a poll has a safe automatic winner.
* Creating completion work for a clear winner.
* Creating manual-completion notification work for ambiguous polls.
* Conditionally completing an open poll.
* Handling races between automatic and manual completion.
* Safe replay of all Cells involved in the workflow.

### Out of scope

* Poll creation.
* Poll voting UX.
* Manual poll completion UX.
* Restaurant auto-selection.
* The implementation of push notification delivery.
* Downstream notifications resulting from a successfully completed poll.
* Recurring event creation or advancement.

Restaurant selection may be added to the completion decision in a future extension.

---

# 3. Architecture

The service consists of three principal Cell types preceded by a scheduled housekeeping Cell:

1. **Completion Discovery Cell**
2. **Completion Candidate Cell**
3. **Completion Close Cell**
4. **Completion Ambiguous Cell**

The intended flow is:

```text
Scheduled trigger
      │
      ▼
Completion Discovery
      │
      │ one Cell per eligible poll
      ▼
Completion Candidate
      │
      ├───────────────┐
      │               │
 clear winner      ambiguous
      │               │
      ▼               ▼
Completion Close  Completion Ambiguous
      │               │
      ▼               ▼
Firestore        Push notification
conditional      workflow
completion
```

The Cells form a pipeline.

No individual Cell is required to perform the entire operation.

---

# 4. Architectural Principles

## 4.1 Discovery is not completion

The scheduled Cell discovers eligible polls.

It does not complete them.

## 4.2 Candidate evaluation is a pure decision

The Completion Candidate Cell reads the current poll and associated data and applies the completion rules.

Its output is a decision:

```text
winner identified
```

or:

```text
manual completion required
```

It does not itself modify the poll.

## 4.3 Close is the concurrency boundary

Completion Close is the only component that transitions an open poll to completed.

It performs a conditional write based on the current poll state.

The candidate's decision is therefore **not an unconditional instruction to overwrite the poll**.

## 4.4 Cells are replayable

Any Cell may be executed more than once.

Correctness must not depend on exactly-once Cell execution.

## 4.5 Current state is authoritative

Completion Candidate operates on the poll state available when it executes.

Completion Close re-checks the current Firestore state before changing it.

This deliberately allows the world to change between decision and execution.

---

# 5. Completion Discovery

The scheduled Completion Discovery Cell runs once per day at the configured housekeeping time.

The current operational schedule is:

```text
16:00 Europe/London
```

The discovery operation considers polls satisfying:

```text
completed == false
date == today
```

The date is interpreted using the service's configured Europe/London scheduling semantics.

For every matching poll, the discovery Cell creates a Completion Candidate Cell containing:

```text
pollId
```

The poll ID is the identity of the work.

The discovery Cell does not need to determine the eventual winner.

---

# 6. Discovery Replay

Discovery is allowed to be replayed.

For example:

```text
Discovery #1
    ↓
poll A found
    ↓
CompletionCandidate(A)

Discovery #2
    ↓
poll A found again
    ↓
CompletionCandidate(A)
```

This is acceptable.

The architecture does not require the discovery operation itself to establish exactly-once downstream execution.

Any duplicate work must be safe because subsequent Cells are replayable.

---

# 7. Completion Candidate

The Completion Candidate Cell receives:

```text
pollId
```

It loads the current:

```text
polls/{pollId}
```

document.

The poll must still be eligible for automatic completion.

At minimum:

```text
completed == false
```

must be true when the Candidate evaluates the poll.

If the poll is already completed, the Candidate performs no completion work and succeeds without error.

This allows a Candidate to safely execute after a human or another automatic Cell has already completed the poll.

---

# 8. Candidate Venue Set

Candidate venues are obtained from:

```text
polls/{pollId}.pubs
```

The keys of the `pubs` map are venue IDs.

The Candidate evaluates those venue IDs according to the completion mode.

---

# 9. Single-Venue Completion

If exactly one venue is present in the poll:

```text
number of candidate venues == 1
```

that venue is the deterministic winner.

No vote document is required for this decision.

The Candidate creates:

```text
Completion Close Cell
```

containing at least:

```text
pollId
selectedVenueId
```

---

# 10. Multi-Venue Completion

If multiple candidate venues exist, the Candidate evaluates votes.

The vote document is:

```text
votes/{pollId}
```

For each candidate venue:

```text
count = number of entries in votes/{pollId}[venueId]
```

If the field is absent or is not a list:

```text
count = 0
```

The Candidate then determines the highest vote count.

---

# 11. Clear Winner Rule

A multi-venue poll has a clear winner only when:

1. at least one candidate has more than zero votes;
2. exactly one candidate has the highest vote count.

Therefore:

```text
all candidates have zero votes
    → ambiguous

two or more candidates share the highest vote count
    → ambiguous

exactly one candidate has the highest positive vote count
    → candidate winner
```

A tie is never automatically resolved.

There is no implicit tie-breaker.

---

# 12. Food Eligibility

For multi-venue polls, a clear vote winner must also satisfy the venue food requirement.

The venue document is:

```text
pubs/{venueId}
```

The winner is automatically eligible only when:

```text
food == true
```

The value must be a boolean `true`.

The following therefore do not qualify:

```text
food missing
food == false
food is not boolean
venue document missing
```

If the clear winner fails this requirement, the poll is treated as requiring manual completion.

---

# 13. Candidate Decision

The Candidate therefore produces one of two decisions:

```text
CLEAR WINNER
    pollId
    selectedVenueId
```

or:

```text
MANUAL COMPLETION REQUIRED
    pollId
    reason
```

Possible reasons include:

```text
no_votes
tie
winner_not_food_eligible
winner_venue_missing
```

The exact reason values are implementation-defined but should provide sufficient operational information to explain why automatic completion was not performed.

---

# 14. Completion Close

The Completion Close Cell receives:

```text
pollId
selectedVenueId
```

It loads the current poll state.

The purpose of this Cell is to perform the final, conditional state transition.

The Candidate's earlier decision is treated as a proposal, not as an unconditional command.

---

# 15. Conditional Completion

Completion Close must only change the poll when the poll is still open.

Conceptually, the operation is:

```text
IF completed == false
THEN
    completed = true
    selected = selectedVenueId
ELSE
    leave the poll unchanged
```

The implementation should use the strongest Firestore conditional/transaction mechanism appropriate to guarantee that the completion operation cannot overwrite a concurrently completed poll.

The operation should ideally provide compare-and-swap semantics:

```text
expected:
    completed == false

new value:
    completed == true
    selected == selectedVenueId
```

The exact Firestore implementation is an implementation detail.

The semantic contract is not.

---

# 16. Race With Manual Completion

A human may complete the poll after the Candidate has selected a winner but before Completion Close executes.

For example:

```text
Candidate
    ↓
winner = Venue A

Human
    ↓
completes poll with Venue B

Completion Close
    ↓
poll already completed
```

Completion Close must not overwrite the human's decision.

The result is a successful no-op.

The existing completed poll is trusted as the authoritative result.

Therefore:

```text
automatic candidate = Venue A
actual poll selection = Venue B
```

does **not** constitute an error.

The service simply recognises that the required state transition has already been performed.

---

# 17. Duplicate Completion Close Cells

Multiple Completion Candidate Cells may independently select the same winner.

For example:

```text
Candidate A → Venue A
Candidate B → Venue A
```

This may result in multiple Completion Close Cells.

Only one needs to perform the actual state transition.

The others must safely observe that the poll has already been completed and finish successfully without overwriting it.

Thus:

```text
Close A
    → completed == false
    → completes poll

Close B
    → completed == true
    → successful no-op
```

This is the principal idempotency mechanism of the service.

---

# 18. Auto-Completion Idempotency

The service does **not** require the entire workflow to execute exactly once.

Instead, each stage is replayable.

```text
Discovery
    → may rediscover poll

Candidate
    → may recalculate same decision

Close
    → may attempt same transition

Firestore
    → guarantees that an already-completed poll is not overwritten
```

The resulting invariant is:

> **Repeated execution can cause additional attempts, but cannot cause an already-completed poll to be changed by automatic completion.**

This is the primary correctness property of the design.

---

# 19. Completion Ambiguous

When Completion Candidate cannot safely determine a winner, it creates a Completion Ambiguous Cell.

The Completion Ambiguous Cell contains at least:

```text
pollId
reason
```

It does not modify the poll.

Its responsibility is to request the normal push-notification workflow indicating that manual completion is required.

The notification event is:

```text
poll_manual_completion_required
```

The Completion Ambiguous Cell should not contain the implementation details of push delivery.

It invokes the application's normal notification mechanism.

---

# 20. Replay of Completion Ambiguous

Completion Ambiguous must also be safe to replay.

If the same ambiguous poll produces multiple notification requests, normal notification infrastructure is responsible for applying its normal deduplication semantics.

The auto-completion service must not introduce a second, special-purpose push deduplication mechanism.

The service's responsibility ends at producing the appropriate notification work.

---

# 21. No Completion on Ambiguity

The following conditions must never result in automatic completion:

* no votes;
* tie for highest vote count;
* winner has `food != true`;
* winner venue document does not exist;
* no valid deterministic winner.

Instead:

```text
Candidate
    ↓
Completion Ambiguous
```

The poll remains open for human action.

---

# 22. Completion Write

A successful automatic completion writes:

```text
polls/{pollId}
```

with:

```text
completed: true
selected: selectedVenueId
```

The write must not remove or overwrite unrelated poll fields.

The operation is therefore a merge/update operation.

No restaurant selection is currently performed by auto-completion.

Future versions may extend the completion decision to select a restaurant.

---

# 23. Downstream Completion Events

The successful transition:

```text
completed: false
        ↓
completed: true
```

with:

```text
selected
```

set produces the normal downstream completed-poll event flow.

Auto-completion does not directly implement:

* completion email;
* completion push;
* reschedule notifications;
* other post-completion actions.

Those are separate consumers of the completed-poll state/event.

The contract for Auto Completion is therefore simply to produce a correctly completed poll.

---

# 24. Audit

Automatic completion may produce an immutable poll action audit record.

The logical audit event is:

```text
actionType = complete
actorUid = backend:auto
```

with:

```text
pollId
pollDate
selectedVenueId
at
```

Audit is historical evidence and must not become part of the completion correctness mechanism.

If an audit write is best effort, an audit failure must not reverse a successful poll completion.

The exact audit persistence strategy should preserve the broader poll-action-audit contract.

---

# 25. State and Side-Effect Ordering

The critical ordering is:

```text
Candidate
    │
    ├── decision only
    │
    ▼
Completion Close
    │
    ├── conditional poll completion
    │
    ▼
completed poll
    │
    └── downstream completion event
```

Completion Close must not send downstream "poll completed" effects before the poll has actually reached the completed state.

This prevents notifications describing a completion that did not occur.

---

# 26. Failure Semantics

A Cell failure should be classified according to whether retrying can safely continue the operation.

### Discovery failure

Retry discovery.

### Candidate read/evaluation failure

Retry Candidate evaluation.

### Completion Close transient failure

Retry the Close Cell.

The conditional completion operation makes this safe.

### Completion Close discovers poll already completed

Successful no-op.

### Ambiguous notification failure

Retry according to the normal notification workflow.

The notification failure must not complete the poll.

---

# 27. Safety Invariants

### Invariant 1 — Never overwrite manual completion

Automatic completion must never change a poll whose `completed` field is already true.

### Invariant 2 — Only deterministic winners are automatic

A tie or absence of a positive winner never results in automatic completion.

### Invariant 3 — Food requirement

A multi-venue automatic winner must have `food == true`.

### Invariant 4 — Candidate decisions are provisional

A Candidate's selected venue does not grant permission to overwrite later poll state.

### Invariant 5 — Close is conditional

Only an open poll may be transitioned to completed by automatic completion.

### Invariant 6 — Replay is safe

Repeating any Cell must not corrupt poll state.

### Invariant 7 — Manual completion wins races

If human completion occurs between Candidate and Close, the human decision is retained.

### Invariant 8 — No partial completion

The poll must not become `completed == true` without a valid `selected` venue.

### Invariant 9 — Ambiguity leaves poll open

An ambiguous Candidate result never completes the poll.

### Invariant 10 — Downstream effects follow durable state

Completion notifications are driven only after the poll has actually become completed.

---

# 28. Responsibility Boundaries

| Responsibility                               | Component                               |
| -------------------------------------------- | --------------------------------------- |
| Scheduled daily invocation                   | Cellar scheduler / housekeeping trigger |
| Find today's open polls                      | Completion Discovery                    |
| Create candidate work                        | Completion Discovery                    |
| Read poll/vote/venue state                   | Completion Candidate                    |
| Apply winner-selection rules                 | Completion Candidate                    |
| Decide clear winner vs ambiguity             | Completion Candidate                    |
| Request manual-completion notification       | Completion Ambiguous                    |
| Conditionally complete poll                  | Completion Close                        |
| Protect against concurrent manual completion | Completion Close / Firestore            |
| Produce completion audit                     | Completion Close / application service  |
| Send downstream completion notifications     | Separate completion event flow          |
| Push delivery                                | Notification subsystem                  |

---

# 29. Required Data Contracts

The current authoritative Firestore contracts required by this service are:

### Poll

```text
polls/{pollId}
```

Required fields:

```text
date
completed
```

Relevant fields:

```text
pubs
selected
```

### Votes

```text
votes/{pollId}
```

Relevant structure:

```text
venueId → array of voter UIDs
```

Missing or non-list vote fields count as zero.

### Venue

```text
pubs/{venueId}
```

Relevant field:

```text
food
```

### Poll action audit

```text
poll_action_audit/{auditId}
```

Relevant completion fields:

```text
pollId
actionType
actorUid
at
pollDate
selectedVenueId
```

The service does not require any additional Firestore schema to implement the core completion decision beyond these contracts.

---

# 30. Testing Contract

Tests must cover the decision rules and concurrency semantics.

### Discovery

* finds today's uncompleted polls;
* ignores completed polls;
* ignores polls for other dates;
* creates candidate work for discovered polls;
* replaying discovery is safe.

### Candidate

* one venue selects that venue;
* multiple venues calculate vote counts;
* missing votes count as zero;
* non-list vote values count as zero;
* all-zero votes are ambiguous;
* ties are ambiguous;
* unique highest vote is selected;
* missing venue is ambiguous;
* `food == true` permits automatic completion;
* `food == false` is ambiguous;
* non-boolean food is ambiguous;
* replay produces the same decision from unchanged state.

### Close

* open poll is completed;
* selected venue is written;
* unrelated poll fields are preserved;
* already-completed poll is not changed;
* already-completed poll succeeds as a no-op;
* human completion with a different venue is preserved;
* duplicate Close Cells cannot overwrite one another's completed result;
* transient write failure is retryable.

### Ambiguous

* ambiguous result leaves poll open;
* notification work is created;
* notification failure does not complete the poll;
* replay is safe according to normal notification deduplication semantics.

### Integration

* successful auto-completion produces the normal downstream completed-poll event;
* manual completion still produces normal downstream completion behaviour;
* automatic completion does not directly implement downstream notification delivery.

---

# 31. Implementation Sequence

The service should be implemented incrementally.

### Phase 1 — Completion Discovery

Implement the scheduled Cell and have it log the polls it discovers.

### Phase 2 — Completion Candidate

Implement the winner-selection rules and log:

```text
pollId
decision
selectedVenueId
reason
```

No poll mutation yet.

### Phase 3 — Completion Close

Implement the conditional completion operation.

This is the critical correctness milestone.

### Phase 4 — Completion Ambiguous

Connect ambiguous results to the normal push-notification workflow.

### Phase 5 — Audit and downstream integration

Verify that successful completion produces the expected audit and downstream completed-poll event.

---

# 32. Acceptance Criteria

Poll Auto-Completion is considered implemented on the new architecture when:

1. A scheduled housekeeping Cell discovers today's eligible polls.
2. Discovery creates Completion Candidate Cells.
3. Candidate evaluation deterministically identifies a winner or ambiguity.
4. Single-venue polls select their sole venue.
5. Multi-venue polls require exactly one positive highest vote count.
6. Multi-venue winners require `food == true`.
7. Ambiguous polls remain open.
8. Ambiguous polls create normal manual-completion notification work.
9. Clear winners create Completion Close Cells.
10. Completion Close conditionally transitions an open poll to completed.
11. Completion Close never overwrites an already-completed poll.
12. A race with manual completion results in the human's completion being retained.
13. Duplicate Close Cells are safe.
14. Cell replay does not corrupt poll state.
15. Successful completion produces the normal downstream completed-poll event.
16. The service does not depend upon exactly-once execution.
17. The complete workflow operates through the new Listener/Cellar/Handler/Service architecture without requiring the legacy implementation.

---

# 33. Architectural Summary

The complete design is:

```text
                         Scheduled 16:00
                                │
                                ▼
                    ┌──────────────────────┐
                    │ Completion Discovery │
                    │                      │
                    │ Find today's         │
                    │ uncompleted polls    │
                    └──────────┬───────────┘
                               │
                               │ pollId
                               ▼
                    ┌──────────────────────┐
                    │ Completion Candidate │
                    │                      │
                    │ Read current state   │
                    │ Apply winner rules   │
                    └──────────┬───────────┘
                               │
                     ┌─────────┴─────────┐
                     │                   │
                clear winner         ambiguous
                     │                   │
                     ▼                   ▼
          ┌───────────────────┐  ┌────────────────────┐
          │ Completion Close  │  │ Completion          │
          │                   │  │ Ambiguous           │
          │ Conditional       │  │                    │
          │ completion        │  │ Request normal     │
          │                   │  │ push notification   │
          └─────────┬─────────┘  └────────────────────┘
                    │
                    │ completed == false
                    ▼
             ┌──────────────┐
             │   Firestore  │
             │              │
             │ completed:   │
             │ true         │
             │ selected: V  │
             └──────┬───────┘
                    │
                    ▼
             Normal completed-
             poll event flow
```

The fundamental design principle is:

> **Auto-completion is a sequence of replayable decisions culminating in one conditional state transition. The Candidate proposes a winner; Completion Close may apply that proposal only while the poll remains open. If somebody else has already completed the poll, automatic completion steps aside and treats the existing result as authoritative.**
