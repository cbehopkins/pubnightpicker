# sub_events Design Breakdown and Clean-Sheet Architecture Review

## Scope

This document does two things:

1. Describes the current implementation in service terms:
   - What each plugin/listener watches
   - What side effects/notifications it triggers
   - How idempotency/action tracking works
   - How retry/error handling works
2. Treats the current design as a specification input and proposes a clean-sheet architecture that simplifies the system, especially idempotency models.

## Current Runtime Topology

Current flow:

1. Firestore watch callbacks in `EventProducer` emit `Event` objects.
2. `QueueRunner` dequeues events and dispatches through `EventRegistry`.
3. Matching `EventPlugin` executes `filter -> handle -> mark_done`.
4. Runtime background work runs on two paths:
  - Interval/cron trigger calls `PluginRuntime.run_housekeeping` for periodic maintenance plugins.
  - `ScheduledHousekeepingRunner` runs time-triggered work plugins from the queue loop.

## Queue Lifecycle Model

The queue should be treated as a work-item state machine, not just a transport pipe. A work item is not complete until its runtime work and its writeback/idempotency state both succeed.

### Event queue work items

Event-driven work items move through these states:

1. `enqueued` - an `Event` has been produced and placed on the queue.
2. `running` - `QueueRunner` has dequeued the item and is dispatching it.
3. `retry_scheduled` - the work item hit a retryable failure and has been placed back on the retry path with backoff.
4. `completed` - the handler succeeded and the writeback/`mark_done` step succeeded.
5. `terminal_failed` - the item failed with a non-retryable error or exhausted retries.
6. `dequeued` - the item has fully completed its lifecycle and is no longer active in the queue.

The important boundary is between `handle()` succeeding and `mark_done()` succeeding. For plugins that persist through ActionMan/Firestore, successful runtime work is not considered complete until the writeback has also succeeded.

### Time-triggered work items

Time-triggered work items use a related but distinct lifecycle:

1. `registered` - the plugin has been scheduled or registered with the runner.
2. `scheduled` - the next execution time has been computed.
3. `due` - the scheduled time has arrived and the item is eligible to run.
4. `running` - the runner is executing the work item.
5. `rescheduled` - the item has completed or been deferred and a later execution time has been computed.
6. `completed` - the time-triggered action and any required writeback succeeded.
7. `terminal_failed` - the work item failed in a non-retryable way or exhausted retries.

This is why the document distinguishes periodic maintenance from time-triggered business logic: both use the same runtime machinery, but they carry different lifecycle expectations and different failure consequences.

## Services (Listener Plugins)

### Service: New Poll Notifications

- Plugin: `NewPollListenerPlugin`
- Event type listened for: `EventType.NEW_POLL`
- Source watch:
  - Query: open polls (`completed=False`, optional `min_date`)
  - Change types: ADDED only
- Notifications/side effects:
  - Poll-open email via `send_poll_open_email`
  - Poll-open web push via `send_poll_open_push`
- Data touched:
  - Reads: `open_actions/{poll_id}`
  - Writes: `open_actions/{poll_id}`

Idempotency and action tracking:

- Uses `ActionMan` + `ActionTrack` over `open_actions/{poll_id}`.
- Action key is `poll_id`.
- `filter()` checks if any bound action type is still missing for this key.
- `mark_done()` currently marks all bound actions as completed for the key.

Retry/error handling:

- Callback-level retry signal uses `CallbackExceptionRetry`.
- In `ActionMan.run()`, `CallbackExceptionRetry` is logged and the specific action is left unmarked in memory.
- `CallbackExceptionIgnore` is logged and the action is marked as done.
- `NewPollListenerPlugin.handle()` does not raise on callback retry exceptions (they are handled inside `ActionMan`).

### Service: Complete Poll Notifications

- Plugin: `CompletePollListenerPlugin`
- Event type listened for: `EventType.COMP_POLL`
- Source watch:
  - Query: completed polls (`completed=True`, optional `min_date`)
  - Change types: ADDED and MODIFIED
