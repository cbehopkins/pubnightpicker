# Sub Events V2 Specification (Listeners, Events, Housekeeping)

## Purpose

This document is a clean-slate V2 specification for listener, event, and housekeeping behavior, using the current system only as a reference implementation.

Scope boundaries:
- Included: listener entities, event triggers, scheduled and periodic housekeeping, canary health checking, and pubs cache behavior.
- Excluded: queue persistence architecture, queue durability, and global idempotency storage design (tracked elsewhere).

Source of truth for current listener registrations:
- [firebase_sub/plugins/plugin_config.py](firebase_sub/plugins/plugin_config.py)
- [firebase_sub/runtime/event_producers.py](firebase_sub/runtime/event_producers.py)
- [firebase_sub/cli/sub_events.py](firebase_sub/cli/sub_events.py)

## 1) User Requirements (V2)

### 1.1 Product Requirements

1. The system shall expose each business behavior as an explicit entity with a single responsibility.
2. The system shall define each entity in trigger -> execution terms.
3. The system shall keep listener registration explicit in code (no implicit discovery).
4. The system shall support predictable behavior under duplicates, retries, and restarts.
5. The system shall support both periodic cleanup tasks and deadline-based scheduled tasks.
6. The system shall include listener health monitoring (canary) and fail fast when listeners are stale.
7. The system shall maintain a live pub/event venue cache for consumers that need fast local lookup.

### 1.2 Operational Requirements

1. Every entity shall be idempotent by contract.
2. Every entity shall define what data it reads and writes.
3. Every entity shall define which errors are terminal vs retryable.
4. Every entity shall expose clear logging for trigger, execution, success, skip, and failure paths.
5. Every entity shall be testable in isolation with deterministic input documents.

### 1.3 Registration and Wiring Requirements

1. Listener entities shall be declared centrally in [firebase_sub/plugins/plugin_config.py](firebase_sub/plugins/plugin_config.py).
2. Event source watches shall be declared in one producer surface (equivalent to EventProducer).
3. Event type to listener routing shall be explicit and deterministic.
4. Housekeeping entities shall be split into:
- periodic maintenance entities
- time-triggered scheduled entities

## 2) V2 Entity Catalog

## 2.1 Event Producer Entities (Watchers)

These entities listen to Firestore changes and emit internal events.

### Entity: Poll Open Event Producer

Trigger:
- New poll document where completed == false (ADDED).

Executes:
- Emits NEW_POLL event with poll document payload.

Reads:
- polls collection filtered by completion state and optional lookback min date.

Writes:
- none directly.

### Entity: Poll Complete Event Producer

Trigger:
- Poll document where completed == true (ADDED or MODIFIED).

Executes:
- Emits COMP_POLL event with poll document payload.

Reads:
- polls collection filtered by completion state and optional lookback min date.

Writes:
- none directly.

### Entity: Notification Request Event Producer

Trigger:
- notification_req document ADDED or MODIFIED.

Executes:
- Emits PUSH_TEST when document id is push_test.
- Emits PUSH for all other notification request documents.

Reads:
- notification_req collection.

Writes:
- none directly.

### Entity: Chat Message Event Producer

Trigger:
- messages document ADDED.

Executes:
- Emits CHAT_MESSAGE event.

Reads:
- messages collection.

Writes:
- none directly.

### Entity: Admin Delete Request Event Producer

Trigger:
- admin_delete_requests document ADDED or MODIFIED.

Executes:
- Emits ADMIN_DELETE_REQUEST event.

Reads:
- admin_delete_requests collection.

Writes:
- none directly.

## 2.2 Listener Entities (Event Consumers)

These entities consume produced events.

### Entity: New Poll Listener

Trigger:
- Event type NEW_POLL.

Executes:
- Sends poll-open email notifications to users with open poll email preference.
- Sends poll-open push notifications to users/endpoints with push preference enabled.
- Marks open-action completion state for the poll.

Reads:
- open_actions/{poll_id} idempotency/action state.
- poll date (from event payload; fallback poll lookup when needed).
- user email preference and push preference surfaces.

Writes:
- open_actions/{poll_id} action completion updates.

Corner cases:
- Missing poll_id or missing poll date -> terminal skip/failure path should be explicit.
- Duplicate events for same poll should produce no duplicate notifications after state is done.
- Partial callback failure behavior must be defined (all-or-nothing vs per-channel completion).

### Entity: Complete Poll Listener

Trigger:
- Event type COMP_POLL.

Executes:
- Resolves selected venue and completion dedupe key.
- Sends completion/reschedule emails (mailing list plus personal email recipients).
- Sends completion/reschedule push notifications.
- Persists completion action state.

