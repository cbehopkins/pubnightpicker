# Poll Auto-Complete Specification

## 1. Purpose

Define the backend behaviour that automatically completes eligible polls, including:
- how the winning venue is selected
- when completion is blocked
- what database writes and downstream events are triggered

This specification is reverse-engineered from implementation and tests and is intended for reimplementation.

## 2. Scope

In scope:
- same-day scheduled auto-complete jobs
- winner selection rules
- completion write contract
- manual-completion notification path
- downstream complete-poll event fan-out

Out of scope:
- front-end voting UX
- open-poll creation flow
- recurring event creation/advance logic except where it intersects completion semantics

## 3. Scheduled jobs and execution windows

Two scheduled housekeeping jobs execute daily at 16:00 Europe/London:
- auto_complete_single_event_polls_due_tomorrow
- auto_complete_multi_option_polls_due_today

Although one function name still says due_tomorrow, both operate on polls whose date equals today.

References:
- [firebase_sub/plugins/plugin_config.py](firebase_sub/plugins/plugin_config.py#L155)
- [firebase_sub/plugins/plugin_config.py](firebase_sub/plugins/plugin_config.py#L164)
- [firebase_sub/plugins/plugin_config.py](firebase_sub/plugins/plugin_config.py#L171)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L258)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L294)

## 4. Candidate poll selection

Both auto-complete jobs query polls where:
- completed == false
- date == target_date (today, ISO date)

Candidate filtering then diverges:
- single-option flow: exactly one venue key in polls/{pollId}.pubs
- multi-option flow: one or more venue keys in polls/{pollId}.pubs

Reference:
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L156)

## 5. Winner-selection algorithm

## 5.1 Single-option polls

Algorithm:
1. Read venue IDs from polls/{pollId}.pubs keys.
2. If there is exactly one venue ID, that venue wins deterministically.
3. If not exactly one, skip this flow.

No vote document is consulted.

Reference:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L258)

## 5.2 Multi-option polls

Algorithm input:
- candidate_venue_ids = keys of polls/{pollId}.pubs
- vote doc = votes/{pollId}

Per-candidate count rule:
- count(venue) = len(votes/{pollId}[venue]) if field exists and is a list
- otherwise count(venue) = 0

Winner rule:
1. Compute counts for every candidate venue.
2. Let top_vote_count = max(counts).
3. If top_vote_count <= 0: no winner.
4. Let winners = venues with count == top_vote_count.
5. If len(winners) != 1: no winner (tie).
6. Else winners[0] is the clear winner.

Additional eligibility rule before completion:
- clear winner must have food == true in venue doc pubs/{venueId}
- if missing venue doc, non-boolean food, or food false: treat as not eligible

Reference:
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L249)
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L428)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L415)

## 5.3 Pseudocode

winner = None
counts = {venue: len(votes[venue]) if list else 0 for venue in candidate_venues}
if max(counts.values(), default=0) > 0:
    top = max(counts.values())
    tied = [v for v, c in counts.items() if c == top]
    if len(tied) == 1:
        winner = tied[0]

if winner is None:
    manual_completion_required()
elif venue_food_true(winner) is not True:
    manual_completion_required()
else:
    complete_poll(winner)

## 6. Poll completion write contract

When a poll is auto-completed, the write is a merge update on polls/{pollId}:
- completed: true
- selected: winnerVenueId

No other poll fields are required by this step.

Reference:
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L431)

## 7. Side effects and additional database updates

## 7.1 Poll action audit write

On each successful auto-completion, write one immutable audit record to poll_action_audit using document ID:
- {pollId}_{actionType}_{timestampMicros}

Payload fields:
- pollId
- actionType = complete
- actorUid = backend:auto
- at (server-side runtime timestamp value)
- pollDate
- selectedVenueId

Audit write failures are logged and do not roll back poll completion.

References:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L193)
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py#L289)

## 7.2 Manual-completion push notification path

Triggered when multi-option auto-complete cannot safely complete because:
- no clear winner (including tie or all zero counts), or
- clear winner exists but winner venue food is not true

Recipients are derived as:
1. Read roles/canCompletePoll document.
2. For each uid with truthy role assignment:
   - user must exist
   - users/{uid}.webPushEnabled must be true
   - users/{uid}.pushPreferences.pollCompletes must be true, default true when absent
3. For eligible users, stream active endpoints from users/{uid}/push_endpoints where active == true.

Push payload event type:
- poll_manual_completion_required

Important: this path sends push notifications only; it does not write notification_req or notification_ack documents directly.