- Notifications/side effects:
  - Poll complete/rescheduled emails via `send_ampub_email` (mailing list + personal)
  - Poll complete/rescheduled web push via `send_poll_complete_push`
- Data touched:
  - Reads: `comp_actions/{poll_id}`, `polls/{poll_id}`, runtime `PubsList`
  - Writes: `comp_actions/{poll_id}`

Idempotency and action tracking:

- Uses `ActionMan` + `ActionTrack` over `comp_actions/{poll_id}`.
- Action key is `PushDedupeKeys.complete_key(pub_id, restaurant_id, restaurant_time)`.
- This key changes when winner/restaurant/time changes, enabling reschedule behavior.
- `handle()` computes a pending update and stores it in `_pending_updates[poll_id]`.
- `mark_done()` persists only the pending update for that poll.

Retry/error handling:

- Extra handler-level retry decorator retries `RetryablePollDataNotReadyError` (e.g., pubs cache not ready).
- Retry count and delay are configurable (`comp_poll_max_retries`, `comp_poll_retry_delay_seconds`).
- Push/email callbacks may raise `CallbackExceptionRetry`; `ActionMan` logs and leaves action unmarked.
- Unmarked actions remain pending for future runs.

### Service: Notification Request Processor

- Plugin: `NotificationRequestListenerPlugin`
- Event types listened for: `EventType.PUSH`, `EventType.PUSH_TEST`
- Source watch:
  - Query: `notification_req`
  - Change types: ADDED and MODIFIED
- Notifications/side effects:
  - For `PUSH_TEST` (doc id `push_test`): sends diagnostic push and writes ack key/value to `notification_ack/push_test`
  - For normal `PUSH`: mirrors request payload into `notification_ack/{doc_id}`
- Data touched:
  - Reads: request doc, ack doc
  - Writes: ack doc; clears processed request keys for push-test UID entries

Idempotency and action tracking:

- Push-test idempotency:
  - If `ack_payload[uid] == request_value`, request key is consumed/deleted as duplicate.
- Mirror idempotency:
  - Computes patch only for changed/missing keys and writes merge update.

Retry/error handling:

- Both handlers are mostly best-effort and catch/log internal exceptions in places.
- No central action ledger for this plugin; idempotency is based on req/ack document state.
- Completion/writeback note:
  - Completion state is persisted inline during `handle()`.
  - `mark_done()` is an acknowledgment hook and currently a no-op.

### Service: Chat Message Push

- Plugin: `ChatMessageListenerPlugin`
- Event type listened for: `EventType.CHAT_MESSAGE`
- Source watch:
  - Query: `messages`
  - Change types: ADDED only
- Notifications/side effects:
  - Sends push notifications for global chat or event chat scope
- Data touched:
  - Reads: message doc, users/prefs, attendance, message participants, mute lists, endpoints
  - Writes: `chat_push_actions/{message_id}` and endpoint deactivation state for invalid/stale endpoints

Idempotency and action tracking:

- Message-level dedupe marker: `chat_push_actions/{message_id}.processed`.
- Endpoint-level dedupe: `delivered_endpoints` array of endpoint hashes.
- Legacy compatibility: falls back to `notified` UID dedupe for old docs.
- During send, each successful endpoint append is persisted immediately, so partial progress survives retry/crash.

Retry/error handling:

- `send_chat_push` counts retryable failures and raises `CallbackExceptionRetry` if any occurred.
- Invalid/stale endpoints are deactivated for known non-retryable statuses.
- This plugin does not convert `CallbackExceptionRetry` into internal no-raise behavior.
- Completion/writeback note:
  - Delivery state is persisted inline during `handle()`.
  - `mark_done()` is an acknowledgment hook and currently a no-op.

### Service: Admin Delete Request Processor

- Plugin: `AdminDeleteRequestListenerPlugin`
- Event type listened for: `EventType.ADMIN_DELETE_REQUEST`
- Source watch:
  - Query: `admin_delete_requests`
  - Change types: ADDED and MODIFIED
- Notifications/side effects:
  - Validates request preconditions
  - Writes audited status transitions
  - Optionally deletes Firebase Auth user
  - Emits Firestore metrics counters