Reads:
- polls/{poll_id} document.
- comp_actions/{poll_id} action state.
- pubs cache for selected venue payload.

Writes:
- comp_actions/{poll_id} completion updates.

Corner cases:
- Poll missing selected venue -> skip.
- Poll selected venue not present in pubs cache at startup -> retryable data-not-ready path.
- Dedupe key changes when selected venue or restaurant metadata changes; this should intentionally allow a reschedule notification path.
- No-op execution (nothing to send) should clear pending in-memory updates.

Current reschedule workaround (previously_actioned):
- The current system has a special hack for post-completion venue changes.
- Trigger is not event creation. Trigger is a COMP_POLL path re-run caused by a modification to an already completed poll document.
- Complete poll producer watches completed == true documents on ADDED and MODIFIED, so updates to selected, restaurant, or restaurant_time can re-emit COMP_POLL.
- Complete listener builds a completion dedupe key from selected + restaurant + restaurant_time.
- If that key is new for a poll, actions run again.
- At action execution time, previously_actioned is computed per action type from prior action history.
- When previously_actioned is true:
- Email subject and preamble switch to a rescheduled variant.
- Push payload event type switches from poll_completed to poll_rescheduled.

Why this is hacky:
- Business intent (modification after completion) is inferred indirectly from action history and dedupe key changes.
- It mixes two concepts inside one listener flow:
- first completion notification
- post-completion modification notification
- Semantics are tied to current action ledger behavior, not an explicit domain event.

V2 improvement target:
- Introduce an explicit event modification domain event separate from creation/completion flows.
- Recommended split:
- PollCompleted event for initial completion notification.
- PollCompletionModified event (or EventModified) for post-completion changes such as selected venue change.
- This should remove dependence on previously_actioned as a hidden intent signal and make notification copy/routing explicit by event type.

### Entity: Notification Mirror Listener

Trigger:
- Event type PUSH (non-push_test notification request docs).

Executes:
- Mirrors request key/value pairs into notification_ack/{doc_id}.

Reads:
- notification_req/{doc_id} payload.
- notification_ack/{doc_id} payload.

Writes:
- notification_ack/{doc_id} merge patch with changed keys only.

Corner cases:
- Empty or already-synced payload should be no-op.
- Partial patch write failures should be retryable.

### Entity: Notification Push Test Listener

Trigger:
- Event type PUSH_TEST (notification_req/push_test).

Executes:
- For each uid key in push_test request payload:
- Sends diagnostic push to active endpoints for that user.
- Writes matching uid ack value into notification_ack/push_test on successful delivery.
- Deletes consumed uid request key from notification_req/push_test.

Reads:
- notification_req/push_test payload.
- notification_ack/push_test payload.
- user push endpoints and user-level push enablement.

Writes:
- notification_ack/push_test (uid ack field).
- notification_req/push_test (deletes processed uid keys).

Corner cases:
- If ack value already matches request value, request key is consumed as duplicate.
- No successful delivery for uid: request key is still removed to avoid endless retries.
- Invalid uid keys are skipped.

### Entity: Chat Message Push Listener

Trigger:
- Event type CHAT_MESSAGE.

Executes:
- Resolves scope:
- scopeType event -> event chat flow.
- otherwise -> global chat flow.
- Resolves recipient users by push preferences.
- Event chat: intersects recipients with attendees and participants, then removes muted users.
- Excludes message author.
- Sends push notifications to eligible active endpoints.
- Writes per-message delivery state to chat_push_actions/{message_id}.
- Marks message as processed.

Reads:
- messages/{message_id} payload.
- users preferences.
- attendance for event scope.
- event chat participants from message history.
- per-user push endpoints.
- chat_push_actions/{message_id} existing state.

Writes:
- chat_push_actions/{message_id} including delivered endpoint hashes, notified user ids, processed marker, timestamps.
- push endpoint active/disabled fields for stale or invalid endpoints.

Corner cases:
- Missing or malformed scope defaults to global/main behavior.
- Endpoint-level dedupe should survive partial failures/retries.
- If no eligible users or no remaining endpoints, message is still marked processed.

### Entity: Admin Delete Request Listener

Trigger:
- Event type ADMIN_DELETE_REQUEST.

Executes:
- Processes only pending requests.
- Checks kill-switch.
- Validates targetUid and preconditions (no user docs remaining).
- Writes audited outcomes and state transitions.
- Optionally performs Firebase Auth deletion when enabled and allowed.
- Emits aggregate outcome metrics.