References:
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L355)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py#L389)
- [firebase_sub/push_contract.py](firebase_sub/push_contract.py#L6)
- [firebase_sub/push_contract.py](firebase_sub/push_contract.py#L16)
- [firebase_sub/send_push.py](firebase_sub/send_push.py#L479)

## 8. Downstream triggered events after poll completion

After polls/{pollId}.completed becomes true and selected is set, the runtime complete-poll watcher emits a COMP_POLL event (on add/modify for completed polls query).

That event drives complete actions (deduped by a completion key):
- group email
- personal email
- push notification poll_completed or poll_rescheduled

Dedupe and persistence:
- dedupe key is based on selected venue and optional restaurant fields
- action state is merged into comp_actions/{pollId}

This means auto-complete must at minimum set selected and completed correctly, because those fields are the contract used by downstream completion notifications.

References:
- [firebase_sub/runtime/event_producers.py](firebase_sub/runtime/event_producers.py#L112)
- [firebase_sub/runtime/event_producers.py](firebase_sub/runtime/event_producers.py#L141)
- [firebase_sub/plugins/complete_poll.py](firebase_sub/plugins/complete_poll.py#L58)
- [firebase_sub/plugins/complete_poll.py](firebase_sub/plugins/complete_poll.py#L77)
- [firebase_sub/plugins/complete_poll.py](firebase_sub/plugins/complete_poll.py#L117)
- [firebase_sub/runtime/action_policies.py](firebase_sub/runtime/action_policies.py#L53)
- [firebase_sub/database/handlers.py](firebase_sub/database/handlers.py#L114)
- [firebase_sub/database/handlers.py](firebase_sub/database/handlers.py#L121)

## 9. Inferred schema for this feature

## 9.1 polls collection

Path:
- polls/{pollId}

Fields required by auto-complete logic:
- date: string (YYYY-MM-DD)
- completed: boolean
- pubs: map where keys are venue IDs

Fields written by auto-complete:
- selected: string venue ID
- completed: true

## 9.2 votes collection

Path:
- votes/{pollId}

Relevant shape:
- map from venueId -> list of voter uid strings
- non-list or missing values count as zero votes

## 9.3 venue documents

Path:
- pubs/{venueId}

Relevant field:
- food: boolean true required for winner auto-completion in multi-option flow

## 9.4 poll_action_audit collection

Path:
- poll_action_audit/{pollId_complete_timestampMicros}

Fields:
- pollId
- actionType
- actorUid
- at
- pollDate
- selectedVenueId (for completion)

## 9.5 role and user state for manual-completion notifications

Role assignment:
- roles/canCompletePoll : map uid -> truthy/falsey

User eligibility:
- users/{uid}.webPushEnabled : boolean
- users/{uid}.pushPreferences.pollCompletes : boolean (default true)

Endpoints:
- users/{uid}/push_endpoints/{endpointId}.active : boolean

## 10. Behavioural edge cases

- Missing or unreadable votes document -> treated as no clear winner.
- Tie for top votes -> no clear winner.
- Top votes all zero -> no clear winner.
- Winner venue missing or food not exactly boolean true -> manual completion required.
- Poll audit write failure does not prevent completion.
- Manual completion push send failure is logged and does not complete the poll.

## 11. Conformance requirements for reimplementation

A conforming implementation must:
- run daily same-day completion checks for uncompleted polls
- use deterministic single-option winner selection
- use clear-winner vote algorithm for multi-option polls
- require winner venue food == true for multi-option auto-complete
- write poll completion via merge update setting completed and selected
- write completion audit record best-effort
- trigger manual completion notifications for tie/no-winner/no-food outcomes
- preserve downstream compatibility by producing poll documents with selected + completed semantics consumed by complete-poll notifications

## 12. Evidence sources

- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py)
- [firebase_sub/database/housekeeping_store.py](firebase_sub/database/housekeeping_store.py)
- [tests/test_housekeeping_tasks.py](tests/test_housekeeping_tasks.py)
- [firebase_sub/plugins/plugin_config.py](firebase_sub/plugins/plugin_config.py)
- [firebase_sub/runtime/event_producers.py](firebase_sub/runtime/event_producers.py)
- [firebase_sub/plugins/complete_poll.py](firebase_sub/plugins/complete_poll.py)
- [firebase_sub/runtime/action_policies.py](firebase_sub/runtime/action_policies.py)
- [firebase_sub/push_contract.py](firebase_sub/push_contract.py)
- [tests/test_send_push.py](tests/test_send_push.py)