- Data touched:
  - Reads: request doc, user docs, kill-switch doc
  - Writes: request status, audit records, metrics docs

Idempotency and action tracking:

- Request status state machine enforces valid transitions.
- Non-pending requests are skipped.
- `UserNotFoundError` treated as idempotent success (`auth_deleted`).

Retry/error handling:

- Handler catches and records terminal/auth failures as status transitions.
- Uses explicit outcome auditing rather than queue-level retry contracts.
- Completion/writeback note:
  - Request status transitions are persisted inline in the handler state machine.
  - `mark_done()` is an acknowledgment hook and currently a no-op.

## Shared Runtime Retry/Error Semantics

### Event dispatch semantics

- `EventRegistry.dispatch` stops at first plugin exception and re-raises.
- A plugin only calls `mark_done` if its `handle` succeeded.
- If `mark_done` raises after a successful `handle`, dispatch raises `EventWritebackError`.
- This makes handler failure and writeback failure distinct at runtime.

### Queue-level retry semantics

- `QueueRunner` requeues only known transient infrastructure/network errors via FQCN matching.
- Requeue uses exponential backoff (default base 0.1s, max 5s).
- Non-transient exceptions propagate and stop the runtime.
- Retry classification follows the exception chain, so transient causes wrapped by runtime errors (including `EventWritebackError`) are still retryable.

### Action callback semantics (`ActionMan`)

- `CallbackExceptionRetry` means leave action unmarked and log.
- `CallbackExceptionIgnore` means mark as done despite callback failure.
- Callbacks run per action type, with per-action dummy-run override support.

## Periodic Maintenance and Time-Triggered Work

## Runtime model

There are two distinct execution patterns:

1. **Periodic maintenance** (cleanup jobs):
   - Triggered by either:
     - `PeriodicTrigger(interval_seconds)` or
     - `CroniterTrigger(cron_expression)`
   - Calls `PluginRuntime.run_housekeeping` -> `HousekeepingPluginRunner.run_all`
   - Runs all enabled maintenance plugins sequentially
   - Purpose: optional hygiene and data cleanup at regular intervals (weekly-scale operations)
   - Semantics: best-effort, fire-and-forget; if a cycle is missed, it's deferred maintenance
2. **Time-triggered business logic**:
   - `ScheduledHousekeepingRunner` runs from main queue loop (`QueueRunner`)
   - Each plugin supplies `run_at(now)` and is rescheduled after each run
   - Purpose: functional business requirements tied to specific deadlines (e.g., auto-complete poll at 5pm on event day)
   - Semantics: deadline-driven commitment; miss implies functional failure, not just deferred cleanup

Both use the same plugin infrastructure and error handling but differ in intent and consequence.

Error handling for both:

- Runner catches `PlannedPluginException`, `UnexpectedPluginException`, and generic `Exception`, logs, then continues with next plugin.

## Periodic maintenance jobs (build_housekeeping_plugins)

### Job: delete_notification_diagnostics

- Input/listen model: scheduled invocation only (no Firestore watch)
- Behavior:
  - Deletes diagnostics docs in request/ack collections
- Idempotency:
  - Delete-by-id; repeated runs are no-op
- Retry/error:
  - Exceptions logged by runner; next cycle retries naturally

### Job: delete_notification_docs_for_past_polls

- Input/listen model: scheduled invocation only
- Behavior:
  - For polls with date < today, deletes matching req/ack docs
- Idempotency:
  - Deterministic poll-id scan + delete-by-id
- Retry/error:
  - Best-effort per run; retries on next cycle

### Job: delete_inactive_push_endpoints

- Input/listen model: scheduled invocation only
- Behavior:
  - Deletes inactive endpoints disabled before retention cutoff
  - Falls back to local filtering if composite index unavailable
- Idempotency:
  - Delete-by-reference when cutoff condition met
- Retry/error:
  - Index precondition failure is handled with fallback logic

### Job: delete_stale_push_diagnostic_entries

- Input/listen model: scheduled invocation only
- Behavior:
  - Removes stale uid keys from `notification_req/push_test` and `notification_ack/push_test`