Reads:
- admin_delete_requests/{request_id}.
- users/{uid}, user-public/{uid} for preconditions.
- system_config/admin_delete kill-switch doc.

Writes:
- admin_delete_requests/{request_id} status updates.
- admin_delete_request_audit immutable records.
- admin_delete_request_metrics global and daily counters.

Corner cases:
- Disabled mode: listener registered but should skip execution cleanly.
- Non-pending requests should be no-op.
- UserNotFound during auth delete should be treated as idempotent success.
- Invalid status transitions should be blocked and logged.

## 2.3 Support Entities

### Entity: Pubs List Listener (Pubs Cache)

Trigger:
- pubs collection ADDED, MODIFIED, REMOVED (via PollManager).

Executes:
- Maintains in-memory dict of venue documents by id.
- Materializes and normalizes next_occurrence_date.
- Writes corrected next_occurrence_date back to venue doc when needed.

Reads:
- pubs collection watch snapshots.
- recurrence fields and existing next_occurrence_date values.

Writes:
- pubs/{venue_id}.next_occurrence_date merge update when materialized value differs.

Corner cases:
- Missing document payload (to_dict None) should be ignored.
- Venue removal should evict cache entry.
- Consumers must handle MissingPubError when startup races occur.

### Entity: Listener Canary

Trigger:
- periodic timer invokes send_canary.
- watch callback on listener_health/sub_events observes updates.

Executes:
- Writes nonce to listener_health/sub_events.
- Tracks last sent nonce/time and last seen nonce.
- Reports stale state when a sent nonce is not observed before timeout.

Reads:
- listener_health/sub_events watched payload.

Writes:
- listener_health/sub_events nonce and sentAt.

Corner cases:
- Before first canary send, stale check returns healthy.
- Write failures should be logged but must not crash immediately.
- Stale detection must be deterministic and drive fail-fast exit.

## 2.4 Periodic Housekeeping Entities

These run on interval/cron trigger as maintenance tasks.

### Entity: delete_notification_diagnostics

Trigger:
- periodic housekeeping cycle.

Executes:
- Deletes diagnostics docs in notification_req and notification_ack.

### Entity: delete_notification_docs_for_past_polls

Trigger:
- periodic housekeeping cycle.

Executes:
- Deletes notification request/ack docs for polls before today.

### Entity: delete_inactive_push_endpoints

Trigger:
- periodic housekeeping cycle.

Executes:
- Deletes inactive endpoints older than retention cutoff.
- Falls back to local filtering when composite index is unavailable.

Corner cases:
- retention_days < 0 must fail validation.
- disabledAt missing or malformed should not be deleted by fallback.

### Entity: delete_stale_push_diagnostic_entries

Trigger:
- periodic housekeeping cycle.

Executes:
- Deletes stale uid timestamp keys from notification_req/push_test and notification_ack/push_test.

Corner cases:
- Supports both direct numeric timestamps and object payload with ts field.

### Entity: delete_stale_poll_action_audit_entries

Trigger:
- periodic housekeeping cycle.

Executes:
- Deletes poll_action_audit records older than retention cutoff.

### Entity: maintain_event_recurrence_polls

Trigger:
- periodic housekeeping cycle.

Executes:
- Scans event venues (venueType == event).
- Resolves/materializes next_occurrence_date from recurrence rule.
- Creates event poll documents when within creation lead window and poll missing.
- Advances or clears next_occurrence_date when current occurrence window is complete.
- Writes poll action audit for created polls.

Corner cases:
- Recurrence missing: venue skipped and/or next occurrence cleared.
- Invalid recurrence output: venue skipped with warning.
- Per-venue failures should not stop processing of other venues.

## 2.5 Scheduled Housekeeping Entities (Deadline-Driven)

### Entity: auto_complete_single_event_polls_due_tomorrow

Trigger:
- daily schedule, 16:00 Europe/London.

Executes:
- Finds uncompleted polls due today with exactly one venue option.
- Marks poll completed with that venue as winner.
- Writes poll action audit record.

Corner cases:
- Historical name includes due_tomorrow for compatibility, but behavior is due today.
- Empty/invalid single option should skip safely.

### Entity: auto_complete_multi_option_polls_due_today

Trigger:
- daily schedule, 16:00 Europe/London.

Executes:
- Finds uncompleted polls due today with one or more venues.
- Determines winner from votes.
- If clear winner with food venue: marks completed and audits action.
- Otherwise: sends manual completion required push to users with canCompletePoll role and notification preference.

Corner cases:
- No clear winner -> manual completion push path.
- Winner has no food -> manual completion push path.
- Push failures in manual-completion notifier are logged and do not crash scheduler loop.