- Idempotency:
  - Uses field-delete merge updates for stale keys only
- Retry/error:
  - Exceptions surfaced to runner and logged

### Job: delete_stale_poll_action_audit_entries

- Input/listen model: scheduled invocation only
- Behavior:
  - Deletes `poll_action_audit` docs older than retention
- Idempotency:
  - Deterministic cutoff + delete-by-reference
- Retry/error:
  - Best-effort per run

### Job: maintain_event_recurrence_polls

- Input/listen model: scheduled invocation only
- Behavior:
  - For event venues with recurrence:
    - materializes `next_occurrence_date`
    - creates event poll when creation window opens
    - advances/clears occurrence date after event week
  - Writes poll action audit entries for created/completed actions
- Idempotency:
  - Uses deterministic event poll IDs (`event_poll_id`)
  - Repeated runs converge on same poll/occurrence state
- Retry/error:
  - Per-venue exceptions are caught/logged so one bad venue does not stop the whole job

## Time-triggered business logic jobs (build_scheduled_housekeeping_plugins)

### Job: auto_complete_single_event_polls_due_tomorrow

- Schedule: daily 16:00 Europe/London
- Behavior:
  - Auto-completes same-day single-option event polls
  - Legacy naming note: the plugin name still includes `due_tomorrow` for compatibility
  - Correction note: this schedule was moved after a bug that could run around 01:00 on the day before the event
- Idempotency:
  - Operates on uncompleted polls only
- Retry/error:
  - Exceptions logged by scheduled runner; rescheduled next day

### Job: auto_complete_multi_option_polls_due_today

- Schedule: daily 16:00 Europe/London
- Behavior:
  - Auto-completes eligible multi-option polls due today
  - If no clear winner or winner has no food, sends manual completion required push
- Idempotency:
  - Operates on uncompleted polls only; deterministic winner rules
- Retry/error:
  - Push send failures are caught/logged in manual-completion notifier path

## Assessment of Current Design as Specification Input

Strengths:

1. Strong decomposition into producer/queue/registry/plugins.
2. Clear `filter -> handle -> mark_done` contract.
3. Periodic maintenance and time-triggered work have explicit plugin boundaries and schedules.
4. Complete-poll dedupe key models business reality (reschedules).
5. Chat push endpoint-level dedupe preserves partial progress.

Complexity pressure points:

1. Idempotency state is fragmented across multiple collections and patterns:
   - `open_actions`, `comp_actions`, `chat_push_actions`, `notification_ack`, request status docs, ad-hoc audit docs
2. Retry semantics are inconsistent:
   - Some flows use callback-local retry signaling (`CallbackExceptionRetry` inside `ActionMan`)
   - Some bubble exceptions to queue runner
   - Queue runner retries only infrastructure-transient error classes
3. Service contracts are not uniform in persistence semantics:
   - Some plugins persist in `mark_done`
   - Others persist inline during `handle`
4. Periodic maintenance/time-triggered flows and event flows are separate execution models with different scheduling and retry logic.

## v2 Architecture Proposal (Updated)

## Goals

1. Keep plugin decomposition but unify execution contract.
2. Keep idempotency state physically separated by concern while standardizing semantics.
3. Make retry policy explicit and consistent across services.
4. Keep Firestore as business source of truth.
5. Keep queue implementation in-memory in v2; defer persistent queue redesign to v3.

## Proposed architecture

### 1) Canonical work-item lifecycle (not a new datastore in v2)

In v2, "canonical" means a consistent lifecycle and policy, not a single shared queue table.

Lifecycle states (applies conceptually across listeners, periodic maintenance, and time-triggered work):

- `ready`
- `running`
- `retry_scheduled`
- `terminal_failed`
- `done`

Core fields (carried in runtime context and plugin-level state as needed):

- `serviceType` (`new_poll_notify`, `complete_poll_notify`, `chat_push`, etc.)
- `entityId` (poll id, message id, request id)
- `dedupeKey` (business key, e.g. complete-key)
- `attemptCount`, `nextAttemptAt`, `lastError`

Implementation note for v2:

- Keep current in-memory queue/runtime loop.
- Apply a shared lifecycle contract so each concern follows the same execution semantics.

### 2) Idempotency model: separate collections, shared contract

Keep concern-specific collections in place:

- `open_actions`
- `comp_actions`
- `chat_push_actions`
- `notification_ack` / request-state patterns
- request status/audit collections (e.g., admin delete)

Do not collapse these into one collection in v2.

Standardize them through a shared contract:

1. Deterministic idempotency key per concern.
2. Common state vocabulary (`pending`, `done`, `retryable_failed`, `terminal_failed` where relevant).
3. Common timestamps/attempt metadata where practical.
4. Common helper interface (for example, `IdempotencyStore`) used by plugins.

This preserves separation of concerns while reducing semantic drift.

### 3) Standardized retry policy

Define explicit error classes and handling:

- `RetryableServiceError` -> retry with scheduled backoff
- `TerminalServiceError` -> fail terminally without retry
- Unknown exception -> retry up to policy limit, then terminal

Backoff policy should be data-driven per service type (initial delay, multiplier, max delay, max attempts).

### 4) Service adapter boundary

Keep current plugin logic as adapters around the shared lifecycle contract:

- `NewPollService`, `CompletePollService`, `ChatPushService`, etc. implement:
  - `prepare(change)`
  - `execute(work_context)`
  - `commit(result)`

This retains business code while reducing orchestration divergence.

### 5) Alignment of periodic maintenance and time-triggered work (without queue rewrite)

In v2, keep existing execution patterns (periodic/cron for maintenance + scheduled runner for time-triggered business logic), but align behavior with listener lifecycle semantics:

- Consistent retry classification
- Consistent status/attempt logging fields
- Consistent error taxonomy (`retryable` vs `terminal`)

This ensures that both optional maintenance tasks and deadline-driven business logic follow the same reliability guarantees.

## v2 Data and Ownership Model

Firestore remains the authoritative source of business truth.

Collection ownership guidance:

1. Frontend-owned collections: backend reads; backend writes only by explicit exception.
2. Backend-owned collections: frontend treats as internal/derived implementation detail.
3. Shared/bridge collections: explicit schema and field ownership contract.

Idempotency storage in v2:

- Keep existing separate collections by concern.
- Add shared metadata conventions and helper APIs rather than centralizing storage.

## v2 Migration strategy

1. Introduce shared lifecycle/retry/idempotency interfaces (no queue backend change).
2. Apply interfaces to one low-risk service first (`notification_request_listener`).
3. Roll through poll services, chat, admin delete, periodic maintenance, and time-triggered work.
4. Preserve existing collection layout while normalizing semantics and observability.
5. Add architecture guardrails documenting frontend-owned vs backend-owned collections.

## Explicitly deferred to v3

The following are intentionally out of scope for v2:

1. Replacing in-memory queue with SQLite/MySQL/Postgres-backed queue.
2. Introducing a single centralized `work_items` datastore.

These remain valid v3 candidates once v2 semantics are stable.

## Explicitly rejected

The following design option is rejected in favor of maintaining separation of concerns:

1. Consolidating all idempotency collections into one physical collection.

Rationale: separate collections minimize accidental coupling and reduce congestion risk. v2 achieves semantic consistency through shared contracts and helper APIs, not through centralized storage.

## Remaining v2 design decisions

1. Should endpoint-level dedupe remain required for chat, or is per-user dedupe acceptable?
2. Do we want exactly-once effects per recipient/channel, or at-least-once with idempotent targets?
3. Which shared status fields are mandatory across all concern-specific idempotency collections?
4. Should admin-delete remain an inline state machine service or move to staged service steps in v2?

## Summary

Current design is already modular, and v2 should preserve that modularity while improving consistency. The v2 target is:

1. Canonical lifecycle semantics across event listeners, periodic maintenance, and time-triggered business logic (while keeping current in-memory queue)
2. Separate idempotency collections with a shared contract
3. A single retry/error taxonomy
4. Clear Firestore collection ownership boundaries
5. Queue persistence redesign explicitly deferred to v3