## 3) Cross-Cutting Corner Cases and Design Notes for V2

1. Startup race between complete-poll processing and pubs cache warm-up must remain explicit as retryable.
2. Distinguish message-level idempotency from endpoint-level idempotency in chat flow; both are required.
3. Keep request/ack mirror behavior patch-based to avoid full-document churn.
4. For notification push tests, consume duplicate requests intentionally to prevent stuck keys.
5. Push preference defaults should be explicit and versioned, especially when user docs lack pushPreferences fields.
6. Event producer watches that listen to MODIFIED events can emit many duplicates; listener idempotency must be robust.
7. Post-completion poll modifications currently rely on previously_actioned and dedupe-key drift to represent rescheduling intent; this is fragile and should be replaced with explicit modification events.
8. Scheduled tasks using timezone-based wall-clock schedules must define DST behavior (current reference uses Europe/London).
9. Housekeeping tasks should remain best-effort and isolated; one task failure must not stop other housekeeping tasks.
10. Endpoint deactivation behavior for push errors must classify retryable vs terminal statuses consistently.
11. Admin delete must keep kill-switch and state transition validation as hard safety controls.

## 4) Recommended V2 Documentation Format Per Entity

For each entity in implementation docs, include these fixed fields:
- Entity Name
- Trigger Source
- Trigger Condition
- Executes (ordered steps)
- Reads
- Writes
- Idempotency Key/Rule
- Retryable Errors
- Terminal Errors
- Observability (required logs/metrics)
- Corner Cases

This keeps all listener and housekeeping entities directly comparable and easier to maintain.

## 5) Explicit Listener List for plugin_config

V2 should continue to document and register listeners explicitly, matching current intent:
- new_poll_listener
- complete_poll_listener
- notification_request_listener
- admin_delete_request_listener
- chat_message_listener

And explicitly document non-plugin listener/support entities used by runtime:
- Event producer Firestore watchers (poll open, poll complete, notification request, chat message, admin delete request)
- Canary watcher (listener health)
- Pubs list watcher/cache

## 6) Assumed Firestore Data Model (Current Reference)

This section documents the database shape currently assumed by runtime behavior.
It is a reference baseline for V2, not a statement that this shape is ideal.

Authoritative usage references:
- [firebase_sub/database/handlers.py](firebase_sub/database/handlers.py)
- [firebase_sub/database/repositories.py](firebase_sub/database/repositories.py)
- [firebase_sub/database/housekeeping_tasks.py](firebase_sub/database/housekeeping_tasks.py)
- [firebase_sub/database/event_recurrence.py](firebase_sub/database/event_recurrence.py)
- [firebase_sub/cli/bootstrap.py](firebase_sub/cli/bootstrap.py)

### 6.1 Users Collection

Collection path:
- users/{uid}

Purpose:
- Private user profile and notification preference source for backend processing.

Fields used by backend:
- uid: string
	- Expected to match document id.
- name: string
	- Used for display in some user-facing flows.
- notificationEmail: string
	- Email address used for direct personal notifications.
- notificationEmailEnabled: boolean
	- Enables personal completion emails.
- openPollEmailEnabled: boolean
	- Enables poll-open emails.
- webPushEnabled: boolean
	- Master switch for all web push delivery.
- pushPreferences: map
	- pollOpens: boolean
	- pollCompletes: boolean
	- globalChat: boolean
	- eventChat: boolean
	- eventChatMutedPollIds: list of string (optional; used to mute per-event chat push)
- votesVisible: boolean
	- Used by frontend/profile behavior.

Behavioral defaults and edge semantics:
- If webPushEnabled is missing, push delivery is treated as disabled for safety in endpoint queries.
- If pushPreferences is missing, per-event defaults are applied in some paths:
	- pollOpens default true
	- pollCompletes default true
	- globalChat default false
	- eventChat default false

### 6.2 Public User Collection

Collection path:
- user-public/{uid}

Purpose:
- Public profile projection used by clients and by deletion precondition checks.

Fields observed in baseline/bootstrap flows:
- uid: string
- name: string
- photoUrl: string or null
- votesVisible: boolean

Notes:
- Admin delete request processing checks whether this document still exists as part of precondition validation.
- Listener and housekeeping flows in this doc do not otherwise depend on field-level contents here.

### 6.3 Push Endpoints Subcollection

Collection path:
- users/{uid}/push_endpoints/{endpoint_id}

Purpose:
- Per-device/browser web push subscription records.

Required delivery fields:
- endpoint: string
- p256dh: string
- auth: string
- active: boolean

Operational fields used by backend:
- disabledAt: timestamp (written when endpoint is deactivated)
- lastSeenAt: timestamp (written on deactivation/update paths)

Behavioral semantics:
- active must be true for normal delivery candidates.
- Missing p256dh or auth causes endpoint deactivation.
- Web push 404 and 410 responses mark endpoint stale and deactivate it.
- Web push 400, 401, and 403 responses are treated as terminal for endpoint validity and deactivate it.
- Cleanup tasks remove inactive endpoints after retention based on disabledAt.

### 6.4 Venue Collection (Historically Named pubs)

Collection path:
- pubs/{venue_id}

Purpose:
- Canonical venue records for all venue types.

Naming note:
- Collection name is historical. It acts as a generalized venue collection, not just pubs.

Core fields:
- name: string
- venueType: string (values currently pub, event, restaurant)
	- Missing or empty venueType is treated as pub by notification payload normalization.
- web_site: string (optional)
- address: string (optional)
- map: string (optional)

Event recurrence fields (event-type venues):
- recurrence: map (optional)
	- frequency: once | weekly | monthly | yearly
	- date: YYYY-MM-DD (once)
	- start_date: YYYY-MM-DD
	- interval: integer
	- weekdays: list[int] (weekly)
	- weekday: int (0-6)
	- nth: integer (for nth weekday patterns)
	- month: int (1-12)
	- month_day: int
- next_occurrence_date: YYYY-MM-DD (materialized field)

Behavioral semantics:
- PubsList and recurrence maintenance both normalize next_occurrence_date.
- If recurrence is removed or exhausted, next_occurrence_date may be cleared.
- Event poll materialization uses event venues where venueType is event.

### 6.5 Polls Collection (Primary V2 Simplification Target)

Collection path:
- polls/{poll_id}

Purpose:
- Poll lifecycle state used by open-poll and complete-poll listeners, scheduling, and notifications.

Common fields:
- date: string (YYYY-MM-DD expected by most query/scheduling logic)
- completed: boolean

Completion and notification fields:
- selected: string (winning venue id)
	- Required for completion notification flows.
- restaurant: string (optional related venue id for pre-pub meal)
- restaurant_time: string (optional display value)

Event-recurrence generated fields:
- pubs: map (venue_id -> partial venue payload map)
	- Example entry payload includes name and venueType.
- eventVenueId: string
- eventOccurrenceDate: string (YYYY-MM-DD)

Observed shape variants in current system:
- Manual/user-created open poll can be minimal:
	- date
	- completed false
	- without selected
	- without pubs
- Event-generated open poll often includes:
	- date
	- completed false
	- pubs map (single event venue entry)
	- eventVenueId
	- eventOccurrenceDate
- Completed poll should include:
	- completed true
	- selected
	- optional restaurant and restaurant_time

Subtle runtime dependencies and risks:
- Complete poll processing skips when selected is missing.
- New poll processing needs a resolvable date.
- PollRepository poll parsing requires selected and date for parsed PollDocument output in some code paths.
	- This differs from actual open-poll creation shape, which may not include selected.
	- This mismatch is a key V2 simplification and consistency target.
- Completion dedupe key includes selected plus optional restaurant and restaurant_time.
	- Changing those values intentionally triggers reschedule notification behavior.

Recommended V2 simplification direction for poll schema:
- Split poll state into explicit phases, or define one strict schema with phase-conditional required fields.
- Avoid requiring selected in generic poll parse helpers used outside completion phase.
- Keep optional restaurant metadata clearly separate from winner identity.
- Decide whether pubs snapshot belongs in poll docs or should be derived from venue ids at read time.

### 6.6 Closely Coupled Companion Collections

These were not explicitly requested, but they are structurally coupled to poll behavior and are included for clarity.

Votes collection:
- votes/{poll_id}
- Document map of venue_id -> list of user ids.
- Common seed shape includes any: [] key.

Attendance collection:
- attendance/{poll_id}
- Map keyed by venue id.
- Event chat recipient logic reads canCome lists from venue attendance entries.

Roles collection:
- roles/{role_name}
- Map of uid -> boolean grant.
- Manual poll completion push path uses canCompletePoll role membership.

Action and audit collections used by listener/task idempotency and reporting:
- open_actions/{poll_id}
- comp_actions/{poll_id}
- chat_push_actions/{message_id}
- poll_action_audit/{audit_doc_id}

Notification request and ack collections:
- notification_req/{doc_id}
- notification_ack/{doc_id}
- push_test special document id stores uid keyed diagnostic request and ack values.
